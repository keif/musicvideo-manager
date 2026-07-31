# Exact-Hash Duplicate Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Find groups of byte-identical (exact-duplicate) active videos via full SHA-256 hashing, bounded to genuine candidates by the scanner's existing `(file_size, partial_hash)` data, and report them — no filesystem changes.

**Architecture:** A new `internal/duplicates` package (`FindExactDuplicates(cat, root)`) consumes `catalog.ListActive()` directly — no new catalog methods or schema. Videos are grouped in memory by `(FileSize, PartialHash)`; only candidate groups with more than one member get their full bytes streamed through SHA-256, then sub-grouped by that hash into authoritative duplicate groups. A new `duplicates` CLI command drives it.

**Tech Stack:** Go, `crypto/sha256`/`io` (standard library, no new dependency), the existing catalog infrastructure, Cobra CLI — same stack as every prior milestone, and the simplest testing story yet (no `ffmpeg`, no network).

## Global Constraints

- Module path: `github.com/keif/musicvideo-manager`.
- No cgo.
- No AI/model attribution anywhere — not in code comments, not in commit messages.
- No new catalog schema (no new tables or columns) — this feature consumes `catalog.ListActive()`'s existing `FileSize`/`PartialHash`/`Path` fields only.
- Only videos sharing `(FileSize, PartialHash)` with at least one other active video are ever fully read — a video with no collision is never touched, bounding I/O to genuine candidates in a healthy library.
- Full-file hashing streams via `io.Copy` into a `crypto/sha256` hasher — never `os.ReadFile` (video files can be multiple gigabytes).
- A `(FileSize, PartialHash)` collision is only a candidate, never itself reported as a duplicate — the full-SHA-256 comparison is the authoritative check (per ADR 0004).
- A per-file read error is recorded in the run's error list and that file is excluded from grouping — not a hard command failure.
- Report-only: no filesystem changes anywhere in this feature. No renaming, moving, or deleting.
- No `context.Context` parameter on `FindExactDuplicates` — this package does no network I/O and shells out to no external process, matching the precedent set by `nfo.WriteAll(cat, root)` (the other ctx-less writer in this codebase).
- Reported `Paths` are absolute (root-joined), directly actionable by a human deciding what to do about a duplicate group.
- `DuplicateGroups` and each group's `Paths` are sorted (by path) for deterministic output.

---

### Task 1: `internal/duplicates` — finder

**Files:**
- Create: `internal/duplicates/finder.go`
- Test: `internal/duplicates/finder_test.go`

**Interfaces:**
- Consumes: `(*catalog.Catalog) ListActive() ([]*catalog.Video, error)` (existing, from the Foundation/Scanner milestone — `catalog.Video{Path string, FileSize int64, PartialHash string, ...}`), `(*catalog.Catalog) InsertVideo(v *catalog.Video) (int64, error)` (existing, used in tests).
- Produces: `duplicates.DuplicateGroup{FileSize int64, Hash string, Paths []string}`, `duplicates.DuplicateErrorEntry{VideoPath, Message string}`, `duplicates.Result{VideosScanned, CandidatesHashed int, DuplicateGroups []DuplicateGroup, ReclaimableBytes int64, Errors []DuplicateErrorEntry}`, `duplicates.FindExactDuplicates(cat *catalog.Catalog, root string) (Result, error)`. Consumed by `internal/cli` (Task 2).

This task's tests use real temp files with controlled byte content and real `catalog.InsertVideo` calls with explicitly-set `FileSize`/`PartialHash` — no dependency on `internal/scanner`'s actual hash computation. Pure Go, no `ffmpeg`, no network — the simplest testing story of any milestone so far.

- [ ] **Step 1: Write the failing test**

```go
// internal/duplicates/finder_test.go
package duplicates_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/duplicates"
)

func openTestCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return cat
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func insertVideo(t *testing.T, cat *catalog.Catalog, relPath string, fileSize int64, partialHash string) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: fileSize, MTime: 1, PartialHash: partialHash,
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func TestFindExactDuplicatesGroupsIdenticalFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("identical video bytes")

	writeFile(t, filepath.Join(root, "A.mp4"), content)
	writeFile(t, filepath.Join(root, "B.mp4"), content)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "A.mp4", int64(len(content)), "same-partial-hash")
	insertVideo(t, cat, "B.mp4", int64(len(content)), "same-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if result.VideosScanned != 2 || result.CandidatesHashed != 2 {
		t.Fatalf("unexpected counters: %+v", result)
	}
	if len(result.DuplicateGroups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d: %+v", len(result.DuplicateGroups), result.DuplicateGroups)
	}
	group := result.DuplicateGroups[0]
	if group.Hash != sha256Hex(content) {
		t.Fatalf("unexpected hash: %q", group.Hash)
	}
	if group.FileSize != int64(len(content)) {
		t.Fatalf("unexpected file size: %d", group.FileSize)
	}
	wantPaths := []string{filepath.Join(root, "A.mp4"), filepath.Join(root, "B.mp4")}
	if len(group.Paths) != 2 || group.Paths[0] != wantPaths[0] || group.Paths[1] != wantPaths[1] {
		t.Fatalf("unexpected paths: %v", group.Paths)
	}
	if result.ReclaimableBytes != int64(len(content)) {
		t.Fatalf("expected reclaimable bytes %d, got %d", len(content), result.ReclaimableBytes)
	}
}

func TestFindExactDuplicatesRejectsPartialHashCollisionWithDifferentContent(t *testing.T) {
	root := t.TempDir()
	contentA := []byte("AAAA-middle-differs-1-AAAA")
	contentB := []byte("AAAA-middle-differs-2-AAAA")
	if len(contentA) != len(contentB) {
		t.Fatalf("test fixture bug: contents must be the same length")
	}

	writeFile(t, filepath.Join(root, "A.mp4"), contentA)
	writeFile(t, filepath.Join(root, "B.mp4"), contentB)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "A.mp4", int64(len(contentA)), "colliding-partial-hash")
	insertVideo(t, cat, "B.mp4", int64(len(contentB)), "colliding-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if result.CandidatesHashed != 2 {
		t.Fatalf("expected both candidates to be hashed, got %d", result.CandidatesHashed)
	}
	if len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected no duplicate groups (content differs), got %+v", result.DuplicateGroups)
	}
	if result.ReclaimableBytes != 0 {
		t.Fatalf("expected 0 reclaimable bytes, got %d", result.ReclaimableBytes)
	}
}

func TestFindExactDuplicatesSkipsVideoWithNoCollision(t *testing.T) {
	root := t.TempDir()
	content := []byte("unique video bytes")
	writeFile(t, filepath.Join(root, "Solo.mp4"), content)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Solo.mp4", int64(len(content)), "unique-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if result.VideosScanned != 1 {
		t.Fatalf("expected 1 video scanned, got %d", result.VideosScanned)
	}
	if result.CandidatesHashed != 0 {
		t.Fatalf("expected 0 candidates hashed (no collision), got %d", result.CandidatesHashed)
	}
	if len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected no duplicate groups, got %+v", result.DuplicateGroups)
	}
}

func TestFindExactDuplicatesHandlesThreeWayGroup(t *testing.T) {
	root := t.TempDir()
	content := []byte("triplicate video bytes")

	for _, name := range []string{"A.mp4", "B.mp4", "C.mp4"} {
		writeFile(t, filepath.Join(root, name), content)
	}

	cat := openTestCatalog(t)
	for _, name := range []string{"A.mp4", "B.mp4", "C.mp4"} {
		insertVideo(t, cat, name, int64(len(content)), "shared-partial-hash")
	}

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.DuplicateGroups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d: %+v", len(result.DuplicateGroups), result.DuplicateGroups)
	}
	if len(result.DuplicateGroups[0].Paths) != 3 {
		t.Fatalf("expected 3 paths in the group, got %d", len(result.DuplicateGroups[0].Paths))
	}
	wantReclaimable := int64(2) * int64(len(content))
	if result.ReclaimableBytes != wantReclaimable {
		t.Fatalf("expected reclaimable bytes %d, got %d", wantReclaimable, result.ReclaimableBytes)
	}
}

func TestFindExactDuplicatesRecordsReadErrorAndContinues(t *testing.T) {
	root := t.TempDir()
	content := []byte("video bytes")
	writeFile(t, filepath.Join(root, "Real.mp4"), content)
	// "Missing.mp4" is cataloged but has no file on disk -- simulates a
	// file that vanished or became unreadable after the last scan.

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Real.mp4", int64(len(content)), "shared-partial-hash")
	insertVideo(t, cat, "Missing.mp4", int64(len(content)), "shared-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].VideoPath != "Missing.mp4" || result.Errors[0].Message == "" {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if result.CandidatesHashed != 1 {
		t.Fatalf("expected 1 candidate successfully hashed, got %d", result.CandidatesHashed)
	}
	if len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected no duplicate groups (only one survivor), got %+v", result.DuplicateGroups)
	}
}

func TestFindExactDuplicatesReclaimableBytesAcrossMultipleGroups(t *testing.T) {
	root := t.TempDir()
	smallContent := []byte("small")
	bigContent := []byte("this is a much bigger duplicate payload")

	writeFile(t, filepath.Join(root, "Small1.mp4"), smallContent)
	writeFile(t, filepath.Join(root, "Small2.mp4"), smallContent)
	writeFile(t, filepath.Join(root, "Big1.mp4"), bigContent)
	writeFile(t, filepath.Join(root, "Big2.mp4"), bigContent)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Small1.mp4", int64(len(smallContent)), "small-hash")
	insertVideo(t, cat, "Small2.mp4", int64(len(smallContent)), "small-hash")
	insertVideo(t, cat, "Big1.mp4", int64(len(bigContent)), "big-hash")
	insertVideo(t, cat, "Big2.mp4", int64(len(bigContent)), "big-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.DuplicateGroups) != 2 {
		t.Fatalf("expected 2 duplicate groups, got %d: %+v", len(result.DuplicateGroups), result.DuplicateGroups)
	}
	// Sorted by first path: "Big1.mp4" < "Small1.mp4"
	if result.DuplicateGroups[0].Paths[0] != filepath.Join(root, "Big1.mp4") {
		t.Fatalf("expected groups sorted by first path, got %+v", result.DuplicateGroups)
	}
	wantReclaimable := int64(len(smallContent)) + int64(len(bigContent))
	if result.ReclaimableBytes != wantReclaimable {
		t.Fatalf("expected reclaimable bytes %d, got %d", wantReclaimable, result.ReclaimableBytes)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/duplicates/... -v`
Expected: FAIL — `undefined: duplicates.FindExactDuplicates` (the package doesn't exist yet)

- [ ] **Step 3: Implement `finder.go`**

```go
package duplicates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// DuplicateGroup is a set of active videos confirmed byte-identical via
// full SHA-256 hashing.
type DuplicateGroup struct {
	FileSize int64    `json:"file_size"`
	Hash     string   `json:"hash"`
	Paths    []string `json:"paths"` // absolute (root-joined), sorted
}

// DuplicateErrorEntry is a single video whose full-hash read failed. Not
// persisted -- recorded in the run's Result only.
type DuplicateErrorEntry struct {
	VideoPath string `json:"video_path"`
	Message   string `json:"message"`
}

// Result summarizes a FindExactDuplicates run.
type Result struct {
	VideosScanned    int                   `json:"videos_scanned"`
	CandidatesHashed int                   `json:"candidates_hashed"`
	DuplicateGroups  []DuplicateGroup      `json:"duplicate_groups"`
	ReclaimableBytes int64                 `json:"reclaimable_bytes"`
	Errors           []DuplicateErrorEntry `json:"errors"`
}

type sizeHashKey struct {
	size int64
	hash string
}

// FindExactDuplicates finds groups of active videos that are byte-for-byte
// identical. Only videos sharing (FileSize, PartialHash) with at least one
// other active video are fully read -- a (FileSize, PartialHash) collision
// is a candidate, never itself reported; the full SHA-256 comparison is
// the authoritative check. Report-only: no filesystem changes.
func FindExactDuplicates(cat *catalog.Catalog, root string) (Result, error) {
	videos, err := cat.ListActive()
	if err != nil {
		return Result{}, fmt.Errorf("list active videos: %w", err)
	}

	result := Result{VideosScanned: len(videos), DuplicateGroups: []DuplicateGroup{}, Errors: []DuplicateErrorEntry{}}

	candidates := map[sizeHashKey][]*catalog.Video{}
	for _, v := range videos {
		key := sizeHashKey{size: v.FileSize, hash: v.PartialHash}
		candidates[key] = append(candidates[key], v)
	}

	type hashedVideo struct {
		video    *catalog.Video
		fullHash string
	}
	fullGroups := map[string][]hashedVideo{}

	for _, group := range candidates {
		if len(group) < 2 {
			continue
		}
		for _, v := range group {
			fullPath := filepath.Join(root, filepath.FromSlash(v.Path))
			hash, err := hashFile(fullPath)
			if err != nil {
				result.Errors = append(result.Errors, DuplicateErrorEntry{VideoPath: v.Path, Message: err.Error()})
				continue
			}
			result.CandidatesHashed++
			fullGroups[hash] = append(fullGroups[hash], hashedVideo{video: v, fullHash: hash})
		}
	}

	for hash, entries := range fullGroups {
		if len(entries) < 2 {
			continue
		}
		paths := make([]string, 0, len(entries))
		for _, e := range entries {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(e.video.Path)))
		}
		sort.Strings(paths)

		size := entries[0].video.FileSize
		result.DuplicateGroups = append(result.DuplicateGroups, DuplicateGroup{
			FileSize: size,
			Hash:     hash,
			Paths:    paths,
		})
		result.ReclaimableBytes += int64(len(entries)-1) * size
	}

	sort.Slice(result.DuplicateGroups, func(i, j int) bool {
		return result.DuplicateGroups[i].Paths[0] < result.DuplicateGroups[j].Paths[0]
	})

	return result, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/duplicates/... -v`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/duplicates
git commit -m "Add exact-hash duplicate finder"
```

---

### Task 2: `internal/cli` — duplicates command

**Files:**
- Create: `internal/cli/duplicates.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/duplicates_test.go`

**Interfaces:**
- Consumes: `catalog.Open`, `duplicates.FindExactDuplicates`/`Result` (Task 1).
- Produces: `newDuplicatesCmd() *cobra.Command`, registered in `NewRootCmd`. Last code task in this plan.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/duplicates_test.go
package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestDuplicatesCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"duplicates", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when finding duplicates for a library that hasn't been scanned yet")
	}
}

func TestDuplicatesCommandJSONOutputEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	dupCmd := cli.NewRootCmd("test")
	dupCmd.SetOut(&out)
	dupCmd.SetArgs([]string{"duplicates", root, "--json"})

	if err := dupCmd.Execute(); err != nil {
		t.Fatalf("duplicates Execute: %v", err)
	}

	var result struct {
		VideosScanned   int             `json:"videos_scanned"`
		DuplicateGroups []map[string]any `json:"duplicate_groups"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.VideosScanned != 0 || len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestDuplicatesCommand -v`
Expected: FAIL — `unknown command "duplicates"`

- [ ] **Step 3: Implement `duplicates.go`**

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/duplicates"
)

func newDuplicatesCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "duplicates [path]",
		Short: "Find byte-identical duplicate videos via full SHA-256 hashing",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			absRoot, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			stateDir := filepath.Join(absRoot, ".musicvideo-manager")
			if _, err := os.Stat(stateDir); err != nil {
				return fmt.Errorf("library not yet scanned, run 'scan' first: %w", err)
			}

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			result, err := duplicates.FindExactDuplicates(cat, absRoot)
			if err != nil {
				return fmt.Errorf("find duplicates: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Scanned %d videos, hashed %d candidates: %d duplicate groups found, %d bytes reclaimable, %d errors\n",
				result.VideosScanned, result.CandidatesHashed, len(result.DuplicateGroups), result.ReclaimableBytes, len(result.Errors))
			for _, g := range result.DuplicateGroups {
				fmt.Fprintf(cmd.OutOrStdout(), "  duplicate (%d bytes, %s):\n", g.FileSize, g.Hash)
				for _, p := range g.Paths {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", p)
				}
			}
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.VideoPath, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
```

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go`, add `root.AddCommand(newDuplicatesCmd())` to `NewRootCmd`, after the `thumbnail` registration and before `version` (matching the established registration order: scan→match→rename→nfo→artwork→thumbnail→duplicates→version).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across every package.

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "Add duplicates CLI command"
```

---

### Task 3: ADR — exact-hash duplicate detection design

**Files:**
- Create: `docs/adr/0009-exact-hash-duplicate-detection.md`

**Interfaces:**
- Consumes: decisions made in this plan's design spec (`docs/superpowers/specs/2026-07-31-exact-duplicate-detection-design.md`) and this plan's Global Constraints.
- Produces: nothing consumed by code; historical record for contributors, particularly future M5 slices (Chromaprint, frame fingerprinting) that may want to revisit the "no caching" choice documented here.

- [ ] **Step 1: Write the ADR**

```markdown
# 9. Exact-hash duplicate detection: no new schema, no caching, report-only

Status: Accepted

## Context

ADR 0004 established `partial_hash` (SHA-256 of the first/last 1MiB) as
the scanner's cheap move/rename identity check, explicitly deferring full
SHA-256 hashing to "a future `duplicates` sub-project, where accuracy
matters more than scan speed," and noting that a `(file_size,
partial_hash)` collision between two genuinely different files is
possible and that "the `duplicates` sub-project's full-hash pass is the
authoritative check when it matters." This is that sub-project's first
slice.

## Decision

`internal/duplicates.FindExactDuplicates` adds no new catalog schema. It
consumes `catalog.ListActive()`'s existing `FileSize`/`PartialHash`/`Path`
fields directly and does all grouping in memory. Full SHA-256 hashing is
bounded to videos that already collide on `(FileSize, PartialHash)` with
at least one other active video -- in a healthy library this is a small
fraction of the total, keeping the expensive full-file read work bounded
without needing any new persistence.

The computed full hash is **not** cached anywhere (no new `videos.full_hash`
column). Every `duplicates` run recomputes full hashes for that run's
candidate set from scratch. This was a deliberate simplicity choice over
persisting and invalidating a cache: caching would require deciding when a
cached hash goes stale (on `mtime`/`file_size` change, requiring a change
to the scanner's reconciliation logic) and this pass's candidate sets are
expected to be small relative to the whole library, so the repeated-read
cost was judged not worth that complexity for a first slice.

This feature is entirely read-only: it renames, moves, or deletes nothing.
Identifying duplicates and acting on them are different problems with very
different risk profiles, and this slice only does the former.

## Consequences

- Running `duplicates` repeatedly on an unchanged library re-reads every
  candidate file's full bytes each time, rather than reusing a prior run's
  result. If a library's candidate set ever grows large enough for this to
  matter in practice, revisit adding a persisted, invalidation-aware full
  hash -- this ADR's "no caching" call is a first-slice simplicity
  decision, not a permanent architectural constraint.
- Because nothing is cached, `duplicates` and `scan` are fully decoupled:
  running `duplicates` never needs to touch or understand scan-time
  reconciliation logic, and adding this feature required zero changes to
  any previously-shipped code.
- Future M5 slices (Chromaprint audio fingerprinting, frame/perceptual
  video fingerprinting) are near-duplicate detection, a different problem
  from this slice's exact-duplicate detection -- they will likely need
  their own persisted fingerprint storage (unlike this slice), since
  fingerprint computation is more expensive than a full-file hash and
  near-duplicate comparison can't reuse the cheap `partial_hash` pre-filter
  this slice relies on.
```

- [ ] **Step 2: Commit**

```bash
git add docs/adr
git commit -m "Add ADR for exact-hash duplicate detection design"
```

---

### Task 4: Real verification against the live library

**Files:**
- None (verification only — no code changes, mirroring the prior two milestones' closing tasks).

**Interfaces:**
- Consumes: `duplicates` (Task 2), the real catalog at `/Volumes/Yggdrasil/Music Videos/.musicvideo-manager/catalog.sqlite` (624 active videos).
- Produces: a verified, reported real duplicate-detection run.

This is the safest closing verification task of any milestone so far: `duplicates` performs zero filesystem writes of any kind — it only reads files to hash them and prints a report. There is no dry-run distinction to make here at all.

- [ ] **Step 1: Run the real duplicate detection**

Run: `go run ./cmd/musicvideo-manager duplicates "/Volumes/Yggdrasil/Music Videos" --json`

Expected: valid JSON with `videos_scanned` around 624, `candidates_hashed` reflecting however many videos happen to share a `(file_size, partial_hash)` pair with another video (likely a small number, possibly zero, in a real library that hasn't had duplicates deliberately introduced), and 0 (or very few) `errors`.

- [ ] **Step 2: Interpret the result**

If `duplicate_groups` is non-empty: for each group, note the paths and file size. These are real, byte-identical files in the library — report this to your human partner as genuinely actionable information (they may want to manually delete the extras), but do not delete anything yourself; this feature and this task are report-only.

If `duplicate_groups` is empty: this is a valid, expected outcome for a library with no accidental duplicate downloads — report the counts (videos scanned, candidates hashed) as evidence the command ran successfully against real data, not just unit tests.

- [ ] **Step 3: Verify determinism**

Run the exact same command from Step 1 a second time. Expected: byte-identical JSON output (same counts, same groups, same order) — since nothing was written or changed by the first run, a second run over unchanged files must produce an identical result. This is a different kind of verification than the "idempotency" check used for the artwork/thumbnail milestones (which checked that a second run skips already-done work) — here there's no state to skip, so the check is that a read-only command is fully deterministic given unchanged input.

- [ ] **Step 4: Report**

Summarize: videos scanned, candidates hashed, duplicate groups found (if any, with paths and sizes) or confirmed-none, reclaimable bytes, and confirmation the second run produced identical output. This is the final task of the plan — no commit needed (no files changed).

---

## Plan Self-Review Notes

- **Spec coverage:** in-memory `(FileSize, PartialHash)` pre-filter with no new schema ✓ (Task 1), full-SHA-256 authoritative confirmation with streaming I/O ✓ (Task 1), error-tolerant per-file handling ✓ (Task 1), `ReclaimableBytes` reporting ✓ (Task 1), `duplicates` CLI command with the established guard/catalog-open pattern ✓ (Task 2), ADR documenting the no-schema/no-caching/report-only decisions ✓ (Task 3), real (fully read-only, safest of any milestone) verification against the live library ✓ (Task 4). Explicitly out-of-scope spec items (Chromaprint, frame fingerprinting, any deletion/consolidation action, config settings) have no task, correctly.
- **Placeholder scan:** none found — every step has complete, runnable code or exact file content.
- **Type consistency:** verified `duplicates.Result`/`DuplicateGroup`/`DuplicateErrorEntry` field names and `FindExactDuplicates`'s signature match exactly between Task 1 (where they're defined) and Task 2 (where they're consumed). Confirmed against the actual current source (not memory): `catalog.Video` field names (`Path`, `FileSize int64`, `PartialHash string`, `Status`, ...), `(*Catalog) ListActive() ([]*Video, error)`, `(*Catalog) InsertVideo(v *Video) (int64, error)`, `catalog.StatusActive`, `catalog.Open`, and `cli.NewRootCmd(version string) *cobra.Command` all match the plan's usage — same signatures already verified and used correctly in the two prior milestones' plans. Confirmed `videos.path` has a `UNIQUE NOT NULL` constraint (each test's video paths are distinct within their own catalog).
