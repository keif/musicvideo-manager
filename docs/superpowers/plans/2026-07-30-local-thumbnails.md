# Local Video Thumbnails Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate a local thumbnail image (`ffmpeg` frame grab) for every active video in the catalog, written using Kodi's local-thumbnail naming convention, never overwriting an existing thumbnail.

**Architecture:** A new `internal/thumbnail` package mirrors `internal/artwork`'s `WriteAll`/`Result`/error-tolerance shape, but needs no provider, no cache, and no API key — it reads `videos.duration_seconds` (already populated by `scan`) straight from the catalog and shells out to `ffmpeg` (the same `exec.CommandContext` pattern `scanner.Probe` already uses for `ffprobe`) to grab one frame per video. A new `thumbnail` CLI command drives it, reading the seek percentage from a new `config.yaml` section.

**Tech Stack:** Go, `os/exec` (shelling out to the already-required `ffmpeg` binary), the existing catalog and config infrastructure, Cobra CLI — same stack as every prior milestone, no new dependencies.

## Global Constraints

- Module path: `github.com/keif/musicvideo-manager`.
- No cgo.
- No AI/model attribution anywhere — not in code comments, not in commit messages.
- No metadata match is required — this operates on every active video regardless of match status (unlike the fanart.tv artwork feature, which only processes confirmed matches).
- The seek timestamp is computed from `videos.duration_seconds` (already cataloged by `scan`) — never re-probe with `ffprobe`.
- The thumbnail file path is the video's own path with its extension replaced by `.jpg`, in the same directory — Kodi's local-thumbnail convention, auto-detected with no NFO reference needed.
- A thumbnail file that already exists on disk is never overwritten — only missing files are generated.
- A video whose `duration_seconds` is NULL or `<= 0` is skipped and counted, not treated as an error.
- An `ffmpeg` failure for one video is recorded in the run's error list and does not stop processing of the remaining videos.

---

### Task 1: `internal/config` — thumbnail percent setting

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.ThumbnailConfig{Percent int}`, `Config.Thumbnail ThumbnailConfig` field, `Default()`'s new `Thumbnail: ThumbnailConfig{Percent: 10}`. Consumed by `internal/cli` (Task 3).

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`, extend the existing `TestLoadOrCreateWritesDefaultWhenMissing` function by adding this assertion right after the existing `cfg.Scan.Workers` check (do not create a new test function — this extends the existing "defaults for a newly discovered library" test, consistent with how `Match`/`Rename` defaults were verified in prior milestones):

```go
	if cfg.Thumbnail.Percent != 10 {
		t.Fatalf("expected default thumbnail percent 10, got %d", cfg.Thumbnail.Percent)
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/... -run TestLoadOrCreateWritesDefaultWhenMissing -v`
Expected: FAIL — `cfg.Thumbnail` undefined (compile error, `ThumbnailConfig`/`Thumbnail` field don't exist yet)

- [ ] **Step 3: Implement the change**

In `internal/config/config.go`, add the new struct and field. The `Config` struct becomes:

```go
type Config struct {
	SchemaVersion int             `yaml:"schema_version"`
	Library       Library         `yaml:"library"`
	Scan          ScanConfig      `yaml:"scan"`
	Match         MatchConfig     `yaml:"match"`
	Rename        RenameConfig    `yaml:"rename"`
	Thumbnail     ThumbnailConfig `yaml:"thumbnail"`
}
```

Add the new type near `RenameConfig`:

```go
type ThumbnailConfig struct {
	Percent int `yaml:"percent"`
}
```

Update `Default()` to include the new section:

```go
func Default(libraryName string) Config {
	return Config{
		SchemaVersion: 1,
		Library:       Library{Name: libraryName},
		Scan: ScanConfig{
			Extensions: []string{"mp4", "mkv", "avi", "mov", "m4v", "ts", "mpg", "webm"},
			Workers:    0,
		},
		Match: MatchConfig{
			ConfidenceThreshold: 0.7,
		},
		Rename: RenameConfig{
			Template: "{artist}/{title}",
		},
		Thumbnail: ThumbnailConfig{
			Percent: 10,
		},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (all config tests, including the extended one)

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "Add thumbnail percent setting to config"
```

---

### Task 2: `internal/thumbnail` — writer

**Files:**
- Create: `internal/thumbnail/writer.go`
- Test: `internal/thumbnail/writer_test.go`

**Interfaces:**
- Consumes: `(*catalog.Catalog) ListActive() ([]*catalog.Video, error)` (existing, from the Foundation/Scanner milestone — `catalog.Video{Path string, DurationSeconds sql.NullFloat64, ...}`).
- Produces: `thumbnail.ThumbnailErrorEntry{VideoPath, Message string}`, `thumbnail.Result{VideosProcessed, ThumbnailsWritten, ImagesSkipped, NoDuration int, Errors []ThumbnailErrorEntry}`, `thumbnail.WriteAll(ctx context.Context, cat *catalog.Catalog, root string, percent int) (Result, error)`. Consumed by `internal/cli` (Task 3).

This task's tests use real, short synthetic videos generated by `ffmpeg`'s `lavfi` test-source filters — the same technique `internal/scanner/fixture_test.go` already uses — so they exercise the real `ffmpeg` extraction path, not a mock. Tests that don't need `ffmpeg` to run (the two "skip" cases) don't require it, and run everywhere.

- [ ] **Step 1: Write the failing test**

```go
// internal/thumbnail/writer_test.go
package thumbnail_test

import (
	"context"
	"database/sql"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/thumbnail"
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

func insertVideo(t *testing.T, cat *catalog.Catalog, relPath string, duration sql.NullFloat64) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: 5, MTime: 1, PartialHash: relPath,
		Status: catalog.StatusActive, DurationSeconds: duration,
		FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed, skipping")
	}
}

// generateFixtureVideo writes a short synthetic video (color bars + a sine
// wave audio tone) to path using ffmpeg, for tests that need a real,
// decodable file.
func generateFixtureVideo(t *testing.T, path string, durationSeconds int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%d:size=320x240:rate=25", durationSeconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=duration=%d", durationSeconds),
		"-c:v", "libx264", "-c:a", "aac", "-shortest",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture generation failed: %v\n%s", err, out)
	}
}

func TestWriteAllExtractsThumbnail(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	generateFixtureVideo(t, filepath.Join(root, "Beck", "Loser.mp4"), 4)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{Float64: 4, Valid: true})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideosProcessed != 1 || result.ThumbnailsWritten != 1 || result.ImagesSkipped != 0 || result.NoDuration != 0 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	targetPath := filepath.Join(root, "Beck", "Loser.jpg")
	f, err := os.Open(targetPath)
	if err != nil {
		t.Fatalf("expected thumbnail file to exist: %v", err)
	}
	defer f.Close()

	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatalf("expected a valid JPEG, got decode error: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("expected positive JPEG dimensions, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestWriteAllSkipsExistingThumbnail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Beck", "Loser.mp4"), []byte("video"))
	writeFile(t, filepath.Join(root, "Beck", "Loser.jpg"), []byte("already here"))

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{Float64: 4, Valid: true})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ImagesSkipped != 1 || result.ThumbnailsWritten != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	content, err := os.ReadFile(filepath.Join(root, "Beck", "Loser.jpg"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "already here" {
		t.Fatalf("expected existing thumbnail to be left untouched, got %q", content)
	}
}

func TestWriteAllSkipsVideoWithNoDuration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Beck", "Loser.mp4"), []byte("video"))

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.NoDuration != 1 || result.ThumbnailsWritten != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "Beck", "Loser.jpg")); err == nil {
		t.Fatal("expected no thumbnail to be written for a video with no duration")
	}
}

func TestWriteAllRecordsExtractionFailureAndContinues(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Corrupt", "Broken.mp4"), []byte("this is not a video file"))
	generateFixtureVideo(t, filepath.Join(root, "Beck", "Loser.mp4"), 4)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Corrupt/Broken.mp4", sql.NullFloat64{Float64: 4, Valid: true})
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{Float64: 4, Valid: true})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideosProcessed != 2 || result.ThumbnailsWritten != 1 || len(result.Errors) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Errors[0].VideoPath != "Corrupt/Broken.mp4" || result.Errors[0].Message == "" {
		t.Fatalf("unexpected error entry: %+v", result.Errors[0])
	}
	if _, err := os.Stat(filepath.Join(root, "Beck", "Loser.jpg")); err != nil {
		t.Fatalf("expected the good video's thumbnail to still be written: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/thumbnail/... -v`
Expected: FAIL — `undefined: thumbnail.WriteAll` (the package doesn't exist yet)

- [ ] **Step 3: Implement `writer.go`**

```go
package thumbnail

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// ThumbnailErrorEntry is a single video whose thumbnail extraction failed.
// Not persisted -- recorded in the run's Result only.
type ThumbnailErrorEntry struct {
	VideoPath string `json:"video_path"`
	Message   string `json:"message"`
}

// Result summarizes a WriteAll run.
type Result struct {
	VideosProcessed   int                    `json:"videos_processed"`
	ThumbnailsWritten int                    `json:"thumbnails_written"`
	ImagesSkipped     int                    `json:"images_skipped"`
	NoDuration        int                    `json:"no_duration"`
	Errors            []ThumbnailErrorEntry  `json:"errors"`
}

// WriteAll generates a local thumbnail image for every active video in the
// catalog, seeking to percent% into each video's cataloged duration. An
// existing thumbnail file is never overwritten -- this only ever fills gaps.
func WriteAll(ctx context.Context, cat *catalog.Catalog, root string, percent int) (Result, error) {
	videos, err := cat.ListActive()
	if err != nil {
		return Result{}, fmt.Errorf("list active videos: %w", err)
	}

	result := Result{Errors: []ThumbnailErrorEntry{}}

	for _, v := range videos {
		result.VideosProcessed++

		fullPath := filepath.Join(root, filepath.FromSlash(v.Path))
		targetPath := strings.TrimSuffix(fullPath, filepath.Ext(fullPath)) + ".jpg"

		if _, err := os.Stat(targetPath); err == nil {
			result.ImagesSkipped++
			continue
		}

		if !v.DurationSeconds.Valid || v.DurationSeconds.Float64 <= 0 {
			result.NoDuration++
			continue
		}

		seekSeconds := v.DurationSeconds.Float64 * float64(percent) / 100

		if err := extractFrame(ctx, fullPath, seekSeconds, targetPath); err != nil {
			os.Remove(targetPath) // best-effort cleanup of any partial output
			result.Errors = append(result.Errors, ThumbnailErrorEntry{VideoPath: v.Path, Message: err.Error()})
			continue
		}
		result.ThumbnailsWritten++
	}

	return result, nil
}

func extractFrame(ctx context.Context, videoPath string, seekSeconds float64, targetPath string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.3f", seekSeconds),
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "2",
		targetPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg extract frame: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/thumbnail/... -v`
Expected: PASS (all 4 tests; the two `requireFFmpeg` tests skip cleanly if `ffmpeg` isn't installed, otherwise all 4 pass)

- [ ] **Step 5: Commit**

```bash
git add internal/thumbnail
git commit -m "Add local video thumbnail writer"
```

---

### Task 3: `internal/cli` — thumbnail command

**Files:**
- Create: `internal/cli/thumbnail.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/thumbnail_test.go`

**Interfaces:**
- Consumes: `catalog.Open`, `config.LoadOrCreate`, `config.Config.Thumbnail.Percent` (Task 1), `thumbnail.WriteAll`/`Result` (Task 2).
- Produces: `newThumbnailCmd() *cobra.Command`, registered in `NewRootCmd`. Last code task in this plan.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/thumbnail_test.go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestThumbnailCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"thumbnail", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when generating thumbnails for a library that hasn't been scanned yet")
	}
}

func TestThumbnailCommandRejectsInvalidPercentConfig(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	// Simulate a config.yaml that predates the thumbnail section (the
	// established non-backfilling behavior of config.Load) -- percent
	// resolves to the Go zero value, 0, which must be rejected rather
	// than silently seeking to the very first frame of every video.
	configPath := filepath.Join(root, ".musicvideo-manager", "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 1\nlibrary:\n  name: Test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	thumbCmd := cli.NewRootCmd("test")
	thumbCmd.SetOut(&bytes.Buffer{})
	thumbCmd.SetArgs([]string{"thumbnail", root})

	if err := thumbCmd.Execute(); err == nil {
		t.Fatal("expected an error when thumbnail.percent is missing/zero in config.yaml")
	}
}

func TestThumbnailCommandJSONOutputEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	thumbCmd := cli.NewRootCmd("test")
	thumbCmd.SetOut(&out)
	thumbCmd.SetArgs([]string{"thumbnail", root, "--json"})

	if err := thumbCmd.Execute(); err != nil {
		t.Fatalf("thumbnail Execute: %v", err)
	}

	var result struct {
		VideosProcessed   int `json:"videos_processed"`
		ThumbnailsWritten int `json:"thumbnails_written"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.VideosProcessed != 0 || result.ThumbnailsWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
```

Note: a fresh `scan` on an empty temp directory writes a brand-new `config.yaml` via `config.LoadOrCreate`, which (after Task 1) already includes `thumbnail: {percent: 10}` by default — so `TestThumbnailCommandJSONOutputEmptyLibrary` needs no config override and exercises the normal, valid-config path.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestThumbnailCommand -v`
Expected: FAIL — `unknown command "thumbnail"`

- [ ] **Step 3: Implement `thumbnail.go`**

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/config"
	"github.com/keif/musicvideo-manager/internal/thumbnail"
)

func newThumbnailCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "thumbnail [path]",
		Short: "Generate local video thumbnails by extracting a frame with ffmpeg",
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

			cfg, err := config.LoadOrCreate(filepath.Join(stateDir, "config.yaml"), filepath.Base(absRoot))
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Thumbnail.Percent <= 0 || cfg.Thumbnail.Percent >= 100 {
				return fmt.Errorf("thumbnail.percent must be between 1 and 99 in config.yaml (got %d) -- add a thumbnail: section with percent: 10 (or your preferred value)", cfg.Thumbnail.Percent)
			}

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			result, err := thumbnail.WriteAll(cmd.Context(), cat, absRoot, cfg.Thumbnail.Percent)
			if err != nil {
				return fmt.Errorf("write thumbnails: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Processed %d videos: %d thumbnails written, %d skipped, %d with no duration, %d errors\n",
				result.VideosProcessed, result.ThumbnailsWritten, result.ImagesSkipped, result.NoDuration, len(result.Errors))
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

The `thumbnail.percent` validation guard runs right after loading config, before opening the catalog — this directly protects against the exact config-backfill gap that bit this project twice before (the `match` and `rename` sections both silently resolved to Go zero-values on a pre-existing `config.yaml` until manually patched).

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go`, add `root.AddCommand(newThumbnailCmd())` to `NewRootCmd`, after the `nfo` registration and before `version` (matching the established registration order: scan→match→rename→nfo→thumbnail→version).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across every package.

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "Add thumbnail CLI command"
```

---

### Task 4: Real verification against the live library

**Files:**
- None (verification only — no code changes, mirroring the fanart.tv artwork plan's closing task).

**Interfaces:**
- Consumes: `thumbnail` (Task 3), the real catalog at `/Volumes/Yggdrasil/Music Videos/.musicvideo-manager/catalog.sqlite`.
- Produces: a verified, reported real thumbnail-generation run.

This is safe to run for real: `thumbnail` only ever adds new files, never moves or deletes anything, so there is no dry-run-only restriction here — same as the fanart.tv artwork feature's closing task.

- [ ] **Step 1: Confirm `ffmpeg` is available**

Run: `which ffmpeg && which ffprobe`
Expected: both resolve to a real binary path. If not, STOP and report back.

- [ ] **Step 2: Confirm the real library's config has a thumbnail section**

Run: `cat "/Volumes/Yggdrasil/Music Videos/.musicvideo-manager/config.yaml"`

If there is no `thumbnail:` section (expected, since this config predates this feature — the same config-backfill gap noted in Global Constraints), add one manually before proceeding:

```yaml
thumbnail:
    percent: 10
```

- [ ] **Step 3: Run the real thumbnail generation**

Run: `go run ./cmd/musicvideo-manager thumbnail "/Volumes/Yggdrasil/Music Videos" --json`

Expected: valid JSON with `videos_processed` around 624 (the real library's video count), a mix of `thumbnails_written`/`no_duration` depending on how many videos have a cataloged duration, and ideally 0 or very few `errors`. This is a real run against real video files — it may take a while for 624 videos even with fast seeking; let it run to completion.

- [ ] **Step 4: Spot-check a handful of generated thumbnails**

Pick 3-5 generated `.jpg` files and open them (or run `file <path>` to confirm a real, non-empty JPEG with plausible dimensions) to confirm they look like real, reasonable video frames — not black, not corrupted, not tiny/empty files.

- [ ] **Step 5: Verify idempotency**

Run the exact same command from Step 3 a second time. Expected: `thumbnails_written` should now be `0` (or very close to it) since the files from the first run already exist, and `images_skipped` should roughly match the first run's `thumbnails_written` count.

- [ ] **Step 6: Report**

Summarize: total videos processed, thumbnails written/skipped/no-duration/errors from both runs, the spot-checked file names, and confirmation the second run was idempotent. This is the final task of the plan — no commit needed (no files changed, aside from the real library's `config.yaml` if Step 2 required an edit, which is a one-time data fix outside version control, consistent with how the `match`/`rename` config-backfill gaps were handled previously).

---

## Plan Self-Review Notes

- **Spec coverage:** `thumbnail.percent` config setting with a sensible default ✓ (Task 1), writer generating thumbnails from cataloged duration via `ffmpeg`, skip-if-exists, skip-if-no-duration, error-tolerant per-video failures ✓ (Task 2), `thumbnail` CLI command with the "not yet scanned" guard and a config-backfill-gap guard ✓ (Task 3), real (safe, additive-only) verification against the live library ✓ (Task 4). Explicitly out-of-scope spec items (smart/scene-aware frame selection, multiple candidates, force-regenerate, any Go-native decoding) have no task, correctly.
- **Placeholder scan:** none found — every step has complete, runnable code or exact file content. No ADR task in this plan (unlike the fanart.tv artwork plan) — this feature doesn't introduce a new architectural decision beyond the pattern `scanner.Probe` already established for shelling out to an ffmpeg-family binary, so a dedicated ADR would be redundant; this was a deliberate scope decision, not an oversight.
- **Type consistency:** verified `thumbnail.Result`/`ThumbnailErrorEntry` field names and `WriteAll`'s signature match exactly between Task 2 (where they're defined) and Task 3 (where they're consumed). `config.ThumbnailConfig.Percent` (Task 1) matches `cfg.Thumbnail.Percent` as read in Task 3. Confirmed against the actual current source (not memory): `catalog.Video` field names (`Path`, `DurationSeconds sql.NullFloat64`, ...), `(*Catalog) ListActive() ([]*Video, error)`, `(*Catalog) InsertVideo(v *Video) (int64, error)`, `catalog.StatusActive`, and `cli.NewRootCmd(version string) *cobra.Command` all match the plan's usage. Confirmed `videos.path` has a `UNIQUE NOT NULL` constraint (each test's video paths are distinct) and `partial_hash` has no uniqueness constraint (test reuse of `relPath` as the hash value is safe).
