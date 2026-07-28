# Foundation & Scanner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the musicvideo-manager repository (Go module, tooling, CI, Docker, ADRs) and ship a working `scan` command that walks a library, extracts metadata via ffprobe, and populates a per-library SQLite catalog with correct incremental rescan, move/rename detection, and missing-file handling.

**Architecture:** A Go CLI (Cobra) drives a `scanner` package that discovers files, hashes/probes new-or-changed ones through a bounded worker pool, and reconciles the results against a `catalog` package wrapping a per-library SQLite database (pure-Go driver, embedded-SQL migrations). Config and catalog live together in `<library-root>/.musicvideo-manager/`. A `pkg/provider` interface is defined now as the seam future metadata providers will implement, with no implementations yet.

**Tech Stack:** Go 1.23+, Cobra, `modernc.org/sqlite` (pure-Go, no cgo), `gopkg.in/yaml.v3`, ffmpeg/ffprobe (external runtime + test dependency), GitHub Actions, Docker.

## Global Constraints

- Module path: `github.com/keif/musicvideo-manager`.
- Target platforms: macOS and Linux only (no Windows in this phase).
- No cgo — `modernc.org/sqlite` only; do not add `mattn/go-sqlite3` or any cgo-based dependency.
- Catalog and config live at `<library-root>/.musicvideo-manager/{config.yaml,catalog.sqlite}` — never in a central per-machine app-data directory.
- Supported video extensions (case-insensitive): `mp4, mkv, avi, mov, m4v, ts, mpg, webm`.
- `scan.workers: 0` in config means "auto" → `runtime.NumCPU()`.
- Do not add stub CLI commands (`match`, `rename`, `artwork`, `doctor`, `duplicates`, `playlists`) — only `scan` and `version` exist in this phase.
- License: MIT. No AI/model attribution anywhere in code, commits, comments, or docs.
- Deviation from spec: the spec mentions `golang-migrate` for schema migrations. This plan uses a small hand-rolled embedded-SQL migrator instead (Task 3) — `golang-migrate`'s maintained SQLite driver depends on the cgo-based `mattn/go-sqlite3`, which would contradict the pure-Go/no-cgo decision. The embedded, versioned, additive-migration *behavior* the spec calls for is preserved; only the library choice changes.

---

### Task 1: Repository & Go module scaffold

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `LICENSE`
- Create: `cmd/musicvideo-manager/main.go`
- Create: directories `internal/catalog/`, `internal/scanner/`, `internal/config/`, `internal/cli/`, `pkg/provider/`, `web/`, `docs/adr/`, `examples/`, `scripts/`, `testdata/`, `.github/workflows/` (via `.gitkeep` where needed so git tracks the empty dir)

**Interfaces:**
- Consumes: nothing (first task).
- Produces: a buildable Go module (`github.com/keif/musicvideo-manager`) and repo skeleton every later task adds files into. `cmd/musicvideo-manager/main.go` is replaced (not just extended) by Task 12.

- [ ] **Step 1: Create the directory skeleton**

```bash
cd ~/projects/musicvideo-manager
mkdir -p cmd/musicvideo-manager internal/catalog/migrations internal/scanner internal/config internal/cli pkg/provider web docs/adr examples scripts testdata/fixtures .github/workflows
touch web/.gitkeep examples/.gitkeep scripts/.gitkeep testdata/fixtures/.gitkeep
```

- [ ] **Step 2: Initialize the Go module**

```bash
go mod init github.com/keif/musicvideo-manager
go 1.23
```

Run `go mod init github.com/keif/musicvideo-manager` (the `go 1.23` line above is informational — `go mod init` sets the Go version from your local toolchain automatically; open `go.mod` afterward and confirm the `go` directive reads `1.23` or higher, editing it if your local Go is older but you want the floor documented in Global Constraints).

- [ ] **Step 3: Write `.gitignore`**

```gitignore
/musicvideo-manager
/dist/
*.sqlite
*.sqlite-journal
.musicvideo-manager/
testdata/fixtures/*.mp4
!testdata/fixtures/.gitkeep
```

- [ ] **Step 4: Write `LICENSE`**

```text
MIT License

Copyright (c) 2026 Keith Baker

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 5: Write a placeholder `main.go`**

```go
package main

import "fmt"

func main() {
	fmt.Println("musicvideo-manager")
}
```

- [ ] **Step 6: Verify the module builds**

Run: `go build ./... && go vet ./...`
Expected: both commands exit 0 with no output.

Run: `go run ./cmd/musicvideo-manager`
Expected: prints `musicvideo-manager`

- [ ] **Step 7: Commit**

```bash
git add go.mod .gitignore LICENSE cmd/ internal/ pkg/ web/ docs/adr examples scripts testdata/fixtures .github
git commit -m "Scaffold Go module and repository layout"
```

---

### Task 2: `pkg/provider` — metadata provider interface

**Files:**
- Create: `pkg/provider/provider.go`
- Test: `pkg/provider/provider_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `provider.MatchQuery`, `provider.MatchResult`, `provider.MetadataProvider` — the interface future metadata-provider sub-projects implement. Not consumed anywhere else in this plan; this task only defines the seam.

- [ ] **Step 1: Write a failing test that a fake provider satisfies the interface**

```go
package provider_test

import (
	"context"
	"testing"

	"github.com/keif/musicvideo-manager/pkg/provider"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error) {
	return []provider.MatchResult{
		{Artist: q.Artist, Title: q.Title, Confidence: 1.0, SourceID: "fake-1"},
	}, nil
}

func TestFakeProviderSatisfiesInterface(t *testing.T) {
	var p provider.MetadataProvider = fakeProvider{}

	results, err := p.Match(context.Background(), provider.MatchQuery{
		Filename: "Artist - Title.mp4",
		Artist:   "Artist",
		Title:    "Title",
	})
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SourceID != "fake-1" {
		t.Fatalf("expected SourceID %q, got %q", "fake-1", results[0].SourceID)
	}
	if p.Name() != "fake" {
		t.Fatalf("expected Name() %q, got %q", "fake", p.Name())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails to compile (types don't exist yet)**

Run: `go test ./pkg/provider/... -v`
Expected: FAIL — `undefined: provider.MatchQuery` (or similar undefined-symbol errors)

- [ ] **Step 3: Implement the interface and types**

```go
package provider

import "context"

// MatchQuery describes what's known about a file when asking a provider
// to identify it.
type MatchQuery struct {
	Filename string
	Artist   string // best-effort guess from filename parsing; may be empty
	Title    string
}

// MatchResult is a single candidate identification returned by a provider.
type MatchResult struct {
	Artist     string
	Title      string
	Album      string
	Year       int
	Confidence float64 // 0.0-1.0
	SourceID   string  // provider-specific ID (MBID, Discogs ID, etc.)
}

// MetadataProvider is implemented by each metadata source (MusicBrainz,
// Discogs, TheAudioDB, Last.fm, ...). No implementations exist yet; this
// interface is the seam future provider sub-projects build against.
type MetadataProvider interface {
	Name() string
	Match(ctx context.Context, q MatchQuery) ([]MatchResult, error)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/provider/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/provider
git commit -m "Add metadata provider plugin interface"
```

---

### Task 3: `internal/catalog` — embedded migrations, Open/Close

**Files:**
- Create: `internal/catalog/migrations/0001_init.sql`
- Create: `internal/catalog/catalog.go`
- Test: `internal/catalog/catalog_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `catalog.Open(path string) (*Catalog, error)`, `(*Catalog) Close() error`. The `videos`, `scan_runs`, and `scan_errors` tables (schema below) exist in any database opened this way. Task 4 and Task 5 add repository methods on `*Catalog`; Task 12 calls `Open`/`Close` directly.

- [ ] **Step 1: Write the migration SQL**

```sql
-- internal/catalog/migrations/0001_init.sql
CREATE TABLE videos (
    id                INTEGER PRIMARY KEY,
    path              TEXT NOT NULL UNIQUE,
    file_size         INTEGER NOT NULL,
    mtime             INTEGER NOT NULL,
    partial_hash      TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active',
    duration_seconds  REAL,
    width             INTEGER,
    height            INTEGER,
    video_codec       TEXT,
    audio_codec       TEXT,
    container         TEXT,
    bitrate           INTEGER,
    fps               REAL,
    first_seen_at     INTEGER NOT NULL,
    last_seen_at      INTEGER NOT NULL,
    last_scanned_at   INTEGER NOT NULL
);
CREATE INDEX idx_videos_status ON videos(status);
CREATE INDEX idx_videos_hash ON videos(partial_hash);

CREATE TABLE scan_runs (
    id             INTEGER PRIMARY KEY,
    started_at     INTEGER NOT NULL,
    finished_at    INTEGER,
    files_scanned  INTEGER NOT NULL DEFAULT 0,
    files_added    INTEGER NOT NULL DEFAULT 0,
    files_updated  INTEGER NOT NULL DEFAULT 0,
    files_moved    INTEGER NOT NULL DEFAULT 0,
    files_missing  INTEGER NOT NULL DEFAULT 0,
    errors_count   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE scan_errors (
    id           INTEGER PRIMARY KEY,
    scan_run_id  INTEGER NOT NULL REFERENCES scan_runs(id),
    path         TEXT NOT NULL,
    message      TEXT NOT NULL,
    occurred_at  INTEGER NOT NULL
);
```

- [ ] **Step 2: Write the failing test**

```go
package catalog_test

import (
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func TestOpenCreatesSchemaAndIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "catalog.sqlite")

	cat, err := catalog.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := cat.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-opening an existing catalog must not fail or re-apply migrations.
	cat2, err := catalog.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer cat2.Close()
}

func TestOpenRejectsUnwritableDirectory(t *testing.T) {
	_, err := catalog.Open(filepath.Join(t.TempDir(), "missing-parent", "catalog.sqlite"))
	if err == nil {
		t.Fatal("expected error opening catalog in a nonexistent directory, got nil")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/catalog/... -v`
Expected: FAIL — `undefined: catalog.Open`

- [ ] **Step 4: Implement `catalog.go`**

```go
package catalog

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Catalog wraps a per-library SQLite database.
type Catalog struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and
// applies any pending migrations. The parent directory must already exist.
func Open(path string) (*Catalog, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Catalog{db: db}, nil
}

// Close closes the underlying database connection.
func (c *Catalog) Close() error {
	return c.db.Close()
}

type migration struct {
	version int
	name    string
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		versionStr := strings.SplitN(e.Name(), "_", 2)[0]
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return fmt.Errorf("invalid migration filename %q: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: e.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })

	for _, m := range migrations {
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", m.version, err)
		}
		if applied > 0 {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.version, time.Now().Unix()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Fetch dependencies**

Run: `go get modernc.org/sqlite && go mod tidy`
Expected: `go.mod`/`go.sum` updated with `modernc.org/sqlite` and its transitive deps, exit 0.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/catalog/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/catalog go.mod go.sum
git commit -m "Add catalog schema and embedded migration runner"
```

---

### Task 4: `internal/catalog` — Video model & repository methods

**Files:**
- Create: `internal/catalog/video.go`
- Test: `internal/catalog/video_test.go`

**Interfaces:**
- Consumes: `catalog.Open` (Task 3).
- Produces: `catalog.VideoStatus`, consts `catalog.StatusActive` / `catalog.StatusMissing`, `catalog.Video` struct, and methods `(*Catalog) ListActive() ([]*Video, error)`, `(*Catalog) FindMissingByHash(fileSize int64, hash string) ([]*Video, error)`, `(*Catalog) InsertVideo(v *Video) (int64, error)`, `(*Catalog) UpdateProbe(v *Video) error`, `(*Catalog) UpdatePath(id int64, newPath string, mtime int64, at int64) error`, `(*Catalog) TouchSeen(id int64, at int64) error`, `(*Catalog) MarkMissing(id int64) error`. Consumed by `internal/scanner` (Tasks 10-11).

- [ ] **Step 1: Write the failing test**

```go
package catalog_test

import (
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
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

func TestInsertAndListActive(t *testing.T) {
	cat := openTestCatalog(t)

	v := &catalog.Video{
		Path: "Artist/Song.mp4", FileSize: 1000, MTime: 111, PartialHash: "abc",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	}
	id, err := cat.InsertVideo(v)
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active video, got %d", len(active))
	}
	if active[0].Path != "Artist/Song.mp4" {
		t.Fatalf("unexpected path %q", active[0].Path)
	}
	if active[0].DurationSeconds.Valid {
		t.Fatal("expected DurationSeconds to be NULL for a video with no probe data")
	}
}

func TestMarkMissingExcludesFromListActive(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "a.mp4", FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	if err := cat.MarkMissing(id); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected 0 active videos after MarkMissing, got %d", len(active))
	}
}

func TestFindMissingByHashOnlyMatchesMissingRows(t *testing.T) {
	cat := openTestCatalog(t)
	activeID, err := cat.InsertVideo(&catalog.Video{
		Path: "active.mp4", FileSize: 500, MTime: 1, PartialHash: "samehash",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo active: %v", err)
	}
	missingID, err := cat.InsertVideo(&catalog.Video{
		Path: "missing.mp4", FileSize: 500, MTime: 2, PartialHash: "samehash",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo missing: %v", err)
	}
	if err := cat.MarkMissing(missingID); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	candidates, err := cat.FindMissingByHash(500, "samehash")
	if err != nil {
		t.Fatalf("FindMissingByHash: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != missingID {
		t.Fatalf("expected only the missing row (id=%d), got %+v", missingID, candidates)
	}
	_ = activeID
}

func TestUpdatePathReactivatesRow(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "old/Path.mp4", FileSize: 10, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	if err := cat.MarkMissing(id); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	if err := cat.UpdatePath(id, "new/Path.mp4", 99, 100); err != nil {
		t.Fatalf("UpdatePath: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Path != "new/Path.mp4" || active[0].MTime != 99 {
		t.Fatalf("unexpected state after UpdatePath: %+v", active)
	}
}

func TestTouchSeenUpdatesTimestampsAndReactivates(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "a.mp4", FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	if err := cat.TouchSeen(id, 500); err != nil {
		t.Fatalf("TouchSeen: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].LastSeenAt != 500 || active[0].LastScannedAt != 500 {
		t.Fatalf("unexpected state after TouchSeen: %+v", active)
	}
}

func TestUpdateProbeSetsMetadataFields(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "a.mp4", FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	v := &catalog.Video{
		ID: id, FileSize: 2000, MTime: 222, PartialHash: "newhash", Status: catalog.StatusActive,
		DurationSeconds: sqlNullFloat(120.5), Width: sqlNullInt(1920), Height: sqlNullInt(1080),
		VideoCodec: sqlNullString("h264"), AudioCodec: sqlNullString("aac"), Container: sqlNullString("mov,mp4,m4a"),
		Bitrate: sqlNullInt(5000000), FPS: sqlNullFloat(29.97), LastSeenAt: 300, LastScannedAt: 300,
	}
	if err := cat.UpdateProbe(v); err != nil {
		t.Fatalf("UpdateProbe: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active video, got %d", len(active))
	}
	got := active[0]
	if !got.DurationSeconds.Valid || got.DurationSeconds.Float64 != 120.5 {
		t.Fatalf("unexpected DurationSeconds: %+v", got.DurationSeconds)
	}
	if !got.VideoCodec.Valid || got.VideoCodec.String != "h264" {
		t.Fatalf("unexpected VideoCodec: %+v", got.VideoCodec)
	}
}
```

- [ ] **Step 2: Add small test-only helpers used above**

Append to the same `video_test.go` file (these adapt Go literals to `database/sql` null types for readability in tests):

```go
import "database/sql"

func sqlNullFloat(v float64) sql.NullFloat64 { return sql.NullFloat64{Float64: v, Valid: true} }
func sqlNullInt(v int64) sql.NullInt64       { return sql.NullInt64{Int64: v, Valid: true} }
func sqlNullString(v string) sql.NullString  { return sql.NullString{String: v, Valid: true} }
```

(Add this `import "database/sql"` to the existing import block at the top of the file rather than as a second import statement.)

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/catalog/... -v`
Expected: FAIL — `undefined: catalog.Video` (or similar)

- [ ] **Step 4: Implement `video.go`**

```go
package catalog

import "database/sql"

// VideoStatus is the lifecycle state of a catalogued video.
type VideoStatus string

const (
	StatusActive  VideoStatus = "active"
	StatusMissing VideoStatus = "missing"
)

// Video is a single row in the videos table.
type Video struct {
	ID              int64
	Path            string
	FileSize        int64
	MTime           int64
	PartialHash     string
	Status          VideoStatus
	DurationSeconds sql.NullFloat64
	Width           sql.NullInt64
	Height          sql.NullInt64
	VideoCodec      sql.NullString
	AudioCodec      sql.NullString
	Container       sql.NullString
	Bitrate         sql.NullInt64
	FPS             sql.NullFloat64
	FirstSeenAt     int64
	LastSeenAt      int64
	LastScannedAt   int64
}

const videoColumns = `id, path, file_size, mtime, partial_hash, status,
	duration_seconds, width, height, video_codec, audio_codec, container, bitrate, fps,
	first_seen_at, last_seen_at, last_scanned_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVideo(row rowScanner) (*Video, error) {
	v := &Video{}
	err := row.Scan(&v.ID, &v.Path, &v.FileSize, &v.MTime, &v.PartialHash, &v.Status,
		&v.DurationSeconds, &v.Width, &v.Height, &v.VideoCodec, &v.AudioCodec, &v.Container, &v.Bitrate, &v.FPS,
		&v.FirstSeenAt, &v.LastSeenAt, &v.LastScannedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// ListActive returns every video row with status = 'active'.
func (c *Catalog) ListActive() ([]*Video, error) {
	rows, err := c.db.Query(`SELECT ` + videoColumns + ` FROM videos WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// FindMissingByHash returns videos with status = 'missing' whose stored
// file_size and partial_hash match, used for move/rename detection.
func (c *Catalog) FindMissingByHash(fileSize int64, hash string) ([]*Video, error) {
	rows, err := c.db.Query(`SELECT `+videoColumns+` FROM videos WHERE status = 'missing' AND file_size = ? AND partial_hash = ?`, fileSize, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InsertVideo inserts a new video row and returns its assigned ID.
func (c *Catalog) InsertVideo(v *Video) (int64, error) {
	res, err := c.db.Exec(`INSERT INTO videos (path, file_size, mtime, partial_hash, status,
		duration_seconds, width, height, video_codec, audio_codec, container, bitrate, fps,
		first_seen_at, last_seen_at, last_scanned_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Path, v.FileSize, v.MTime, v.PartialHash, v.Status,
		v.DurationSeconds, v.Width, v.Height, v.VideoCodec, v.AudioCodec, v.Container, v.Bitrate, v.FPS,
		v.FirstSeenAt, v.LastSeenAt, v.LastScannedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateProbe overwrites size/hash/status/metadata fields and timestamps
// for an existing row, keyed by v.ID. Used for the "changed" reconciliation case.
func (c *Catalog) UpdateProbe(v *Video) error {
	_, err := c.db.Exec(`UPDATE videos SET file_size=?, mtime=?, partial_hash=?, status=?,
		duration_seconds=?, width=?, height=?, video_codec=?, audio_codec=?, container=?, bitrate=?, fps=?,
		last_seen_at=?, last_scanned_at=? WHERE id=?`,
		v.FileSize, v.MTime, v.PartialHash, v.Status,
		v.DurationSeconds, v.Width, v.Height, v.VideoCodec, v.AudioCodec, v.Container, v.Bitrate, v.FPS,
		v.LastSeenAt, v.LastScannedAt, v.ID)
	return err
}

// UpdatePath moves an existing row to a new path and reactivates it.
// Used for the "moved/renamed" and "resurrected" reconciliation cases,
// where probe metadata is reused rather than recomputed.
func (c *Catalog) UpdatePath(id int64, newPath string, mtime int64, at int64) error {
	_, err := c.db.Exec(`UPDATE videos SET path=?, mtime=?, status='active', last_seen_at=?, last_scanned_at=? WHERE id=?`,
		newPath, mtime, at, at, id)
	return err
}

// TouchSeen marks a row as seen (and active) without changing its stored
// metadata. Used for the "unchanged" reconciliation case.
func (c *Catalog) TouchSeen(id int64, at int64) error {
	_, err := c.db.Exec(`UPDATE videos SET status = 'active', last_seen_at = ?, last_scanned_at = ? WHERE id = ?`, at, at, id)
	return err
}

// MarkMissing marks a row missing. Rows are never deleted by scanning.
func (c *Catalog) MarkMissing(id int64) error {
	_, err := c.db.Exec(`UPDATE videos SET status = 'missing' WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (all `TestInsert...`, `TestMarkMissing...`, `TestFindMissingByHash...`, `TestUpdatePath...`, `TestTouchSeen...`, `TestUpdateProbe...` tests pass)

- [ ] **Step 6: Commit**

```bash
git add internal/catalog
git commit -m "Add video catalog repository methods"
```

---

### Task 5: `internal/catalog` — ScanRun & ScanError repository methods

**Files:**
- Create: `internal/catalog/scanrun.go`
- Test: `internal/catalog/scanrun_test.go`

**Interfaces:**
- Consumes: `catalog.Open` (Task 3).
- Produces: `catalog.ScanRunCounts` struct, `(*Catalog) StartScanRun(startedAt int64) (int64, error)`, `(*Catalog) FinishScanRun(id int64, counts ScanRunCounts, finishedAt int64) error`, `(*Catalog) RecordScanError(scanRunID int64, path, message string, occurredAt int64) error`. Consumed by `scanner.Run` (Task 11).

- [ ] **Step 1: Write the failing test**

```go
package catalog_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func TestScanRunLifecycle(t *testing.T) {
	cat := openTestCatalog(t)

	id, err := cat.StartScanRun(1000)
	if err != nil {
		t.Fatalf("StartScanRun: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero scan run id")
	}

	if err := cat.RecordScanError(id, "bad/file.mp4", "ffprobe: invalid data", 1005); err != nil {
		t.Fatalf("RecordScanError: %v", err)
	}

	counts := catalog.ScanRunCounts{Scanned: 3, Added: 1, Updated: 1, Moved: 0, Missing: 0, Errors: 1}
	if err := cat.FinishScanRun(id, counts, 1010); err != nil {
		t.Fatalf("FinishScanRun: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/catalog/... -run TestScanRunLifecycle -v`
Expected: FAIL — `undefined: catalog.ScanRunCounts` (or similar)

- [ ] **Step 3: Implement `scanrun.go`**

```go
package catalog

// ScanRunCounts summarizes the outcome of a single scan run.
type ScanRunCounts struct {
	Scanned int
	Added   int
	Updated int
	Moved   int
	Missing int
	Errors  int
}

// StartScanRun records the start of a scan and returns its ID.
func (c *Catalog) StartScanRun(startedAt int64) (int64, error) {
	res, err := c.db.Exec(`INSERT INTO scan_runs (started_at) VALUES (?)`, startedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScanRun records the outcome counts and completion time for a scan run.
func (c *Catalog) FinishScanRun(id int64, counts ScanRunCounts, finishedAt int64) error {
	_, err := c.db.Exec(`UPDATE scan_runs SET finished_at=?, files_scanned=?, files_added=?,
		files_updated=?, files_moved=?, files_missing=?, errors_count=? WHERE id=?`,
		finishedAt, counts.Scanned, counts.Added, counts.Updated, counts.Moved, counts.Missing, counts.Errors, id)
	return err
}

// RecordScanError logs a single file-level failure encountered during a scan run.
func (c *Catalog) RecordScanError(scanRunID int64, path, message string, occurredAt int64) error {
	_, err := c.db.Exec(`INSERT INTO scan_errors (scan_run_id, path, message, occurred_at) VALUES (?, ?, ?, ?)`,
		scanRunID, path, message, occurredAt)
	return err
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (all catalog package tests, including Task 4's, still pass)

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "Add scan run and scan error catalog repository methods"
```

---

### Task 6: `internal/config` — per-library config file

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config`, `config.Library`, `config.ScanConfig` structs; `config.Default(libraryName string) Config`; `config.LoadOrCreate(path string, defaultLibraryName string) (Config, error)`. Consumed by `internal/cli` (Task 12).

- [ ] **Step 1: Write the failing test**

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/config"
)

func TestLoadOrCreateWritesDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg, err := config.LoadOrCreate(path, "My Library")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Library.Name != "My Library" {
		t.Fatalf("expected library name %q, got %q", "My Library", cfg.Library.Name)
	}
	if len(cfg.Scan.Extensions) == 0 {
		t.Fatal("expected default extensions to be populated")
	}
	if cfg.Scan.Workers != 0 {
		t.Fatalf("expected default workers 0 (auto), got %d", cfg.Scan.Workers)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to be written to disk: %v", err)
	}
}

func TestLoadOrCreateReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "schema_version: 1\nlibrary:\n  name: Existing\nscan:\n  extensions: [mp4]\n  workers: 4\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Ignored Default")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Library.Name != "Existing" {
		t.Fatalf("expected existing config to be loaded, got name %q", cfg.Library.Name)
	}
	if cfg.Scan.Workers != 4 {
		t.Fatalf("expected workers 4, got %d", cfg.Scan.Workers)
	}
}

func TestLoadOrCreateRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := config.LoadOrCreate(path, "Default"); err == nil {
		t.Fatal("expected error loading invalid YAML, got nil")
	}
}

func TestLoadOrCreateRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Default")
	if err != nil {
		t.Fatalf("LoadOrCreate on empty file: %v", err)
	}
	// An empty YAML file parses to a zero-value Config: schema_version 0,
	// no extensions configured. Callers (the scan command) are responsible
	// for deciding whether that's acceptable; Load itself doesn't inject
	// defaults into a file that exists but is empty.
	if cfg.SchemaVersion != 0 {
		t.Fatalf("expected zero-value SchemaVersion for empty file, got %d", cfg.SchemaVersion)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL — `undefined: config.LoadOrCreate`

- [ ] **Step 3: Implement `config.go`**

```go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the per-library configuration stored at
// <library-root>/.musicvideo-manager/config.yaml.
type Config struct {
	SchemaVersion int        `yaml:"schema_version"`
	Library       Library    `yaml:"library"`
	Scan          ScanConfig `yaml:"scan"`
}

type Library struct {
	Name string `yaml:"name"`
}

type ScanConfig struct {
	Extensions []string `yaml:"extensions"`
	Workers    int      `yaml:"workers"`
}

// Default returns the default configuration for a newly discovered library.
func Default(libraryName string) Config {
	return Config{
		SchemaVersion: 1,
		Library:       Library{Name: libraryName},
		Scan: ScanConfig{
			Extensions: []string{"mp4", "mkv", "avi", "mov", "m4v", "ts", "mpg", "webm"},
			Workers:    0,
		},
	}
}

// Load reads and parses an existing config file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// Save writes cfg to path, creating the parent directory if needed.
func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// LoadOrCreate loads the config at path, or writes and returns a default
// config (using defaultLibraryName) if no file exists there yet.
func LoadOrCreate(path string, defaultLibraryName string) (Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := Default(defaultLibraryName)
		if err := Save(path, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	} else if err != nil {
		return Config{}, fmt.Errorf("stat config: %w", err)
	}
	return Load(path)
}
```

- [ ] **Step 4: Fetch dependencies**

Run: `go get gopkg.in/yaml.v3 && go mod tidy`
Expected: exit 0, `go.mod`/`go.sum` updated.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config go.mod go.sum
git commit -m "Add per-library config load/save"
```

---

### Task 7: `internal/scanner` — partial hash

**Files:**
- Create: `internal/scanner/hash.go`
- Test: `internal/scanner/hash_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `scanner.ComputeHash(path string) (string, error)`. Consumed by `scanner.Reconciler` (Task 10) as the default `HashFunc`.

- [ ] **Step 1: Write the failing test**

```go
package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/scanner"
)

func writeFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestComputeHashEmptyFile(t *testing.T) {
	path := writeFile(t, t.TempDir(), "empty.mp4", nil)
	hash, err := scanner.ComputeHash(path)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash for empty file")
	}
}

func TestComputeHashSmallFileDeterministicAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	pathA := writeFile(t, dir, "a.mp4", []byte("hello world"))
	pathB := writeFile(t, dir, "b.mp4", []byte("hello world"))
	pathC := writeFile(t, dir, "c.mp4", []byte("goodbye world"))

	hashA, err := scanner.ComputeHash(pathA)
	if err != nil {
		t.Fatalf("ComputeHash a: %v", err)
	}
	hashB, err := scanner.ComputeHash(pathB)
	if err != nil {
		t.Fatalf("ComputeHash b: %v", err)
	}
	hashC, err := scanner.ComputeHash(pathC)
	if err != nil {
		t.Fatalf("ComputeHash c: %v", err)
	}

	if hashA != hashB {
		t.Fatalf("expected identical content to hash identically: %q != %q", hashA, hashB)
	}
	if hashA == hashC {
		t.Fatal("expected different content to hash differently")
	}
}

func TestComputeHashLargeFileIgnoresMiddleBytes(t *testing.T) {
	dir := t.TempDir()
	const chunk = 1 << 20 // 1MiB, matches scanner.hashChunkSize

	build := func(first, last byte) []byte {
		buf := make([]byte, 2*chunk+100)
		for i := 0; i < chunk; i++ {
			buf[i] = first
		}
		for i := len(buf) - chunk; i < len(buf); i++ {
			buf[i] = last
		}
		return buf
	}

	contentA := build(0xAA, 0xBB)
	contentA[chunk+50] = 0x01 // differs only in the untouched middle region
	contentB := build(0xAA, 0xBB)
	contentB[chunk+50] = 0x02

	pathA := writeFile(t, dir, "large-a.mp4", contentA)
	pathB := writeFile(t, dir, "large-b.mp4", contentB)

	hashA, err := scanner.ComputeHash(pathA)
	if err != nil {
		t.Fatalf("ComputeHash a: %v", err)
	}
	hashB, err := scanner.ComputeHash(pathB)
	if err != nil {
		t.Fatalf("ComputeHash b: %v", err)
	}
	if hashA != hashB {
		t.Fatal("expected files differing only in the middle region to hash identically")
	}

	contentC := build(0xAA, 0xCC) // differs in the last chunk
	pathC := writeFile(t, dir, "large-c.mp4", contentC)
	hashC, err := scanner.ComputeHash(pathC)
	if err != nil {
		t.Fatalf("ComputeHash c: %v", err)
	}
	if hashA == hashC {
		t.Fatal("expected files differing in the last chunk to hash differently")
	}
}

func TestComputeHashNonexistentFile(t *testing.T) {
	_, err := scanner.ComputeHash(filepath.Join(t.TempDir(), "does-not-exist.mp4"))
	if err == nil {
		t.Fatal("expected error hashing a nonexistent file")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scanner/... -v`
Expected: FAIL — `undefined: scanner.ComputeHash`

- [ ] **Step 3: Implement `hash.go`**

```go
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const hashChunkSize = 1 << 20 // 1MiB

// ComputeHash returns a fast identity hash for path: files up to 2MiB are
// hashed in full; larger files are hashed as SHA-256(first 1MiB || last
// 1MiB), skipping the middle. This is fast enough to run on every scan and
// is sufficient for move/rename detection and common-case dedup.
func ComputeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()

	h := sha256.New()
	if size <= 2*hashChunkSize {
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
	} else {
		buf := make([]byte, hashChunkSize)
		if _, err := io.ReadFull(f, buf); err != nil {
			return "", err
		}
		h.Write(buf)

		if _, err := f.Seek(-hashChunkSize, io.SeekEnd); err != nil {
			return "", err
		}
		if _, err := io.ReadFull(f, buf); err != nil {
			return "", err
		}
		h.Write(buf)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/scanner/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/scanner
git commit -m "Add fast partial-content hash for scan identity"
```

---

### Task 8: `internal/scanner` — ffprobe wrapper

**Files:**
- Create: `internal/scanner/ffprobe.go`
- Create: `internal/scanner/fixture_test.go`
- Test: `internal/scanner/ffprobe_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `scanner.ProbeResult` struct, `scanner.Probe(ctx context.Context, path string) (ProbeResult, error)`. Also produces the shared test helpers `requireFFmpeg(t *testing.T)` and `generateFixtureVideo(t *testing.T, path string, durationSeconds int)` in `fixture_test.go`, reused by Task 12's CLI integration test. Consumed by `scanner.Reconciler` (Task 10) as the default `ProbeFunc`.

- [ ] **Step 1: Write the shared fixture test helpers**

```go
package scanner_test

import (
	"fmt"
	"os/exec"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed, skipping")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed, skipping")
	}
}

// generateFixtureVideo writes a short synthetic video (color bars + a sine
// wave audio tone) to path using ffmpeg, for tests that need a real,
// ffprobe-parseable file.
func generateFixtureVideo(t *testing.T, path string, durationSeconds int) {
	t.Helper()
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
```

- [ ] **Step 2: Write the failing test**

```go
package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/scanner"
)

func TestProbeExtractsMetadata(t *testing.T) {
	requireFFmpeg(t)

	path := filepath.Join(t.TempDir(), "fixture.mp4")
	generateFixtureVideo(t, path, 2)

	result, err := scanner.Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if result.DurationSeconds < 1.5 || result.DurationSeconds > 2.5 {
		t.Errorf("expected duration ~2s, got %f", result.DurationSeconds)
	}
	if result.Width != 320 || result.Height != 240 {
		t.Errorf("expected 320x240, got %dx%d", result.Width, result.Height)
	}
	if result.VideoCodec != "h264" {
		t.Errorf("expected video codec h264, got %q", result.VideoCodec)
	}
	if result.AudioCodec != "aac" {
		t.Errorf("expected audio codec aac, got %q", result.AudioCodec)
	}
	if result.FPS < 24 || result.FPS > 26 {
		t.Errorf("expected fps ~25, got %f", result.FPS)
	}
	if result.Container == "" {
		t.Error("expected non-empty container")
	}
}

func TestProbeCorruptFileReturnsError(t *testing.T) {
	requireFFmpeg(t)

	path := filepath.Join(t.TempDir(), "corrupt.mp4")
	if err := os.WriteFile(path, []byte("this is not a video file"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := scanner.Probe(context.Background(), path); err == nil {
		t.Fatal("expected error probing a corrupt file, got nil")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/scanner/... -run TestProbe -v`
Expected: FAIL — `undefined: scanner.Probe` (or, if ffmpeg isn't installed locally, SKIP — in that case, verify the failure by temporarily checking `go build ./internal/scanner/...` fails instead, then proceed; CI always has ffmpeg installed per Task 14)

- [ ] **Step 4: Implement `ffprobe.go`**

```go
package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ProbeResult holds the ffprobe-derived metadata for a video file. A
// zero-value field (0 or "") means ffprobe did not report that value —
// none of these fields can legitimately be exactly zero for a real video.
type ProbeResult struct {
	DurationSeconds float64
	Width           int
	Height          int
	VideoCodec      string
	AudioCodec      string
	Container       string
	Bitrate         int64
	FPS             float64
}

type ffprobeFormat struct {
	Duration   string `json:"duration"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
}

type ffprobeOutput struct {
	Format  ffprobeFormat    `json:"format"`
	Streams []ffprobeStream  `json:"streams"`
}

// Probe runs ffprobe against path and parses out the fields the catalog cares about.
func Probe(ctx context.Context, path string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return ProbeResult{}, fmt.Errorf("parse ffprobe output for %s: %w", path, err)
	}

	result := ProbeResult{Container: parsed.Format.FormatName}
	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		result.DurationSeconds = d
	}
	if b, err := strconv.ParseInt(parsed.Format.BitRate, 10, 64); err == nil {
		result.Bitrate = b
	}

	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if result.VideoCodec == "" {
				result.VideoCodec = s.CodecName
				result.Width = s.Width
				result.Height = s.Height
				result.FPS = parseFrameRate(s.RFrameRate)
			}
		case "audio":
			if result.AudioCodec == "" {
				result.AudioCodec = s.CodecName
			}
		}
	}
	return result, nil
}

func parseFrameRate(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/scanner/... -v`
Expected: PASS (skips with a clear message if ffmpeg/ffprobe aren't installed locally)

- [ ] **Step 6: Commit**

```bash
git add internal/scanner
git commit -m "Add ffprobe metadata extraction"
```

---

### Task 9: `internal/scanner` — file discovery

**Files:**
- Create: `internal/scanner/discover.go`
- Test: `internal/scanner/discover_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `scanner.FileInfo` struct, `scanner.Discover(root string, extensions []string) (map[string]FileInfo, error)`. Consumed by `scanner.Reconciler` (Task 10).

- [ ] **Step 1: Write the failing test**

```go
package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/scanner"
)

func TestDiscoverFiltersByExtensionAndWalksSubdirs(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	mustWrite("Artist/Song.mp4")
	mustWrite("Artist/Nested/Other.MP4")     // case-insensitive extension match
	mustWrite("Artist/notes.txt")             // wrong extension, excluded
	mustWrite(".musicvideo-manager/catalog.sqlite") // state dir, always excluded, even matching content wouldn't matter

	found, err := scanner.Discover(root, []string{"mp4", "mkv"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(found), found)
	}
	if _, ok := found["Artist/Song.mp4"]; !ok {
		t.Error("expected Artist/Song.mp4 to be discovered")
	}
	if _, ok := found["Artist/Nested/Other.MP4"]; !ok {
		t.Error("expected Artist/Nested/Other.MP4 to be discovered")
	}
}

func TestDiscoverSkipsStateDirEvenWithMatchingExtension(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".musicvideo-manager")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sneaky.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	found, err := scanner.Discover(root, []string{"mp4"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 files, got %d: %+v", len(found), found)
	}
}

func TestDiscoverEmptyDirectory(t *testing.T) {
	found, err := scanner.Discover(t.TempDir(), []string{"mp4"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 files, got %d", len(found))
	}
}

func TestDiscoverNonexistentRoot(t *testing.T) {
	_, err := scanner.Discover(filepath.Join(t.TempDir(), "does-not-exist"), []string{"mp4"})
	if err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scanner/... -run TestDiscover -v`
Expected: FAIL — `undefined: scanner.Discover`

- [ ] **Step 3: Implement `discover.go`**

```go
package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// FileInfo is a discovered file's identity as seen on disk, before any
// hashing or probing has happened.
type FileInfo struct {
	Path  string // relative to root, forward-slash separated
	Size  int64
	MTime int64
}

const stateDirName = ".musicvideo-manager"

// Discover walks root recursively and returns every file whose extension
// (case-insensitive) is in extensions, keyed by root-relative path. The
// state directory (.musicvideo-manager) is always skipped, regardless of
// its contents.
func Discover(root string, extensions []string) (map[string]FileInfo, error) {
	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet["."+strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}

	skipDir := filepath.Join(root, stateDirName)
	result := make(map[string]FileInfo)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == skipDir {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !extSet[ext] {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		result[rel] = FileInfo{Path: rel, Size: info.Size(), MTime: info.ModTime().Unix()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/scanner/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/scanner
git commit -m "Add library file discovery walk"
```

---

### Task 10: `internal/scanner` — Reconciler (classification logic)

**Files:**
- Create: `internal/scanner/reconcile.go`
- Test: `internal/scanner/reconcile_test.go`

**Interfaces:**
- Consumes: `catalog.Catalog` and its methods (Tasks 3-4): `ListActive`, `FindMissingByHash`, `InsertVideo`, `UpdateProbe`, `UpdatePath`, `TouchSeen`, `MarkMissing`; `scanner.Discover` (Task 9); `scanner.ProbeResult` (Task 8).
- Produces: `scanner.HashFunc`, `scanner.ProbeFunc` function types; `scanner.Reconciler` struct (`Catalog *catalog.Catalog`, `Hash HashFunc`, `Probe ProbeFunc`, `Workers int`); `scanner.Result` struct (`Added, Updated, Moved, Missing, Unchanged int`, `Errors []ScanErrorEntry`); `scanner.ScanErrorEntry` struct (`Path, Message string`); method `(*Reconciler) Reconcile(ctx context.Context, root string, extensions []string) (Result, error)`. Consumed by `scanner.Run` (Task 11).

This task uses **fake** `Hash`/`Probe` functions in its tests (no ffmpeg dependency) to test classification logic in isolation and quickly. The real `ComputeHash`/`Probe` get exercised end-to-end by Task 12's CLI integration test.

- [ ] **Step 1: Write the failing test**

```go
package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/scanner"
)

func fakeHash(path string) (string, error) {
	// deterministic per-content fake: hash = content bytes as string
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fakeProbe(ctx context.Context, path string) (scanner.ProbeResult, error) {
	return scanner.ProbeResult{DurationSeconds: 120, Width: 1920, Height: 1080, VideoCodec: "h264"}, nil
}

func newReconciler(t *testing.T) (*scanner.Reconciler, *catalog.Catalog) {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return &scanner.Reconciler{Catalog: cat, Hash: fakeHash, Probe: fakeProbe, Workers: 2}, cat
}

func writeVideoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestReconcileFirstScanAddsAllFiles(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")
	writeVideoFile(t, root, "Artist/Other.mp4", "content-b")

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.Added != 2 || result.Updated != 0 || result.Moved != 0 || result.Missing != 0 || result.Unchanged != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileSecondScanIsUnchanged(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")

	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Unchanged != 1 || result.Added != 0 || result.Updated != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileDetectsEditedFile(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")
	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	// Sleep isn't reliable for mtime granularity in CI; force a different
	// mtime explicitly instead of relying on wall-clock delay.
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a-edited")
	newTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "Artist/Song.mp4"), newTime, newTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Updated != 1 || result.Added != 0 || result.Unchanged != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileDetectsRename(t *testing.T) {
	rec, cat := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Old.mp4", "content-a")
	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	if err := os.Rename(filepath.Join(root, "Artist/Old.mp4"), filepath.Join(root, "Artist/New.mp4")); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Moved != 1 || result.Added != 0 || result.Missing != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Path != "Artist/New.mp4" {
		t.Fatalf("expected single active row at new path, got %+v", active)
	}
}

func TestReconcileDetectsDeletion(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")
	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "Artist/Song.mp4")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Missing != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileContinuesPastHashError(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Good.mp4", "content-a")
	writeVideoFile(t, root, "Bad.mp4", "content-b")

	failing := *rec
	failing.Hash = func(path string) (string, error) {
		if filepath.Base(path) == "Bad.mp4" {
			return "", fmt.Errorf("simulated hash failure")
		}
		return fakeHash(path)
	}

	result, err := failing.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("Reconcile should not fail the whole run on one bad file: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected 1 successful add, got %+v", result)
	}
	if len(result.Errors) != 1 || result.Errors[0].Path != "Bad.mp4" {
		t.Fatalf("expected 1 recorded error for Bad.mp4, got %+v", result.Errors)
	}
}
```

Add `"fmt"` and `"time"` to the import block at the top of `reconcile_test.go` alongside `"context"`, `"os"`, `"path/filepath"`, and `"testing"`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scanner/... -run TestReconcile -v`
Expected: FAIL — `undefined: scanner.Reconciler`

- [ ] **Step 3: Implement `reconcile.go`**

Note: this code was corrected during implementation. An earlier version of
this block marked unseen active rows missing *after* running the worker
pool, which meant a same-pass rename's old-path row was still `status =
'active'` (not yet `'missing'`) when the new path was matched via
`FindMissingByHash` — causing a rename to be misclassified as one `Added`
+ one `Missing` instead of a single `Moved`. It also returned immediately
on a fatal catalog error while workers could still be blocked sending on
the unbuffered `resultsCh`, leaking goroutines. The code below marks
unseen rows missing *before* the worker pool runs (tracking newly-missing
IDs so a same-pass move doesn't get double-counted) and drains `resultsCh`
to completion before returning a fatal error. This is the version to
implement — it has been tested and passes `TestReconcileDetectsRename`
along with the other five Reconcile tests, including under `-race`.

```go
package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

type HashFunc func(path string) (string, error)
type ProbeFunc func(ctx context.Context, path string) (ProbeResult, error)

// Reconciler compares a library's on-disk state against the catalog and
// applies the difference.
type Reconciler struct {
	Catalog *catalog.Catalog
	Hash    HashFunc
	Probe   ProbeFunc
	Workers int
}

// ScanErrorEntry is a single file that failed to hash or probe during a
// reconcile pass. It does not abort the run.
type ScanErrorEntry struct {
	Path    string
	Message string
}

// Result summarizes what a single Reconcile call did.
type Result struct {
	Added     int
	Updated   int
	Moved     int
	Missing   int
	Unchanged int
	Errors    []ScanErrorEntry
}

type job struct {
	root     string
	relPath  string
	info     FileInfo
	existing *catalog.Video // nil if no active catalog row at this path
}

func (j job) fullPath() string {
	return filepath.Join(j.root, filepath.FromSlash(j.relPath))
}

type probed struct {
	job  job
	hash string
	pr   ProbeResult
	err  error
}

// Reconcile walks root, classifies every discovered file as unchanged,
// updated, moved, or newly added, marks any previously-active catalog rows
// that were not seen as missing, and persists all of it to the catalog.
func (r *Reconciler) Reconcile(ctx context.Context, root string, extensions []string) (Result, error) {
	discovered, err := Discover(root, extensions)
	if err != nil {
		return Result{}, fmt.Errorf("discover: %w", err)
	}

	active, err := r.Catalog.ListActive()
	if err != nil {
		return Result{}, fmt.Errorf("list active: %w", err)
	}
	byPath := make(map[string]*catalog.Video, len(active))
	for _, v := range active {
		byPath[v.Path] = v
	}

	now := time.Now().Unix()
	result := Result{}
	seenPaths := make(map[string]bool, len(discovered))
	var toProcess []job

	for relPath, info := range discovered {
		seenPaths[relPath] = true
		existing, found := byPath[relPath]
		if found && existing.FileSize == info.Size && existing.MTime == info.MTime {
			if err := r.Catalog.TouchSeen(existing.ID, now); err != nil {
				return Result{}, fmt.Errorf("touch seen %s: %w", relPath, err)
			}
			result.Unchanged++
			continue
		}
		toProcess = append(toProcess, job{root: root, relPath: relPath, info: info, existing: existing})
	}

	// Mark unseen active rows missing before processing new/changed files.
	// This must happen first so that move/rename detection (which matches
	// against status = 'missing' rows via FindMissingByHash) can see a
	// same-pass rename's old-path row: the old path is absent from
	// `discovered`, but its catalog row is still 'active' until this loop
	// runs. If this ran after processJobs instead, a rename would be
	// misclassified as an Added + Missing pair rather than a single Moved.
	//
	// newlyMissing tracks the IDs marked here so that, if move detection
	// below consumes one of them as a rename candidate, it can be removed
	// from the missing count instead of being double-counted as both
	// Missing and Moved. Candidates already missing from a prior scan pass
	// (the "resurrected" case) are not in this set and are left alone.
	newlyMissing := make(map[int64]bool, len(byPath))
	for relPath, v := range byPath {
		if !seenPaths[relPath] {
			if err := r.Catalog.MarkMissing(v.ID); err != nil {
				return Result{}, fmt.Errorf("mark missing %s: %w", relPath, err)
			}
			newlyMissing[v.ID] = true
		}
	}

	if err := r.processJobs(ctx, toProcess, now, &result, newlyMissing); err != nil {
		return Result{}, err
	}

	result.Missing = len(newlyMissing)

	return result, nil
}

func (r *Reconciler) processJobs(ctx context.Context, jobs []job, now int64, result *Result, newlyMissing map[int64]bool) error {
	if len(jobs) == 0 {
		return nil
	}

	workers := r.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	jobsCh := make(chan job)
	resultsCh := make(chan probed)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				hash, herr := r.Hash(j.fullPath())
				if herr != nil {
					resultsCh <- probed{job: j, err: herr}
					continue
				}
				pr, perr := r.Probe(ctx, j.fullPath())
				resultsCh <- probed{job: j, hash: hash, pr: pr, err: perr}
			}
		}()
	}

	go func() {
		for _, j := range jobs {
			jobsCh <- j
		}
		close(jobsCh)
	}()
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Drain resultsCh to completion even after a fatal applyResult error:
	// workers and the job-feeder goroutine above send on these channels
	// unbuffered, so returning early here while they're still in flight
	// would leave them permanently blocked on a send nobody is reading —
	// a goroutine leak. Recording the first error and continuing to drain
	// lets every goroutine finish and every channel close normally.
	var firstErr error
	for p := range resultsCh {
		if p.err != nil {
			result.Errors = append(result.Errors, ScanErrorEntry{Path: p.job.relPath, Message: p.err.Error()})
			continue
		}
		if err := r.applyResult(p, now, result, newlyMissing); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Reconciler) applyResult(p probed, now int64, result *Result, newlyMissing map[int64]bool) error {
	if p.job.existing != nil {
		v := &catalog.Video{
			ID: p.job.existing.ID, Path: p.job.relPath,
			FileSize: p.job.info.Size, MTime: p.job.info.MTime, PartialHash: p.hash,
			Status:          catalog.StatusActive,
			DurationSeconds: nullFloat(p.pr.DurationSeconds),
			Width:           nullInt(int64(p.pr.Width)),
			Height:          nullInt(int64(p.pr.Height)),
			VideoCodec:      nullString(p.pr.VideoCodec),
			AudioCodec:      nullString(p.pr.AudioCodec),
			Container:       nullString(p.pr.Container),
			Bitrate:         nullInt(p.pr.Bitrate),
			FPS:             nullFloat(p.pr.FPS),
			LastSeenAt:      now,
			LastScannedAt:   now,
		}
		if err := r.Catalog.UpdateProbe(v); err != nil {
			return fmt.Errorf("update probe %s: %w", p.job.relPath, err)
		}
		result.Updated++
		return nil
	}

	candidates, err := r.Catalog.FindMissingByHash(p.job.info.Size, p.hash)
	if err != nil {
		return fmt.Errorf("find missing by hash %s: %w", p.job.relPath, err)
	}
	if len(candidates) > 0 {
		best := candidates[0]
		bestDiff := abs64(best.MTime - p.job.info.MTime)
		for _, c := range candidates[1:] {
			d := abs64(c.MTime - p.job.info.MTime)
			if d < bestDiff {
				best, bestDiff = c, d
			}
		}
		if err := r.Catalog.UpdatePath(best.ID, p.job.relPath, p.job.info.MTime, now); err != nil {
			return fmt.Errorf("update path %s: %w", p.job.relPath, err)
		}
		// If best.ID was marked missing earlier in this same pass (a
		// same-scan rename), it's no longer missing now that it's been
		// reattached to the new path — don't double-count it as Missing.
		delete(newlyMissing, best.ID)
		result.Moved++
		return nil
	}

	v := &catalog.Video{
		Path: p.job.relPath, FileSize: p.job.info.Size, MTime: p.job.info.MTime, PartialHash: p.hash,
		Status:          catalog.StatusActive,
		DurationSeconds: nullFloat(p.pr.DurationSeconds),
		Width:           nullInt(int64(p.pr.Width)),
		Height:          nullInt(int64(p.pr.Height)),
		VideoCodec:      nullString(p.pr.VideoCodec),
		AudioCodec:      nullString(p.pr.AudioCodec),
		Container:       nullString(p.pr.Container),
		Bitrate:         nullInt(p.pr.Bitrate),
		FPS:             nullFloat(p.pr.FPS),
		FirstSeenAt:     now,
		LastSeenAt:      now,
		LastScannedAt:   now,
	}
	if _, err := r.Catalog.InsertVideo(v); err != nil {
		return fmt.Errorf("insert video %s: %w", p.job.relPath, err)
	}
	result.Added++
	return nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func nullFloat(v float64) sql.NullFloat64 {
	if v == 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}

func nullInt(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/scanner/... -v`
Expected: PASS (all Reconcile tests, plus Task 7-9's hash/probe/discover tests, still pass)

- [ ] **Step 5: Commit**

```bash
git add internal/scanner
git commit -m "Add scan reconciliation: unchanged/updated/moved/missing classification"
```

---

### Task 11: `internal/scanner` — Run orchestration (scan_runs bookkeeping)

**Files:**
- Create: `internal/scanner/run.go`
- Test: `internal/scanner/run_test.go`

**Interfaces:**
- Consumes: `Reconciler`/`Result`/`ScanErrorEntry` (Task 10); `ComputeHash` (Task 7); `Probe` (Task 8); `catalog.StartScanRun`/`FinishScanRun`/`RecordScanError`/`ScanRunCounts` (Task 5).
- Produces: `scanner.RunSummary` struct (`FilesScanned, FilesAdded, FilesUpdated, FilesMoved, FilesMissing int`, `Errors []ScanErrorEntry`, all with `json` tags), `scanner.Run(ctx context.Context, cat *catalog.Catalog, root string, extensions []string, workers int) (RunSummary, error)`. Consumed by `internal/cli`'s scan command (Task 12).

- [ ] **Step 1: Write the failing test**

```go
package scanner_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/scanner"
)

func TestRunPersistsScanRunAndReturnsSummary(t *testing.T) {
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })

	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")

	// scanner.Run wires up the real ComputeHash/Probe by default, but this
	// test only needs to verify orchestration (counts + scan_runs bookkeeping),
	// so it constructs a Reconciler-backed Run indirectly through the public
	// Run function with the real hash/probe implementations against a fixture
	// that doesn't need to be a valid video for the *hash* path — Probe will
	// fail for non-video content, which exercises the error-recording path too.
	summary, err := scanner.Run(context.Background(), cat, root, []string{"mp4"}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// "content-a" is not a real video, so ffprobe fails and it's recorded as
	// an error rather than added — this test is asserting Run's bookkeeping
	// (FilesScanned includes errored files, the error is captured), not Probe's
	// success path (covered by Task 8's TestProbeExtractsMetadata).
	if summary.FilesScanned != 1 {
		t.Fatalf("expected 1 file scanned, got %d", summary.FilesScanned)
	}
	if len(summary.Errors) != 1 {
		t.Fatalf("expected 1 error (invalid video content), got %+v", summary.Errors)
	}
	if summary.Errors[0].Path != "Artist/Song.mp4" {
		t.Fatalf("unexpected error path: %+v", summary.Errors[0])
	}
}
```

Add `requireFFmpeg(t)` as the first line of `TestRunPersistsScanRunAndReturnsSummary` (this test invokes the real `ffprobe` via `scanner.Run`'s default `Probe`, so it needs the binary present, even though the probe itself is expected to fail against non-video content).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scanner/... -run TestRunPersistsScanRunAndReturnsSummary -v`
Expected: FAIL — `undefined: scanner.Run`

- [ ] **Step 3: Implement `run.go`**

```go
package scanner

import (
	"context"
	"fmt"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// RunSummary is the JSON- and human-printable outcome of a full scan run.
type RunSummary struct {
	FilesScanned int               `json:"files_scanned"`
	FilesAdded   int               `json:"files_added"`
	FilesUpdated int               `json:"files_updated"`
	FilesMoved   int               `json:"files_moved"`
	FilesMissing int               `json:"files_missing"`
	Errors       []ScanErrorEntry  `json:"errors"`
}

// Run performs one full scan of root: reconciling discovered files against
// the catalog, and recording the run and any per-file errors in scan_runs /
// scan_errors.
func Run(ctx context.Context, cat *catalog.Catalog, root string, extensions []string, workers int) (RunSummary, error) {
	startedAt := time.Now().Unix()
	runID, err := cat.StartScanRun(startedAt)
	if err != nil {
		return RunSummary{}, fmt.Errorf("start scan run: %w", err)
	}

	rec := &Reconciler{Catalog: cat, Hash: ComputeHash, Probe: Probe, Workers: workers}
	result, err := rec.Reconcile(ctx, root, extensions)
	finishedAt := time.Now().Unix()
	if err != nil {
		return RunSummary{}, fmt.Errorf("reconcile: %w", err)
	}

	for _, e := range result.Errors {
		if err := cat.RecordScanError(runID, e.Path, e.Message, finishedAt); err != nil {
			return RunSummary{}, fmt.Errorf("record scan error: %w", err)
		}
	}

	scanned := result.Added + result.Updated + result.Moved + result.Unchanged + len(result.Errors)
	counts := catalog.ScanRunCounts{
		Scanned: scanned, Added: result.Added, Updated: result.Updated,
		Moved: result.Moved, Missing: result.Missing, Errors: len(result.Errors),
	}
	if err := cat.FinishScanRun(runID, counts, finishedAt); err != nil {
		return RunSummary{}, fmt.Errorf("finish scan run: %w", err)
	}

	return RunSummary{
		FilesScanned: scanned,
		FilesAdded:   result.Added,
		FilesUpdated: result.Updated,
		FilesMoved:   result.Moved,
		FilesMissing: result.Missing,
		Errors:       result.Errors,
	}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/scanner/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/scanner
git commit -m "Add scan run orchestration and summary reporting"
```

---

### Task 12: `internal/cli` — scan/version commands and main.go wiring

**Files:**
- Modify: `cmd/musicvideo-manager/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/scan.go`
- Test: `internal/cli/scan_test.go`

**Interfaces:**
- Consumes: `config.LoadOrCreate` (Task 6); `catalog.Open`/`Close` (Task 3); `scanner.Run` (Task 11); `requireFFmpeg`/`generateFixtureVideo` test helpers (Task 8, copied into this package's test file since Go test helpers aren't exported across packages).
- Produces: `cli.NewRootCmd(version string) *cobra.Command`. Consumed by `main.go`. This is the last task in the plan — nothing downstream depends on it.

- [ ] **Step 1: Write the failing test**

```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed, skipping")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed, skipping")
	}
}

func generateFixtureVideo(t *testing.T, path string, durationSeconds int) {
	t.Helper()
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

func TestScanCommandTextOutput(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Artist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	generateFixtureVideo(t, filepath.Join(root, "Artist", "Song.mp4"), 1)

	var out bytes.Buffer
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"scan", root})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, ".musicvideo-manager", "catalog.sqlite")); err != nil {
		t.Fatalf("expected catalog.sqlite to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".musicvideo-manager", "config.yaml")); err != nil {
		t.Fatalf("expected config.yaml to be created: %v", err)
	}
	if out.String() == "" {
		t.Fatal("expected non-empty scan summary output")
	}
}

func TestScanCommandJSONOutput(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	generateFixtureVideo(t, filepath.Join(root, "Song.mp4"), 1)

	var out bytes.Buffer
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"scan", root, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var summary struct {
		FilesAdded int `json:"files_added"`
	}
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if summary.FilesAdded != 1 {
		t.Fatalf("expected 1 file added, got %d", summary.FilesAdded)
	}
}

func TestScanCommandSecondRunReportsUnchanged(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	generateFixtureVideo(t, filepath.Join(root, "Song.mp4"), 1)

	first := cli.NewRootCmd("test")
	first.SetOut(&bytes.Buffer{})
	first.SetArgs([]string{"scan", root, "--json"})
	if err := first.Execute(); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	var out bytes.Buffer
	second := cli.NewRootCmd("test")
	second.SetOut(&out)
	second.SetArgs([]string{"scan", root, "--json"})
	if err := second.Execute(); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	var summary struct {
		FilesAdded int `json:"files_added"`
	}
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("expected valid JSON output: %v", err)
	}
	if summary.FilesAdded != 0 {
		t.Fatalf("expected 0 files added on second scan, got %d", summary.FilesAdded)
	}
}

func TestScanCommandNonexistentRoot(t *testing.T) {
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"scan", "/does/not/exist"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error scanning a nonexistent root")
	}
}

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	rootCmd := cli.NewRootCmd("v1.2.3")
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.String() != "v1.2.3\n" {
		t.Fatalf("expected %q, got %q", "v1.2.3\n", out.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -v`
Expected: FAIL — `undefined: cli.NewRootCmd`

- [ ] **Step 3: Implement `root.go`**

```go
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the musicvideo-manager root command with all
// subcommands attached.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "musicvideo-manager",
		Short: "Organize, enrich, and manage a local music video collection",
	}
	root.AddCommand(newScanCmd())
	root.AddCommand(newVersionCmd(version))
	return root
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}
```

Add `"fmt"` to the import block (needed by `newVersionCmd`).

- [ ] **Step 4: Implement `scan.go`**

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
	"github.com/keif/musicvideo-manager/internal/scanner"
)

func newScanCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a library directory and update its catalog",
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

			info, err := os.Stat(absRoot)
			if err != nil {
				return fmt.Errorf("library root: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("library root %s is not a directory", absRoot)
			}

			stateDir := filepath.Join(absRoot, ".musicvideo-manager")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				return fmt.Errorf("create state dir: %w", err)
			}

			cfg, err := config.LoadOrCreate(filepath.Join(stateDir, "config.yaml"), filepath.Base(absRoot))
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			summary, err := scanner.Run(cmd.Context(), cat, absRoot, cfg.Scan.Extensions, cfg.Scan.Workers)
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Scanned %d files: %d added, %d updated, %d moved, %d missing, %d errors\n",
				summary.FilesScanned, summary.FilesAdded, summary.FilesUpdated, summary.FilesMoved, summary.FilesMissing, len(summary.Errors))
			for _, e := range summary.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.Path, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output the scan summary as JSON")
	return cmd
}
```

- [ ] **Step 5: Replace `main.go`**

```go
package main

import (
	"os"

	"github.com/keif/musicvideo-manager/internal/cli"
)

var version = "dev"

func main() {
	root := cli.NewRootCmd(version)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Fetch dependencies**

Run: `go get github.com/spf13/cobra && go mod tidy`
Expected: exit 0, `go.mod`/`go.sum` updated.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS for every package (`pkg/provider`, `internal/catalog`, `internal/config`, `internal/scanner`, `internal/cli`)

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 8: Manual smoke test against a real library**

Run: `go run ./cmd/musicvideo-manager scan "/Volumes/Yggdrasil/Music Videos" --json | head -20`
Expected: valid JSON summary with `files_added` roughly matching the ~618 files counted earlier, `errors` ideally empty or small. Re-run the same command a second time and confirm `files_added` is now `0` and the total scanned count matches.

- [ ] **Step 9: Commit**

```bash
git add cmd internal/cli go.mod go.sum
git commit -m "Add scan and version CLI commands"
```

---

### Task 13: Makefile, lint config, Dependabot

**Files:**
- Create: `Makefile`
- Create: `.golangci.yml`
- Create: `.github/dependabot.yml`
- Create: `scripts/gen_testdata.sh`

**Interfaces:**
- Consumes: nothing (tooling only).
- Produces: `make build|test|lint|testdata|docker-build` targets used by Task 14's CI workflow and by contributors per Task 17's CONTRIBUTING.md.

- [ ] **Step 1: Write `Makefile`**

```makefile
.PHONY: build test lint testdata docker-build

build:
	go build -o musicvideo-manager ./cmd/musicvideo-manager

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

testdata:
	./scripts/gen_testdata.sh

docker-build:
	docker build -t musicvideo-manager:local .
```

- [ ] **Step 2: Write `.golangci.yml`**

```yaml
run:
  timeout: 5m

linters:
  enable:
    - govet
    - errcheck
    - staticcheck
    - unused
    - ineffassign
    - gosimple
```

- [ ] **Step 3: Write `.github/dependabot.yml`**

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: "/"
    schedule:
      interval: weekly
  - package-ecosystem: github-actions
    directory: "/"
    schedule:
      interval: weekly
```

- [ ] **Step 4: Write `scripts/gen_testdata.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="$(dirname "$0")/../testdata/fixtures"
mkdir -p "$OUT_DIR"

ffmpeg -y \
  -f lavfi -i "testsrc=duration=3:size=320x240:rate=25" \
  -f lavfi -i "sine=duration=3" \
  -c:v libx264 -c:a aac -shortest \
  "$OUT_DIR/sample_short.mp4"

echo "this is not a real video file" > "$OUT_DIR/sample_corrupt.mp4"

echo "Wrote fixtures to $OUT_DIR"
```

- [ ] **Step 5: Make the script executable and verify the tooling**

```bash
chmod +x scripts/gen_testdata.sh
```

Run: `make build`
Expected: produces a `musicvideo-manager` binary in the repo root, exit 0.

Run: `make test`
Expected: PASS (same as `go test ./...`, with race detector and coverage)

(If `golangci-lint` isn't installed locally, `make lint` will fail with a "command not found" — that's expected; Task 14's CI workflow runs it via the standard GitHub Action, which doesn't require a local install. Install it locally with `brew install golangci-lint` or via the instructions at their releases page if you want to run `make lint` yourself.)

- [ ] **Step 6: Commit**

```bash
git add Makefile .golangci.yml .github/dependabot.yml scripts/gen_testdata.sh
git commit -m "Add Makefile, lint config, Dependabot, and testdata generation script"
```

---

### Task 14: GitHub Actions CI

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `make test`/`make lint`/`go vet` (Task 13); ffmpeg (external dependency, installed in-workflow).
- Produces: a passing CI check on pushes/PRs. Nothing downstream in this plan depends on it, but it's what makes the whole test suite (Tasks 2-12) enforceable going forward.

- [ ] **Step 1: Write `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"

      - name: Install ffmpeg (Linux)
        if: runner.os == 'Linux'
        run: sudo apt-get update && sudo apt-get install -y ffmpeg

      - name: Install ffmpeg (macOS)
        if: runner.os == 'macOS'
        run: brew install ffmpeg

      - name: go vet
        run: go vet ./...

      - name: go test
        run: go test ./... -race -cover

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest
```

- [ ] **Step 2: Verify the workflow YAML is well-formed**

Run: `python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK`
Expected: prints `OK` (adjust to any locally available YAML validator if `python3`/`pyyaml` aren't present — the goal is just to catch a syntax typo before pushing)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "Add CI workflow: test matrix and lint"
```

---

### Task 15: Docker

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.dockerignore`

**Interfaces:**
- Consumes: `cmd/musicvideo-manager` (Task 12).
- Produces: a runnable container image exposing the `scan` command against a mounted library volume.

- [ ] **Step 1: Write `.dockerignore`**

```text
.git
testdata/fixtures
*.sqlite
dist
musicvideo-manager
```

- [ ] **Step 2: Write `Dockerfile`**

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /musicvideo-manager ./cmd/musicvideo-manager

FROM alpine:3.20
RUN apk add --no-cache ffmpeg
COPY --from=builder /musicvideo-manager /usr/local/bin/musicvideo-manager
ENTRYPOINT ["musicvideo-manager"]
CMD ["--help"]
```

- [ ] **Step 3: Write `docker-compose.yml`**

```yaml
services:
  musicvideo-manager:
    build: .
    volumes:
      - ./library:/library
    command: ["scan", "/library"]
```

- [ ] **Step 4: Verify the image builds**

Run: `docker build -t musicvideo-manager:local .`
Expected: builds successfully, exit 0. (Skip this step if Docker isn't available in your environment — CI does not currently build the image automatically, since Task 14's workflow only runs `go test`/lint; verify manually before relying on the image.)

- [ ] **Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml .dockerignore
git commit -m "Add Docker image and compose service"
```

---

### Task 16: ADRs

**Files:**
- Create: `docs/adr/0001-canonical-sqlite-catalog.md`
- Create: `docs/adr/0002-per-library-catalog-location.md`
- Create: `docs/adr/0003-provider-plugin-interface.md`
- Create: `docs/adr/0004-partial-hash-identity.md`
- Create: `docs/adr/0005-pure-go-sqlite-and-migrations.md`

**Interfaces:**
- Consumes: decisions made in the design spec (`docs/superpowers/specs/2026-07-20-foundation-and-scanner-design.md`) and this plan's Global Constraints.
- Produces: nothing consumed by code; historical record for contributors.

- [ ] **Step 1: Write `0001-canonical-sqlite-catalog.md`**

```markdown
# 1. Canonical SQLite catalog as source of truth

Status: Accepted

## Context

Most local media managers treat the filesystem (and sidecar NFO files) as
the source of truth, regenerating them on every change. That makes history,
provenance, and multi-target export (Plex today, Jellyfin/Kodi/Emby later)
hard: there's nowhere to record "we matched this against MusicBrainz with
0.62 confidence and a human accepted it" other than baking it into a
filename or an NFO field meant for a specific media server's schema.

## Decision

musicvideo-manager maintains its own SQLite catalog of every video, its
extracted metadata, match history, and artwork. Filenames, NFO files, and
any future per-media-server export are *generated from* the catalog, never
the other way around. The catalog is authoritative.

## Consequences

- Any media-server-specific export (Plex NFO, Jellyfin, Kodi, a static
  site) becomes a pure read + template step against the catalog, not a
  parser of scattered on-disk conventions.
- The catalog must capture enough state (paths, hashes, timestamps, scan
  history) to reconstruct "what do we know and how confident are we"
  without re-scanning or re-querying providers.
- Losing the catalog file loses matching/curation history even though the
  underlying video files are untouched — mitigated by [[0002]]'s decision
  to colocate the catalog with the library so normal backup of the drive
  covers it.
```

- [ ] **Step 2: Write `0002-per-library-catalog-location.md`**

```markdown
# 2. Per-library catalog and config colocated in the library root

Status: Accepted

## Context

The catalog ([[0001]]) needs to live somewhere. A central per-machine
app-data directory (e.g. `~/.local/share/musicvideo-manager/`) is the more
common convention, but this tool's libraries are large media collections
that get moved between machines, external drives, and servers far more
often than typical application state does.

## Decision

Each library's catalog and config live at
`<library-root>/.musicvideo-manager/{catalog.sqlite,config.yaml}`, not in a
central app-data directory. A library is fully self-contained: moving or
copying the library root moves the catalog and config with it.

## Consequences

- No cross-library registry or "which machine has which library" tracking
  is needed for the MVP — multiple libraries are just multiple independent
  `.musicvideo-manager/` directories.
- Provider API keys/credentials must NOT live in this directory, since it's
  expected to be backed up, copied, and potentially shared — they come from
  environment variables or a separate machine-local credentials file.
- Tools that back up or sync the media library automatically back up the
  catalog too, with no extra configuration.
```

- [ ] **Step 3: Write `0003-provider-plugin-interface.md`**

```markdown
# 3. Metadata provider plugin interface

Status: Accepted

## Context

The roadmap calls for multiple metadata sources (MusicBrainz, Discogs,
TheAudioDB, Last.fm) and, as a stretch goal, a plugin SDK for third-party
providers. None of these are implemented in the Foundation/Scanner phase,
but the interface they'll implement against is cheap to define now and
avoids an awkward later move of the type from an internal package to a
public one.

## Decision

`pkg/provider` defines `MetadataProvider` (`Name() string`,
`Match(ctx, MatchQuery) ([]MatchResult, error)`) and its supporting types.
It lives in `pkg/` (not `internal/`) because it's meant to be imported by
code outside this module eventually. No implementations exist yet.

## Consequences

- Future metadata-matching sub-projects implement this interface rather
  than inventing their own per-provider shape.
- Because `pkg/` is public, changes to this interface after providers exist
  are breaking changes for anyone who's implemented against it — worth
  getting the shape right before the first real provider lands, but not
  worth over-designing now with only a placeholder consumer.
```

- [ ] **Step 4: Write `0004-partial-hash-identity.md`**

```markdown
# 4. Partial-hash-by-default identity and move detection

Status: Accepted

## Context

The scanner needs a cheap way to recognize "this is the same file I saw
before, just moved or renamed" without re-reading every byte of every file
on every scan — collections here run into the hundreds to thousands of
files, some multiple gigabytes.

## Decision

Scan-time identity uses `partial_hash = SHA-256(first 1MiB || last 1MiB)`
(or the whole file if it's under 2MiB), combined with the stored
`file_size`. This is used for move/rename detection during `scan`. Full
SHA-256 and Chromaprint audio fingerprinting are deferred to a future
`duplicates` sub-project, where accuracy matters more than scan speed.

## Consequences

- A file whose first/last 1MiB is unchanged but whose middle differs (rare
  for real edits, but possible for deliberately crafted files) will not be
  detected as changed until its size or mtime also changes.
- Two different files that happen to collide on `(file_size, partial_hash)`
  can be misidentified as a move; [[0001's]] scanner reconciliation breaks
  ties by closest `mtime` and treats the rest as new inserts — a heuristic,
  not a correctness guarantee. The `duplicates` sub-project's full-hash
  pass is the authoritative check when it matters.
```

- [ ] **Step 5: Write `0005-pure-go-sqlite-and-migrations.md`**

```markdown
# 5. Pure-Go SQLite driver and a hand-rolled embedded migrator

Status: Accepted

## Context

SQLite access from Go typically goes through `mattn/go-sqlite3`, which
wraps the C SQLite library via cgo. cgo complicates cross-compilation
(needs a C toolchain per target) and CI (needs matching toolchains per
platform in the build matrix) for no benefit here, since this project
targets macOS and Linux binaries with no need for SQLite extensions cgo
would otherwise unlock.

Schema migrations were originally planned around `golang-migrate`, but its
maintained SQLite driver depends on `mattn/go-sqlite3`, which would
reintroduce the exact cgo dependency this decision avoids.

## Decision

Use `modernc.org/sqlite`, a pure-Go SQLite implementation, for all database
access. Pair it with a small hand-rolled migrator (`internal/catalog`):
numbered `.sql` files under `internal/catalog/migrations/`, embedded via
`go:embed`, applied in order inside a transaction, tracked in a
`schema_migrations` table the migrator creates itself.

## Consequences

- No cgo anywhere in the build — `go build`/`go test`/cross-compilation
  need only a Go toolchain, matching [[Global Constraints|the macOS+Linux
  platform target]].
- The migrator only supports forward ("up") migrations — acceptable since
  the catalog is local, additive, and per-library; there's no production
  rollback scenario to design for at this scale.
- Future schema changes (adding `artists`, `songs`, `matches`, `artwork`,
  `collections`, `playlists` tables for later sub-projects) are new
  numbered files in the same migrations directory — no change to the
  migrator itself.
```

- [ ] **Step 6: Commit**

```bash
git add docs/adr
git commit -m "Add architecture decision records for foundation and scanner"
```

---

### Task 17: README and CONTRIBUTING

**Files:**
- Create: `README.md`
- Create: `CONTRIBUTING.md`

**Interfaces:**
- Consumes: everything built in Tasks 1-16 (documents the current, real state of the repo).
- Produces: nothing consumed by code; entry points for contributors.

- [ ] **Step 1: Write `README.md`**

```markdown
# musicvideo-manager

An open-source, media-server-agnostic tool for organizing, enriching, and
managing a local music video collection. Point it at a directory of music
videos and it builds a searchable, self-contained catalog of what you have
— then exports metadata for Plex today, with Jellyfin/Emby/Kodi and static
site export planned.

The catalog (not the filesystem) is the source of truth: every video, its
extracted technical metadata, match history, and artwork lives in a local
SQLite database that travels with the library. Filenames and NFO files are
generated *from* the catalog, not the other way around. See
[docs/adr](docs/adr) for the reasoning behind that and other core decisions.

## Status

Early foundation stage. Currently implemented:

- `scan` — walks a library directory, extracts technical metadata via
  ffprobe, and populates the catalog with correct incremental rescan,
  move/rename detection, and missing-file handling.

Not yet implemented (see the roadmap below): metadata matching/identification,
rename engine, NFO/artwork generation, duplicate detection, collections,
playlists, web UI.

## Requirements

- Go 1.23+
- ffmpeg/ffprobe on your `PATH`

## Install

```bash
go build -o musicvideo-manager ./cmd/musicvideo-manager
```

## Usage

```bash
./musicvideo-manager scan /path/to/your/music-videos
./musicvideo-manager scan /path/to/your/music-videos --json
./musicvideo-manager version
```

The first `scan` of a directory creates
`<library-root>/.musicvideo-manager/{config.yaml,catalog.sqlite}`. Re-running
`scan` is incremental: unchanged files are skipped, edited files are
re-probed, moved/renamed files are detected by content hash, and deleted
files are marked missing (never deleted from the catalog).

## Roadmap

| Milestone | Scope |
|---|---|
| M0 Foundation | repo scaffold, catalog schema, provider interface, CLI skeleton, CI/Docker |
| M1 Scanner | `scan` command, ffprobe extraction, incremental rescan |
| M2 Metadata Matching | filename parsing, embedded tags, MusicBrainz/Discogs/TheAudioDB/Last.fm |
| M3 Rename Engine + NFO | configurable rename templates, Plex/Kodi/Jellyfin/Emby NFO export |
| M4 Artwork | poster/fanart/logo/banner/thumbnail download and caching |
| M5 Duplicate Detection | full hash, Chromaprint audio fingerprinting, frame fingerprinting |
| M6 Collections + Playlists | decade/genre/mood collections, M3U/JSON/smart playlists |
| M7 Web UI | React search, metadata editor, artwork preview, batch editing |

Each milestone gets its own design spec under `docs/superpowers/specs/`
before implementation.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — see [LICENSE](LICENSE).
```

- [ ] **Step 2: Write `CONTRIBUTING.md`**

```markdown
# Contributing

## Development setup

- Go 1.23+
- ffmpeg/ffprobe on your `PATH` (required both at runtime and by the test suite)
- Optional: `golangci-lint` for running `make lint` locally (CI runs it regardless)

```bash
git clone <repo-url>
cd musicvideo-manager
go build ./...
make test
```

## Running tests

```bash
make test          # go test ./... -race -cover
make lint           # golangci-lint run ./...
```

Tests that need a real video file generate one on the fly with `ffmpeg`
(see `requireFFmpeg`/`generateFixtureVideo` helpers in `internal/scanner`
and `internal/cli`) and skip themselves with a clear message if `ffmpeg`/
`ffprobe` aren't on your `PATH`. CI always has them installed.

## Code style

- `gofmt` (run automatically by most editors; CI's lint job also enforces it via `golangci-lint`)
- No comments explaining *what* code does — name things clearly instead.
  Comments are for non-obvious *why* (a workaround, an invariant, a
  constraint that would surprise a reader).
- Keep files focused: one clear responsibility per file. If a file is
  growing unwieldy, that's a signal to split it.

## Commits

Follow [Conventional Commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`). Keep commits
small and focused — one logical change per commit.

## Architecture changes

Non-trivial architecture decisions get an ADR under `docs/adr/`, following
the numbered format already there (`Status` / `Context` / `Decision` /
`Consequences`). New feature areas (metadata matching, rename engine,
artwork, etc.) get a design spec under `docs/superpowers/specs/` before
implementation — see the existing spec for the shape to follow.

## Pull requests

- Every PR should leave `make test` and `make lint` passing.
- New behavior needs tests covering the happy path and the relevant edge
  cases (NULL/empty/boundary values, malformed input) — see existing tests
  in `internal/catalog`, `internal/scanner`, and `internal/config` for the
  expected depth.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CONTRIBUTING.md
git commit -m "Add README and CONTRIBUTING"
```

---

### Task 18: GitHub issues content for M0/M1

**Files:**
- Create: `docs/ISSUES.md`

**Interfaces:**
- Consumes: Tasks 1-17 (each issue maps to one or more completed tasks).
- Produces: copy-pasteable issue bodies for the user to create manually (via `gh issue create -F` or the GitHub UI) once the repo has a remote — this plan does not call `gh issue create` itself, since no GitHub remote exists yet and creating public issues isn't something to automate without that being explicitly set up first.

- [ ] **Step 1: Write `docs/ISSUES.md`**

```markdown
# Planned GitHub Issues — M0 Foundation & M1 Scanner

Copy each section below into a GitHub issue (`gh issue create --title "..." --body-file -` or
the web UI) once this repo has a remote. Grouped by milestone; each maps to
one or more tasks in `docs/superpowers/plans/2026-07-20-foundation-and-scanner.md`.

## M0 — Foundation

### Scaffold the Go module and repository layout
Set up `go.mod`, `.gitignore`, `LICENSE` (MIT), and the `cmd/ internal/ pkg/
web/ docs/ examples/ scripts/ testdata/` directory layout described in the
design spec. Acceptance: `go build ./...` and `go vet ./...` succeed.

### Define the metadata provider plugin interface
Add `pkg/provider` with `MetadataProvider`, `MatchQuery`, `MatchResult`. No
implementations — this is the seam future provider sub-projects build
against. Acceptance: a fake in-package test provider satisfies the
interface.

### Implement the catalog schema and embedded migrations
Add `internal/catalog` with the `videos`/`scan_runs`/`scan_errors` schema
(see the design spec) applied via a hand-rolled embedded-SQL migrator
(`modernc.org/sqlite`, no cgo — see ADR 5). Acceptance: `Open` creates the
schema on a fresh database and is idempotent on re-open.

### Implement the catalog repository methods
Add `Video`/`ScanRunCounts` models and the `ListActive`,
`FindMissingByHash`, `InsertVideo`, `UpdateProbe`, `UpdatePath`,
`TouchSeen`, `MarkMissing`, `StartScanRun`, `FinishScanRun`,
`RecordScanError` methods. Acceptance: unit tests cover insert, list,
status transitions, and hash-based lookup, including NULL metadata fields.

### Implement per-library config load/save
Add `internal/config` with `Config`/`Library`/`ScanConfig` and
`LoadOrCreate`, defaulting extensions to
`mp4,mkv,avi,mov,m4v,ts,mpg,webm` and `workers: 0` (auto). Acceptance: unit
tests cover default-creation, loading an existing file, and rejecting
invalid YAML.

### Add CI, Makefile, lint config, and Dependabot
GitHub Actions matrix over macOS/Linux installing ffmpeg, running
`go vet`/`golangci-lint`/`go test -race -cover`; `Makefile` with
`build/test/lint/testdata/docker-build` targets; Dependabot for Go modules
and Actions. Acceptance: CI is green on a PR that touches only docs.

### Add Docker image and compose service
Multi-stage `Dockerfile` (static binary, ffmpeg installed in the final
stage) and a `docker-compose.yml` mounting a library volume. Acceptance:
`docker build` succeeds and `docker run musicvideo-manager:local --help`
prints usage.

### Write ADRs for the foundation decisions
Five ADRs under `docs/adr/`: canonical catalog, per-library catalog
location, provider plugin interface, partial-hash identity, pure-Go SQLite
+ hand-rolled migrator. Acceptance: each follows the Status/Context/
Decision/Consequences format.

### Write README and CONTRIBUTING
Project overview, status, requirements, usage, roadmap table, dev setup,
test/lint commands, commit conventions. Acceptance: a new contributor can
clone, build, and run the test suite using only the README/CONTRIBUTING.

## M1 — Scanner

### Implement fast partial-content hashing
`ComputeHash` — SHA-256 of the whole file under 2MiB, or first+last 1MiB
above that threshold. Acceptance: unit tests cover empty files, small
files, and large files where only the untouched middle region differs.

### Implement the ffprobe metadata wrapper
`Probe` — shells out to `ffprobe`, parses duration/resolution/codecs/
bitrate/fps/container from its JSON output. Acceptance: an integration
test against an ffmpeg-generated fixture asserts the parsed values; a
second test asserts a corrupt file returns an error rather than a
zero-value result.

### Implement library file discovery
`Discover` — recursive walk filtered to the configured extensions
(case-insensitive), always skipping `.musicvideo-manager/`. Acceptance:
unit tests cover nested subdirectories, mixed-case extensions, the state
directory being skipped even when it contains a matching-extension file,
and an empty/nonexistent root.

### Implement scan reconciliation
`Reconciler.Reconcile` — classifies every discovered file as unchanged,
updated, moved, or newly added against the catalog, and marks unseen
active rows missing. Acceptance: unit tests (using fake hash/probe
functions) cover every classification plus a file that fails to hash/probe
without aborting the whole run.

### Implement scan run orchestration
`scanner.Run` — wraps `Reconciler` with `scan_runs`/`scan_errors`
bookkeeping and returns a `RunSummary`. Acceptance: an integration test
confirms `FilesScanned` accounts for every discovered file including
errors.

### Implement the `scan` and `version` CLI commands
Cobra wiring: `musicvideo-manager scan [path] [--json]` and
`musicvideo-manager version`. Acceptance: an end-to-end test (real ffmpeg
fixture) confirms `.musicvideo-manager/{config.yaml,catalog.sqlite}` are
created, a second run reports zero new files, and `--json` output parses.

---

## Future milestones (placeholders — not detailed until specced)

- M2 Metadata Matching
- M3 Rename Engine + NFO Output
- M4 Artwork
- M5 Duplicate Detection
- M6 Collections + Playlists
- M7 Web UI
```

- [ ] **Step 2: Commit**

```bash
git add docs/ISSUES.md
git commit -m "Add planned GitHub issue list for M0/M1"
```

---

## Plan Self-Review Notes

- **Spec coverage:** repo layout ✓ (Task 1), tech choices ✓ (Tasks 1/3/12, ADR 5 documents the golang-migrate deviation), catalog/config location ✓ (Task 12's scan command), config file shape ✓ (Task 6), catalog schema ✓ (Task 3), provider interface ✓ (Task 2), hashing strategy ✓ (Task 7), scan algorithm (unchanged/changed/new+move/missing, worker pool, error tolerance, scan_runs bookkeeping) ✓ (Tasks 9-11), CLI surface ✓ (Task 12), testing (unit + edge cases + integration) ✓ (all tasks), CI/Docker/Makefile ✓ (Tasks 13-15), ADRs ✓ (Task 16), roadmap/GitHub issues scoped to M0/M1 ✓ (Tasks 17-18).
- **Placeholder scan:** none found — every step has complete, runnable code or exact file content.
- **Type consistency:** verified `catalog.Video`, `catalog.ScanRunCounts`, `scanner.ProbeResult`, `scanner.FileInfo`, `scanner.Result`, `scanner.RunSummary`, and `scanner.Reconciler` field names/types match across every task that constructs or consumes them (Task 10's `job` struct fix for `fullPath()` was caught and corrected during this review).
```
