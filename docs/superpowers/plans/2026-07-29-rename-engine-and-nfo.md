# Rename Engine & Kodi NFO Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Configurable, templated file renaming (dry-run/apply/undo) for confirmed matches, plus Kodi-format `<basename>.nfo`/`artist.nfo` sidecar generation.

**Architecture:** `internal/rename` computes and applies renames against `internal/catalog`'s new rename-history tables; `internal/nfo` marshals Kodi's XML schema and writes sidecar files, independent of rename. Both are driven by two new `internal/cli` commands.

**Tech Stack:** Go, `encoding/xml` (standard library, no new dependency), the existing `modernc.org/sqlite` catalog, Cobra CLI — same stack as the prior two milestones.

## Global Constraints

- Module path: `github.com/keif/musicvideo-manager`.
- No cgo.
- No AI/model attribution anywhere in code, commits, or comments — every commit message plain and descriptive, no "Co-Authored-By" or similar trailer.
- Only videos with `matches.status = 'confirmed'` are eligible for rename or NFO generation.
- Default rename template is exactly `{artist}/{title}` — never `{album}`/`{year}` by default, since MusicBrainz's returned album is often not the canonical studio album.
- `rename_history`/`rename_runs` rows are never deleted, only marked `'undone'` — matches the project's existing "never destroy history" pattern (`videos.status`, `matches` rows).
- A rename collision (two videos computing the same target path) is detected and both are excluded from the applied set — never auto-resolved, never silently overwritten.
- **Safety:** the real-library verification step in this plan (Task 12) runs `rename --dry-run` only. Neither `rename` (without `--dry-run`) nor `nfo` is run for real against `/Volumes/Yggdrasil/Music Videos` as part of implementing this plan — both mutate the real filesystem and require a separate, explicit decision after this plan's tasks are done and reviewed.
- List-returning catalog methods must initialize to an empty slice, not leave a `var` nil — a nil slice JSON-encodes as `null` instead of `[]`, a defect already found and deferred in the prior milestone's `ListNeedsReview`. Don't reproduce it here.

---

### Task 1: `internal/catalog` — rename run tracking

**Files:**
- Create: `internal/catalog/migrations/0003_rename.sql`
- Create: `internal/catalog/renamerun.go`
- Test: `internal/catalog/renamerun_test.go`

**Interfaces:**
- Consumes: `catalog.Open`/migration runner (already generic — a new `NNNN_*.sql` file is auto-discovered and applied; no changes to `catalog.go` needed).
- Produces: `catalog.RenameRunStatus` type + consts `RenameRunStatusApplied`/`RenameRunStatusUndone`, `catalog.RenameHistoryEntry` struct, `(*Catalog) StartRenameRun(startedAt int64) (int64, error)`, `(*Catalog) FinishRenameRun(id int64, filesRenamed int, finishedAt int64) error`, `(*Catalog) RecordRenameHistory(renameRunID, videoID int64, oldPath, newPath string) error`, `(*Catalog) GetLastAppliedRenameRunID() (int64, bool, error)`, `(*Catalog) ListRenameHistory(renameRunID int64) ([]RenameHistoryEntry, error)`, `(*Catalog) MarkRenameRunUndone(id int64) error`. Consumed by `internal/rename` (Tasks 4-5).

- [ ] **Step 1: Write the migration SQL**

```sql
-- internal/catalog/migrations/0003_rename.sql
CREATE TABLE rename_runs (
    id            INTEGER PRIMARY KEY,
    started_at    INTEGER NOT NULL,
    finished_at   INTEGER,
    files_renamed INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'applied'
);

CREATE TABLE rename_history (
    id             INTEGER PRIMARY KEY,
    rename_run_id  INTEGER NOT NULL REFERENCES rename_runs(id),
    video_id       INTEGER NOT NULL REFERENCES videos(id),
    old_path       TEXT NOT NULL,
    new_path       TEXT NOT NULL
);
```

- [ ] **Step 2: Write the failing test**

```go
// internal/catalog/renamerun_test.go
package catalog_test

import "testing"

func TestRenameRunLifecycle(t *testing.T) {
	cat := openTestCatalog(t)

	id, err := cat.StartRenameRun(100)
	if err != nil {
		t.Fatalf("StartRenameRun: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	if err := cat.RecordRenameHistory(id, 1, "old/path.mp4", "new/path.mp4"); err != nil {
		t.Fatalf("RecordRenameHistory: %v", err)
	}
	if err := cat.RecordRenameHistory(id, 2, "old/other.mp4", "new/other.mp4"); err != nil {
		t.Fatalf("RecordRenameHistory: %v", err)
	}

	if err := cat.FinishRenameRun(id, 2, 200); err != nil {
		t.Fatalf("FinishRenameRun: %v", err)
	}

	gotID, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if !ok || gotID != id {
		t.Fatalf("expected last applied run %d, got %d (ok=%v)", id, gotID, ok)
	}

	history, err := cat.ListRenameHistory(id)
	if err != nil {
		t.Fatalf("ListRenameHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].OldPath != "old/path.mp4" || history[0].NewPath != "new/path.mp4" {
		t.Fatalf("unexpected first entry: %+v", history[0])
	}

	if err := cat.MarkRenameRunUndone(id); err != nil {
		t.Fatalf("MarkRenameRunUndone: %v", err)
	}

	_, ok, err = cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID after undo: %v", err)
	}
	if ok {
		t.Fatal("expected no applied run after marking undone")
	}
}

func TestGetLastAppliedRenameRunIDNoneExists(t *testing.T) {
	cat := openTestCatalog(t)

	_, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when no rename run exists")
	}
}

func TestListRenameHistoryEmpty(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.StartRenameRun(100)
	if err != nil {
		t.Fatalf("StartRenameRun: %v", err)
	}

	history, err := cat.ListRenameHistory(id)
	if err != nil {
		t.Fatalf("ListRenameHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 history entries, got %d", len(history))
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/catalog/... -run TestRenameRun -v`
Expected: FAIL — `undefined: catalog...StartRenameRun` (or similar)

- [ ] **Step 4: Implement `renamerun.go`**

```go
package catalog

import "database/sql"

// RenameRunStatus is the lifecycle state of an applied rename batch.
type RenameRunStatus string

const (
	RenameRunStatusApplied RenameRunStatus = "applied"
	RenameRunStatusUndone  RenameRunStatus = "undone"
)

// RenameHistoryEntry is a single file move within a rename run.
type RenameHistoryEntry struct {
	ID          int64
	RenameRunID int64
	VideoID     int64
	OldPath     string
	NewPath     string
}

// StartRenameRun records the start of a rename apply and returns its ID.
func (c *Catalog) StartRenameRun(startedAt int64) (int64, error) {
	res, err := c.db.Exec(`INSERT INTO rename_runs (started_at) VALUES (?)`, startedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRenameRun records the file count and completion time for a rename run.
func (c *Catalog) FinishRenameRun(id int64, filesRenamed int, finishedAt int64) error {
	_, err := c.db.Exec(`UPDATE rename_runs SET finished_at=?, files_renamed=? WHERE id=?`, finishedAt, filesRenamed, id)
	return err
}

// RecordRenameHistory logs a single file move within a rename run.
func (c *Catalog) RecordRenameHistory(renameRunID, videoID int64, oldPath, newPath string) error {
	_, err := c.db.Exec(`INSERT INTO rename_history (rename_run_id, video_id, old_path, new_path) VALUES (?, ?, ?, ?)`,
		renameRunID, videoID, oldPath, newPath)
	return err
}

// GetLastAppliedRenameRunID returns the ID of the most recent rename run
// with status = 'applied', or ok=false if none exists.
func (c *Catalog) GetLastAppliedRenameRunID() (int64, bool, error) {
	var id int64
	err := c.db.QueryRow(`SELECT id FROM rename_runs WHERE status = 'applied' ORDER BY id DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// ListRenameHistory returns every history entry for a rename run, in the
// order they were recorded.
func (c *Catalog) ListRenameHistory(renameRunID int64) ([]RenameHistoryEntry, error) {
	rows, err := c.db.Query(`SELECT id, rename_run_id, video_id, old_path, new_path FROM rename_history WHERE rename_run_id = ? ORDER BY id`, renameRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RenameHistoryEntry{}
	for rows.Next() {
		var e RenameHistoryEntry
		if err := rows.Scan(&e.ID, &e.RenameRunID, &e.VideoID, &e.OldPath, &e.NewPath); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkRenameRunUndone flips a rename run's status to 'undone' so it is
// never targeted by a future undo.
func (c *Catalog) MarkRenameRunUndone(id int64) error {
	_, err := c.db.Exec(`UPDATE rename_runs SET status = 'undone' WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (new rename-run tests plus every pre-existing catalog test — confirming migration 0003 doesn't break anything)

- [ ] **Step 6: Commit**

```bash
git add internal/catalog
git commit -m "Add rename run tracking schema and repository methods"
```

---

### Task 2: `internal/catalog` — video path update + confirmed match query

**Files:**
- Modify: `internal/catalog/video.go`
- Modify: `internal/catalog/match.go`
- Test: `internal/catalog/video_test.go`
- Test: `internal/catalog/match_test.go`

**Interfaces:**
- Consumes: `videoColumns`/`scanVideo` conventions (already in `video.go`), `matches`/`videos`/`songs`/`artists` tables (Tasks 2-4 of the prior milestone).
- Produces: `(*Catalog) UpdateVideoPath(id int64, newPath string) error`, `catalog.ConfirmedMatch` struct, `(*Catalog) ListConfirmedMatches() ([]ConfirmedMatch, error)`. Consumed by `internal/rename` (Task 4) and `internal/nfo` (Task 7).

- [ ] **Step 1: Write the failing tests**

Append to `internal/catalog/video_test.go`:

```go
func TestUpdateVideoPathChangesOnlyPath(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "old/Path.mp4", FileSize: 10, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	if err := cat.UpdateVideoPath(id, "new/Path.mp4"); err != nil {
		t.Fatalf("UpdateVideoPath: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active video, got %d", len(active))
	}
	got := active[0]
	if got.Path != "new/Path.mp4" {
		t.Fatalf("expected path new/Path.mp4, got %q", got.Path)
	}
	if got.MTime != 1 || got.LastSeenAt != 1 || got.LastScannedAt != 1 {
		t.Fatalf("expected mtime/last_seen_at/last_scanned_at unchanged, got %+v", got)
	}
}
```

Append to `internal/catalog/match_test.go`:

```go
func TestListConfirmedMatches(t *testing.T) {
	cat := openTestCatalog(t)

	confirmedID := insertTestVideo(t, cat, "Artist/Confirmed.mp4")
	reviewID := insertTestVideo(t, cat, "Artist/Review.mp4")

	artistID, err := cat.UpsertArtist("Artist", "artist-mbid", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Title", "Album", 2001, "song-mbid", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}

	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: confirmedID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch confirmed: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: reviewID, Provider: "musicbrainz", Confidence: 0.4, Status: catalog.MatchStatusNeedsReview, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch review: %v", err)
	}

	confirmed, err := cat.ListConfirmedMatches()
	if err != nil {
		t.Fatalf("ListConfirmedMatches: %v", err)
	}
	if len(confirmed) != 1 {
		t.Fatalf("expected 1 confirmed match, got %d: %+v", len(confirmed), confirmed)
	}
	got := confirmed[0]
	if got.VideoID != confirmedID || got.VideoPath != "Artist/Confirmed.mp4" {
		t.Fatalf("unexpected video fields: %+v", got)
	}
	if got.Artist != "Artist" || got.Title != "Title" || got.Album != "Album" || got.Year != 2001 {
		t.Fatalf("unexpected song/artist fields: %+v", got)
	}
}

func TestListConfirmedMatchesHandlesNullAlbumAndYear(t *testing.T) {
	cat := openTestCatalog(t)
	videoID := insertTestVideo(t, cat, "Artist/Song.mp4")

	artistID, err := cat.UpsertArtist("Artist", "", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Title", "", 0, "", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	confirmed, err := cat.ListConfirmedMatches()
	if err != nil {
		t.Fatalf("ListConfirmedMatches: %v", err)
	}
	if len(confirmed) != 1 {
		t.Fatalf("expected 1 confirmed match, got %d", len(confirmed))
	}
	if confirmed[0].Album != "" || confirmed[0].Year != 0 {
		t.Fatalf("expected empty album and zero year, got %+v", confirmed[0])
	}
}

func TestListConfirmedMatchesEmpty(t *testing.T) {
	cat := openTestCatalog(t)

	confirmed, err := cat.ListConfirmedMatches()
	if err != nil {
		t.Fatalf("ListConfirmedMatches: %v", err)
	}
	if len(confirmed) != 0 {
		t.Fatalf("expected 0 confirmed matches, got %d", len(confirmed))
	}
}
```

Both test files already have `"database/sql"` imported and `insertTestVideo`/`openTestCatalog` helpers defined (from the prior milestone's `match_test.go`) — reuse them, don't redefine.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/catalog/... -run "TestUpdateVideoPath|TestListConfirmedMatches" -v`
Expected: FAIL — `undefined: catalog...UpdateVideoPath` / `undefined: catalog.ConfirmedMatch`

- [ ] **Step 3: Implement the changes**

Append to `internal/catalog/video.go`:

```go
// UpdateVideoPath updates a video's path only, used by the rename engine.
// Unlike UpdatePath (scan reconciliation), this does not touch status or
// last_seen_at/last_scanned_at -- a rename isn't "seeing" the file for the
// first time, just relocating it.
func (c *Catalog) UpdateVideoPath(id int64, newPath string) error {
	_, err := c.db.Exec(`UPDATE videos SET path = ? WHERE id = ?`, newPath, id)
	return err
}
```

Append to `internal/catalog/match.go`:

```go
// ConfirmedMatch is a video with a confirmed identification, joined with
// its song and artist -- the input the rename engine and NFO generator need.
type ConfirmedMatch struct {
	VideoID   int64
	VideoPath string
	Artist    string
	Title     string
	Album     string
	Year      int
}

// ListConfirmedMatches returns every video with a confirmed match, joined
// with its song and artist, ordered by video path.
func (c *Catalog) ListConfirmedMatches() ([]ConfirmedMatch, error) {
	rows, err := c.db.Query(`SELECT v.id, v.path, a.name, s.title, s.album, s.year
		FROM matches m
		JOIN videos v ON v.id = m.video_id
		JOIN songs s ON s.id = m.song_id
		JOIN artists a ON a.id = s.artist_id
		WHERE m.status = 'confirmed'
		ORDER BY v.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ConfirmedMatch{}
	for rows.Next() {
		var cm ConfirmedMatch
		var album sql.NullString
		var year sql.NullInt64
		if err := rows.Scan(&cm.VideoID, &cm.VideoPath, &cm.Artist, &cm.Title, &album, &year); err != nil {
			return nil, err
		}
		cm.Album = album.String
		cm.Year = int(year.Int64)
		out = append(out, cm)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (every catalog test — this task completes the catalog-layer work for this plan)

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "Add video path update and confirmed match query"
```

---

### Task 3: `internal/rename` — template rendering

**Files:**
- Create: `internal/rename/template.go`
- Test: `internal/rename/template_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `rename.TemplateFields{Artist, Title, Album string, Year int}`, `rename.RenderTemplate(template string, fields TemplateFields) (string, error)`. Consumed by `internal/rename/rename.go` (Task 4).

- [ ] **Step 1: Write the failing test**

```go
// internal/rename/template_test.go
package rename_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/rename"
)

func TestRenderTemplateBasic(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{title}", rename.TemplateFields{Artist: "Bon Jovi", Title: "Wanted Dead or Alive"})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Bon Jovi/Wanted Dead or Alive" {
		t.Fatalf("unexpected result: %q", got)
	}
}

func TestRenderTemplateWithAlbumAndYear(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{year} - {album}/{title}", rename.TemplateFields{
		Artist: "Moby", Title: "Porcelain", Album: "Play", Year: 1999,
	})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Moby/1999 - Play/Porcelain" {
		t.Fatalf("unexpected result: %q", got)
	}
}

func TestRenderTemplateSanitizesIllegalCharacters(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{title}", rename.TemplateFields{Artist: "AC/DC", Title: `Who Made Who?`})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "ACDC/Who Made Who" {
		t.Fatalf("expected illegal characters stripped, got %q", got)
	}
}

func TestRenderTemplateCollapsesEmptySegments(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{album}/{title}", rename.TemplateFields{Artist: "Alice Cooper", Title: "Poison"})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Alice Cooper/Poison" {
		t.Fatalf("expected empty album segment collapsed, got %q", got)
	}
}

func TestRenderTemplateZeroYearOmitted(t *testing.T) {
	got, err := rename.RenderTemplate("{artist}/{year}/{title}", rename.TemplateFields{Artist: "Beck", Title: "Loser"})
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got != "Beck/Loser" {
		t.Fatalf("expected zero year segment collapsed, got %q", got)
	}
}

func TestRenderTemplateEmptyTemplateErrors(t *testing.T) {
	if _, err := rename.RenderTemplate("", rename.TemplateFields{Artist: "X", Title: "Y"}); err == nil {
		t.Fatal("expected error for empty template")
	}
}

func TestRenderTemplateAllEmptyAfterSanitizeErrors(t *testing.T) {
	if _, err := rename.RenderTemplate("{album}", rename.TemplateFields{Artist: "X", Title: "Y"}); err == nil {
		t.Fatal("expected error when the rendered path has no segments left")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/rename/... -v`
Expected: FAIL — `undefined: rename.RenderTemplate` (the `internal/rename` package doesn't exist yet)

- [ ] **Step 3: Implement `template.go`**

```go
package rename

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// TemplateFields are the values available to a rename template.
type TemplateFields struct {
	Artist string
	Title  string
	Album  string
	Year   int
}

var illegalChars = regexp.MustCompile(`[/\\:*?"<>|]`)

var placeholderPattern = regexp.MustCompile(`\{(artist|title|album|year)\}`)

// RenderTemplate substitutes placeholders in template with fields' values,
// sanitizing each value for filesystem safety and collapsing any resulting
// empty path segments. The returned path has no extension -- callers
// append the source file's own extension.
func RenderTemplate(template string, fields TemplateFields) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("empty rename template")
	}

	values := map[string]string{
		"artist": fields.Artist,
		"title":  fields.Title,
		"album":  fields.Album,
	}
	if fields.Year != 0 {
		values["year"] = strconv.Itoa(fields.Year)
	} else {
		values["year"] = ""
	}

	rendered := placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		key := match[1 : len(match)-1]
		return sanitize(values[key])
	})

	segments := strings.Split(rendered, "/")
	var kept []string
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			kept = append(kept, seg)
		}
	}
	if len(kept) == 0 {
		return "", fmt.Errorf("rendered template is empty for fields %+v", fields)
	}
	return filepath.Join(kept...), nil
}

func sanitize(value string) string {
	return strings.TrimSpace(illegalChars.ReplaceAllString(value, ""))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/rename/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rename
git commit -m "Add rename template rendering"
```

---

### Task 4: `internal/rename` — Renamer (plan and apply)

**Files:**
- Create: `internal/rename/rename.go`
- Test: `internal/rename/rename_test.go`

**Interfaces:**
- Consumes: `RenderTemplate`/`TemplateFields` (Task 3), `catalog.ListConfirmedMatches`/`UpdateVideoPath`/`StartRenameRun`/`FinishRenameRun`/`RecordRenameHistory` (Tasks 1-2).
- Produces: `rename.PlannedMove{VideoID, OldPath, NewPath}`, `rename.Conflict{NewPath, OldPaths}`, `rename.Plan{Moves, Unchanged, Conflicts}`, `rename.Result{FilesRenamed, Conflicts}`, `rename.Renamer{Catalog, Template, DryRun}`, `(*Renamer) Plan() (Plan, error)`, `(*Renamer) Apply(root string, plan Plan) (Result, error)`. Consumed by `internal/cli` (Task 9).

This task's tests use **real temporary directories with real fixture files** — never the real library — since `Apply` performs actual `os.Rename` calls.

- [ ] **Step 1: Write the failing test**

```go
// internal/rename/rename_test.go
package rename_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/rename"
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

func setupConfirmedVideo(t *testing.T, cat *catalog.Catalog, root, relPath, artist, title string) int64 {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	videoID, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: 5, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	artistID, err := cat.UpsertArtist(artist, "", 1)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, title, "", 0, "", 1)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sqlNullInt64(songID),
		Provider: "test", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
	return videoID
}

func TestPlanComputesMoveForConfirmedMatch(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Moves) != 1 {
		t.Fatalf("expected 1 planned move, got %d: %+v", len(plan.Moves), plan.Moves)
	}
	if plan.Moves[0].NewPath != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("unexpected new path: %q", plan.Moves[0].NewPath)
	}
	if plan.Unchanged != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanSkipsAlreadyCorrectPath(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Bon Jovi/Wanted Dead or Alive.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Moves) != 0 || plan.Unchanged != 1 {
		t.Fatalf("expected 0 moves and 1 unchanged, got %+v", plan)
	}
}

func TestPlanDetectsCollision(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Song/Song.mp4", "Same Artist", "Same Title")
	setupConfirmedVideo(t, cat, root, "Song/Song.ts", "Same Artist", "Same Title")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Conflicts) != 0 {
		// Different extensions (.mp4 vs .ts) mean different target paths;
		// this asserts the non-collision case explicitly so the real
		// collision test below is unambiguous.
		t.Fatalf("expected no conflict for differing extensions, got %+v", plan.Conflicts)
	}
}

func TestPlanDetectsRealCollision(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "A/Song.mp4", "Same Artist", "Same Title")
	setupConfirmedVideo(t, cat, root, "B/Song.mp4", "Same Artist", "Same Title")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %+v", len(plan.Conflicts), plan.Conflicts)
	}
	if len(plan.Moves) != 0 {
		t.Fatalf("expected 0 moves (both sides of the conflict excluded), got %+v", plan.Moves)
	}
}

func TestApplyMovesFileAndUpdatesCatalog(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := r.Apply(root, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.FilesRenamed != 1 {
		t.Fatalf("expected 1 file renamed, got %d", result.FilesRenamed)
	}

	if _, err := os.Stat(filepath.Join(root, "Bon Jovi", "Wanted Dead or Alive.mp4")); err != nil {
		t.Fatalf("expected renamed file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err == nil {
		t.Fatal("expected old file to no longer exist")
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != videoID || active[0].Path != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("expected catalog path updated, got %+v", active)
	}

	runID, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if !ok {
		t.Fatal("expected an applied rename run to exist")
	}
	history, err := cat.ListRenameHistory(runID)
	if err != nil {
		t.Fatalf("ListRenameHistory: %v", err)
	}
	if len(history) != 1 || history[0].OldPath != "Old Artist Name/Song-video.mp4" || history[0].NewPath != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("unexpected history: %+v", history)
	}
}

func TestApplyDryRunTouchesNothing(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}", DryRun: true}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := r.Apply(root, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.FilesRenamed != 0 {
		t.Fatalf("expected 0 files renamed in dry-run, got %d", result.FilesRenamed)
	}

	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err != nil {
		t.Fatalf("expected original file to still exist untouched: %v", err)
	}
	_, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if ok {
		t.Fatal("expected no rename run to be recorded for a dry run")
	}
}
```

Add a small test-only helper at the bottom of `rename_test.go`:

```go
import "database/sql"

func sqlNullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
```

(Add the `"database/sql"` import to the existing import block at the top of the file rather than as a second import statement.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/rename/... -run "TestPlan|TestApply" -v`
Expected: FAIL — `undefined: rename.Renamer`

- [ ] **Step 3: Implement `rename.go`**

```go
package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// Renamer computes and applies renames for confirmed matches.
type Renamer struct {
	Catalog  *catalog.Catalog
	Template string
	DryRun   bool
}

// PlannedMove is one video's computed rename.
type PlannedMove struct {
	VideoID int64  `json:"video_id"`
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// Conflict is a target path claimed by more than one video.
type Conflict struct {
	NewPath  string   `json:"new_path"`
	OldPaths []string `json:"old_paths"`
}

// Plan is the full outcome of computing renames for every confirmed match,
// before any filesystem write.
type Plan struct {
	Moves     []PlannedMove `json:"moves"`
	Unchanged int           `json:"unchanged"`
	Conflicts []Conflict    `json:"conflicts"`
}

// Result is the outcome of actually applying a Plan.
type Result struct {
	FilesRenamed int        `json:"files_renamed"`
	Conflicts    []Conflict `json:"conflicts"`
}

// Plan computes every confirmed match's target path and detects
// collisions, without touching the filesystem.
func (r *Renamer) Plan() (Plan, error) {
	matches, err := r.Catalog.ListConfirmedMatches()
	if err != nil {
		return Plan{}, fmt.Errorf("list confirmed matches: %w", err)
	}

	byTarget := map[string][]PlannedMove{}
	plan := Plan{Moves: []PlannedMove{}, Conflicts: []Conflict{}}

	for _, m := range matches {
		relStem, err := RenderTemplate(r.Template, TemplateFields{Artist: m.Artist, Title: m.Title, Album: m.Album, Year: m.Year})
		if err != nil {
			return Plan{}, fmt.Errorf("render template for video %d: %w", m.VideoID, err)
		}
		ext := filepath.Ext(m.VideoPath)
		newPath := relStem + ext

		if newPath == m.VideoPath {
			plan.Unchanged++
			continue
		}

		byTarget[newPath] = append(byTarget[newPath], PlannedMove{VideoID: m.VideoID, OldPath: m.VideoPath, NewPath: newPath})
	}

	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	for _, target := range targets {
		moves := byTarget[target]
		if len(moves) > 1 {
			oldPaths := make([]string, 0, len(moves))
			for _, mv := range moves {
				oldPaths = append(oldPaths, mv.OldPath)
			}
			sort.Strings(oldPaths)
			plan.Conflicts = append(plan.Conflicts, Conflict{NewPath: target, OldPaths: oldPaths})
			continue
		}
		plan.Moves = append(plan.Moves, moves[0])
	}

	return plan, nil
}

// Apply performs the moves in plan against root, updating the catalog and
// recording rename history. Apply refuses to touch the filesystem when
// r.DryRun is true -- defense in depth alongside the caller's own
// dry-run branching.
func (r *Renamer) Apply(root string, plan Plan) (Result, error) {
	if r.DryRun {
		return Result{Conflicts: plan.Conflicts}, nil
	}
	if len(plan.Moves) == 0 {
		return Result{Conflicts: plan.Conflicts}, nil
	}

	now := time.Now().Unix()
	runID, err := r.Catalog.StartRenameRun(now)
	if err != nil {
		return Result{}, fmt.Errorf("start rename run: %w", err)
	}

	renamed := 0
	for _, mv := range plan.Moves {
		oldFull := filepath.Join(root, filepath.FromSlash(mv.OldPath))
		newFull := filepath.Join(root, filepath.FromSlash(mv.NewPath))

		if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
			return Result{}, fmt.Errorf("create directory for %s: %w", mv.NewPath, err)
		}
		if err := os.Rename(oldFull, newFull); err != nil {
			return Result{}, fmt.Errorf("rename %s to %s: %w", mv.OldPath, mv.NewPath, err)
		}
		if err := r.Catalog.UpdateVideoPath(mv.VideoID, mv.NewPath); err != nil {
			return Result{}, fmt.Errorf("update catalog path for video %d: %w", mv.VideoID, err)
		}
		if err := r.Catalog.RecordRenameHistory(runID, mv.VideoID, mv.OldPath, mv.NewPath); err != nil {
			return Result{}, fmt.Errorf("record rename history for video %d: %w", mv.VideoID, err)
		}
		renamed++
	}

	if err := r.Catalog.FinishRenameRun(runID, renamed, time.Now().Unix()); err != nil {
		return Result{}, fmt.Errorf("finish rename run: %w", err)
	}

	return Result{FilesRenamed: renamed, Conflicts: plan.Conflicts}, nil
}
```

Note: `Apply` returns early (no rename run recorded) both when `DryRun` is set and when `plan.Moves` is empty — an empty apply has nothing to undo either, so recording a run with `files_renamed=0` would just clutter `rename_runs` for no benefit.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/rename/... -v`
Expected: PASS (all Plan/Apply tests, plus Task 3's template tests)

- [ ] **Step 5: Commit**

```bash
git add internal/rename
git commit -m "Add rename plan/apply orchestration"
```

---

### Task 5: `internal/rename` — undo

**Files:**
- Create: `internal/rename/undo.go`
- Test: `internal/rename/undo_test.go`

**Interfaces:**
- Consumes: `catalog.GetLastAppliedRenameRunID`/`ListRenameHistory`/`MarkRenameRunUndone`/`UpdateVideoPath` (Tasks 1-2), `Renamer` (Task 4, test setup only).
- Produces: `rename.UndoResult{FilesRestored int}`, `rename.Undo(cat *catalog.Catalog, root string) (UndoResult, error)`. Consumed by `internal/cli` (Task 9).

- [ ] **Step 1: Write the failing test**

```go
// internal/rename/undo_test.go
package rename_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/rename"
)

func TestUndoReversesLastAppliedRun(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := r.Apply(root, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	result, err := rename.Undo(cat, root)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.FilesRestored != 1 {
		t.Fatalf("expected 1 file restored, got %d", result.FilesRestored)
	}

	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err != nil {
		t.Fatalf("expected original file restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Bon Jovi", "Wanted Dead or Alive.mp4")); err == nil {
		t.Fatal("expected renamed file to no longer exist after undo")
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != videoID || active[0].Path != "Old Artist Name/Song-video.mp4" {
		t.Fatalf("expected catalog path restored, got %+v", active)
	}
}

func TestUndoTwiceErrorsOnSecondCall(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := r.Apply(root, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := rename.Undo(cat, root); err != nil {
		t.Fatalf("first Undo: %v", err)
	}
	if _, err := rename.Undo(cat, root); err == nil {
		t.Fatal("expected an error undoing when no applied run remains")
	}
}

func TestUndoNoAppliedRunErrors(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)

	if _, err := rename.Undo(cat, root); err == nil {
		t.Fatal("expected an error when there is nothing to undo")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/rename/... -run TestUndo -v`
Expected: FAIL — `undefined: rename.Undo`

- [ ] **Step 3: Implement `undo.go`**

```go
package rename

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// UndoResult is the outcome of reversing a rename run.
type UndoResult struct {
	FilesRestored int `json:"files_restored"`
}

// Undo reverses the most recently applied rename run: every file moved by
// that run is moved back to its original path, the catalog is updated to
// match, and the run is marked 'undone' so it can't be undone again.
func Undo(cat *catalog.Catalog, root string) (UndoResult, error) {
	runID, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		return UndoResult{}, fmt.Errorf("get last applied rename run: %w", err)
	}
	if !ok {
		return UndoResult{}, fmt.Errorf("no applied rename run to undo")
	}

	history, err := cat.ListRenameHistory(runID)
	if err != nil {
		return UndoResult{}, fmt.Errorf("list rename history: %w", err)
	}

	restored := 0
	for _, entry := range history {
		oldFull := filepath.Join(root, filepath.FromSlash(entry.OldPath))
		newFull := filepath.Join(root, filepath.FromSlash(entry.NewPath))

		if err := os.MkdirAll(filepath.Dir(oldFull), 0o755); err != nil {
			return UndoResult{}, fmt.Errorf("create directory for %s: %w", entry.OldPath, err)
		}
		if err := os.Rename(newFull, oldFull); err != nil {
			return UndoResult{}, fmt.Errorf("restore %s from %s: %w", entry.OldPath, entry.NewPath, err)
		}
		if err := cat.UpdateVideoPath(entry.VideoID, entry.OldPath); err != nil {
			return UndoResult{}, fmt.Errorf("update catalog path for video %d: %w", entry.VideoID, err)
		}
		restored++
	}

	if err := cat.MarkRenameRunUndone(runID); err != nil {
		return UndoResult{}, fmt.Errorf("mark rename run undone: %w", err)
	}

	return UndoResult{FilesRestored: restored}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/rename/... -v`
Expected: PASS (all Task 3-5 tests in this package)

- [ ] **Step 5: Commit**

```bash
git add internal/rename
git commit -m "Add rename undo"
```

---

### Task 6: `internal/nfo` — Kodi XML types

**Files:**
- Create: `internal/nfo/musicvideo.go`
- Create: `internal/nfo/artist.go`
- Test: `internal/nfo/musicvideo_test.go`
- Test: `internal/nfo/artist_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `nfo.MusicVideoNFO{Title, Artist, Album, Year, FilenameAndPath}`, `nfo.MarshalMusicVideo(n MusicVideoNFO) ([]byte, error)`, `nfo.ArtistNFO{Name}`, `nfo.MarshalArtist(n ArtistNFO) ([]byte, error)`. Consumed by `internal/nfo/writer.go` (Task 7).

- [ ] **Step 1: Write the failing tests**

```go
// internal/nfo/musicvideo_test.go
package nfo_test

import (
	"strings"
	"testing"

	"github.com/keif/musicvideo-manager/internal/nfo"
)

func TestMarshalMusicVideoBasicFields(t *testing.T) {
	body, err := nfo.MarshalMusicVideo(nfo.MusicVideoNFO{
		Title: "Wanted Dead or Alive", Artist: "Bon Jovi", Album: "Slippery When Wet", Year: 1986,
		FilenameAndPath: "Bon Jovi/Wanted Dead or Alive.mp4",
	})
	if err != nil {
		t.Fatalf("MarshalMusicVideo: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "<musicvideo>") {
		t.Fatalf("expected <musicvideo> root element, got: %s", s)
	}
	if !strings.Contains(s, "<title>Wanted Dead or Alive</title>") {
		t.Fatalf("expected title element, got: %s", s)
	}
	if !strings.Contains(s, "<artist>Bon Jovi</artist>") {
		t.Fatalf("expected artist element, got: %s", s)
	}
	if !strings.Contains(s, "<album>Slippery When Wet</album>") {
		t.Fatalf("expected album element, got: %s", s)
	}
	if !strings.Contains(s, "<year>1986</year>") {
		t.Fatalf("expected year element, got: %s", s)
	}
	if !strings.HasPrefix(s, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("expected XML header, got: %s", s)
	}
}

func TestMarshalMusicVideoOmitsEmptyAlbumAndYear(t *testing.T) {
	body, err := nfo.MarshalMusicVideo(nfo.MusicVideoNFO{
		Title: "Loser", Artist: "Beck", FilenameAndPath: "Beck/Loser.mp4",
	})
	if err != nil {
		t.Fatalf("MarshalMusicVideo: %v", err)
	}
	s := string(body)
	if strings.Contains(s, "<album>") {
		t.Fatalf("expected no album element when empty, got: %s", s)
	}
	if strings.Contains(s, "<year>") {
		t.Fatalf("expected no year element when zero, got: %s", s)
	}
}

func TestMarshalMusicVideoEscapesSpecialCharacters(t *testing.T) {
	body, err := nfo.MarshalMusicVideo(nfo.MusicVideoNFO{
		Title: "Rock & Roll", Artist: "AT&T Band", FilenameAndPath: "x/y.mp4",
	})
	if err != nil {
		t.Fatalf("MarshalMusicVideo: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "Rock &amp; Roll") {
		t.Fatalf("expected & to be escaped as &amp;, got: %s", s)
	}
	if !strings.Contains(s, "AT&amp;T Band") {
		t.Fatalf("expected & to be escaped as &amp;, got: %s", s)
	}
}
```

```go
// internal/nfo/artist_test.go
package nfo_test

import (
	"strings"
	"testing"

	"github.com/keif/musicvideo-manager/internal/nfo"
)

func TestMarshalArtist(t *testing.T) {
	body, err := nfo.MarshalArtist(nfo.ArtistNFO{Name: "Bon Jovi"})
	if err != nil {
		t.Fatalf("MarshalArtist: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "<artist>") {
		t.Fatalf("expected <artist> root element, got: %s", s)
	}
	if !strings.Contains(s, "<name>Bon Jovi</name>") {
		t.Fatalf("expected name element, got: %s", s)
	}
	if !strings.HasPrefix(s, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("expected XML header, got: %s", s)
	}
}

func TestMarshalArtistEscapesSpecialCharacters(t *testing.T) {
	body, err := nfo.MarshalArtist(nfo.ArtistNFO{Name: "AT&T Band"})
	if err != nil {
		t.Fatalf("MarshalArtist: %v", err)
	}
	if !strings.Contains(string(body), "AT&amp;T Band") {
		t.Fatalf("expected & to be escaped, got: %s", body)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/nfo/... -v`
Expected: FAIL — `undefined: nfo.MarshalMusicVideo` (the `internal/nfo` package doesn't exist yet)

- [ ] **Step 3: Implement `musicvideo.go` and `artist.go`**

```go
// internal/nfo/musicvideo.go
package nfo

import "encoding/xml"

// MusicVideoNFO is Kodi's native per-video music-video NFO schema.
type MusicVideoNFO struct {
	XMLName         xml.Name `xml:"musicvideo"`
	Title           string   `xml:"title"`
	Artist          string   `xml:"artist"`
	Album           string   `xml:"album,omitempty"`
	Year            int      `xml:"year,omitempty"`
	FilenameAndPath string   `xml:"filenameandpath"`
}

// MarshalMusicVideo renders a MusicVideoNFO as indented XML with the
// standard XML declaration.
func MarshalMusicVideo(n MusicVideoNFO) ([]byte, error) {
	body, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
```

```go
// internal/nfo/artist.go
package nfo

import "encoding/xml"

// ArtistNFO is Kodi's native per-artist NFO schema.
type ArtistNFO struct {
	XMLName xml.Name `xml:"artist"`
	Name    string   `xml:"name"`
}

// MarshalArtist renders an ArtistNFO as indented XML with the standard
// XML declaration.
func MarshalArtist(n ArtistNFO) ([]byte, error) {
	body, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/nfo/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/nfo
git commit -m "Add Kodi NFO XML types"
```

---

### Task 7: `internal/nfo` — writer orchestration

**Files:**
- Create: `internal/nfo/writer.go`
- Test: `internal/nfo/writer_test.go`

**Interfaces:**
- Consumes: `MarshalMusicVideo`/`MarshalArtist`/`MusicVideoNFO`/`ArtistNFO` (Task 6), `catalog.ListConfirmedMatches` (Task 2).
- Produces: `nfo.Result{VideoNFOsWritten, ArtistNFOsWritten int}`, `nfo.WriteAll(cat *catalog.Catalog, root string) (Result, error)`. Consumed by `internal/cli` (Task 10).

This task's tests use real temporary directories with real fixture files, since `WriteAll` performs actual `os.WriteFile` calls.

- [ ] **Step 1: Write the failing test**

```go
// internal/nfo/writer_test.go
package nfo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/nfo"
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

func setupConfirmedVideo(t *testing.T, cat *catalog.Catalog, root, relPath, artist, title string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	videoID, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: 5, MTime: 1, PartialHash: relPath,
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	artistID, err := cat.UpsertArtist(artist, "", 1)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, title, "", 0, "", 1)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sqlNullInt64ForTest(songID),
		Provider: "test", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
}

func TestWriteAllWritesVideoAndArtistNFOs(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Bon Jovi/Wanted Dead or Alive.mp4", "Bon Jovi", "Wanted Dead or Alive")

	result, err := nfo.WriteAll(cat, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideoNFOsWritten != 1 || result.ArtistNFOsWritten != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	videoNFO, err := os.ReadFile(filepath.Join(root, "Bon Jovi", "Wanted Dead or Alive.nfo"))
	if err != nil {
		t.Fatalf("expected video nfo to exist: %v", err)
	}
	if !strings.Contains(string(videoNFO), "<title>Wanted Dead or Alive</title>") {
		t.Fatalf("unexpected video nfo content: %s", videoNFO)
	}

	artistNFO, err := os.ReadFile(filepath.Join(root, "Bon Jovi", "artist.nfo"))
	if err != nil {
		t.Fatalf("expected artist nfo to exist: %v", err)
	}
	if !strings.Contains(string(artistNFO), "<name>Bon Jovi</name>") {
		t.Fatalf("unexpected artist nfo content: %s", artistNFO)
	}
}

func TestWriteAllWritesOneArtistNFOPerFolderNotPerVideo(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Metallica/One.mp4", "Metallica", "One")
	setupConfirmedVideo(t, cat, root, "Metallica/Enter Sandman.mp4", "Metallica", "Enter Sandman")

	result, err := nfo.WriteAll(cat, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideoNFOsWritten != 2 {
		t.Fatalf("expected 2 video nfos, got %d", result.VideoNFOsWritten)
	}
	if result.ArtistNFOsWritten != 1 {
		t.Fatalf("expected 1 artist nfo (one folder), got %d", result.ArtistNFOsWritten)
	}
}

func TestWriteAllEmptyLibrary(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)

	result, err := nfo.WriteAll(cat, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideoNFOsWritten != 0 || result.ArtistNFOsWritten != 0 {
		t.Fatalf("expected all-zero result, got %+v", result)
	}
}
```

Add a small test-only helper at the bottom of `writer_test.go`:

```go
import "database/sql"

func sqlNullInt64ForTest(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
```

(Add the `"database/sql"` import to the existing import block at the top of the file rather than as a second import statement. Named `sqlNullInt64ForTest` rather than `sqlNullInt64` since `internal/rename`'s test package already has a same-named helper — these are different packages so there's no actual Go conflict, but the distinct name avoids any confusion when reading both test suites side by side.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/nfo/... -run TestWriteAll -v`
Expected: FAIL — `undefined: nfo.WriteAll`

- [ ] **Step 3: Implement `writer.go`**

```go
package nfo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// Result summarizes a WriteAll run.
type Result struct {
	VideoNFOsWritten  int `json:"video_nfos_written"`
	ArtistNFOsWritten int `json:"artist_nfos_written"`
}

// WriteAll generates a per-video NFO for every confirmed match and a
// per-artist NFO for every distinct video folder among them, rooted at root.
func WriteAll(cat *catalog.Catalog, root string) (Result, error) {
	matches, err := cat.ListConfirmedMatches()
	if err != nil {
		return Result{}, fmt.Errorf("list confirmed matches: %w", err)
	}

	result := Result{}
	writtenArtistDirs := map[string]bool{}

	for _, m := range matches {
		videoFull := filepath.Join(root, filepath.FromSlash(m.VideoPath))
		ext := filepath.Ext(videoFull)
		nfoPath := strings.TrimSuffix(videoFull, ext) + ".nfo"

		body, err := MarshalMusicVideo(MusicVideoNFO{
			Title: m.Title, Artist: m.Artist, Album: m.Album, Year: m.Year,
			FilenameAndPath: m.VideoPath,
		})
		if err != nil {
			return Result{}, fmt.Errorf("marshal video nfo for %s: %w", m.VideoPath, err)
		}
		if err := os.WriteFile(nfoPath, body, 0o644); err != nil {
			return Result{}, fmt.Errorf("write video nfo for %s: %w", m.VideoPath, err)
		}
		result.VideoNFOsWritten++

		artistDir := filepath.Dir(videoFull)
		if !writtenArtistDirs[artistDir] {
			artistBody, err := MarshalArtist(ArtistNFO{Name: m.Artist})
			if err != nil {
				return Result{}, fmt.Errorf("marshal artist nfo for %s: %w", m.Artist, err)
			}
			if err := os.WriteFile(filepath.Join(artistDir, "artist.nfo"), artistBody, 0o644); err != nil {
				return Result{}, fmt.Errorf("write artist nfo for %s: %w", m.Artist, err)
			}
			writtenArtistDirs[artistDir] = true
			result.ArtistNFOsWritten++
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/nfo/... -v`
Expected: PASS (all Task 6-7 tests in this package)

- [ ] **Step 5: Commit**

```bash
git add internal/nfo
git commit -m "Add NFO writer orchestration"
```

---

### Task 8: `internal/config` — rename template

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.RenameConfig{Template string}`, `Config.Rename RenameConfig` field, `Default()` populates `Rename.Template = "{artist}/{title}"`. Consumed by `internal/cli` (Task 9).

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestDefaultIncludesRenameTemplate(t *testing.T) {
	cfg := config.Default("My Library")
	if cfg.Rename.Template != "{artist}/{title}" {
		t.Fatalf("expected default rename template %q, got %q", "{artist}/{title}", cfg.Rename.Template)
	}
}

func TestLoadOrCreateParsesExistingRenameSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "schema_version: 1\nlibrary:\n  name: Existing\nscan:\n  extensions: [mp4]\n  workers: 4\nmatch:\n  confidence_threshold: 0.7\nrename:\n  template: \"{artist}/{year} - {title}\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Ignored Default")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Rename.Template != "{artist}/{year} - {title}" {
		t.Fatalf("expected loaded rename template, got %q", cfg.Rename.Template)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/... -run "TestDefaultIncludesRenameTemplate|TestLoadOrCreateParsesExistingRenameSection" -v`
Expected: FAIL — `cfg.Rename undefined (type config.Config has no field or method Rename)`

- [ ] **Step 3: Add `RenameConfig` and wire it into `Config`/`Default`**

In `internal/config/config.go`, change the `Config` struct:

```go
type Config struct {
	SchemaVersion int          `yaml:"schema_version"`
	Library       Library      `yaml:"library"`
	Scan          ScanConfig   `yaml:"scan"`
	Match         MatchConfig  `yaml:"match"`
	Rename        RenameConfig `yaml:"rename"`
}
```

Add the new type after `MatchConfig`:

```go
type RenameConfig struct {
	Template string `yaml:"template"`
}
```

Change `Default` to populate it:

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
	}
}
```

`Load`, `Save`, `LoadOrCreate` are unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (all pre-existing tests plus the 2 new ones)

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "Add rename template to config"
```

---

### Task 9: `internal/cli` — rename command

**Files:**
- Create: `internal/cli/rename.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/rename_test.go`

**Interfaces:**
- Consumes: `config.LoadOrCreate` (now returns `cfg.Rename.Template` too), `catalog.Open`, `rename.Renamer`/`Plan`/`Result`/`Undo` (Tasks 3-5).
- Produces: `newRenameCmd() *cobra.Command`, registered in `NewRootCmd`.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/rename_test.go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestRenameCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"rename", root, "--dry-run"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when renaming a library that hasn't been scanned yet")
	}
}

func TestRenameCommandDryRunEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	renameCmd := cli.NewRootCmd("test")
	renameCmd.SetOut(&out)
	renameCmd.SetArgs([]string{"rename", root, "--dry-run", "--json"})

	if err := renameCmd.Execute(); err != nil {
		t.Fatalf("rename Execute: %v", err)
	}

	var plan struct {
		Moves     []any `json:"moves"`
		Unchanged int   `json:"unchanged"`
		Conflicts []any `json:"conflicts"`
	}
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if len(plan.Moves) != 0 || plan.Unchanged != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("expected an all-zero plan for an empty library, got %+v", plan)
	}
}

func TestRenameCommandDryRunDoesNotTouchFilesystem(t *testing.T) {
	root := t.TempDir()
	videoPath := filepath.Join(root, "Some Artist", "Some Song.mp4")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	renameCmd := cli.NewRootCmd("test")
	renameCmd.SetOut(&bytes.Buffer{})
	renameCmd.SetArgs([]string{"rename", root, "--dry-run"})
	if err := renameCmd.Execute(); err != nil {
		t.Fatalf("rename Execute: %v", err)
	}

	if _, err := os.Stat(videoPath); err != nil {
		t.Fatalf("expected original file untouched by dry-run: %v", err)
	}
}

func TestRenameCommandUndoWithNothingToUndoErrors(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	undoCmd := cli.NewRootCmd("test")
	undoCmd.SetOut(&bytes.Buffer{})
	undoCmd.SetArgs([]string{"rename", root, "--undo"})

	if err := undoCmd.Execute(); err == nil {
		t.Fatal("expected an error undoing when nothing has been renamed")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestRenameCommand -v`
Expected: FAIL — `unknown command "rename"`

- [ ] **Step 3: Implement `rename.go`**

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
	"github.com/keif/musicvideo-manager/internal/rename"
)

func newRenameCmd() *cobra.Command {
	var jsonOutput bool
	var dryRun bool
	var undo bool

	cmd := &cobra.Command{
		Use:   "rename [path]",
		Short: "Rename catalogued videos into a templated layout",
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

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			if undo {
				result, err := rename.Undo(cat, absRoot)
				if err != nil {
					return fmt.Errorf("undo: %w", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Restored %d files\n", result.FilesRestored)
				return nil
			}

			renamer := &rename.Renamer{Catalog: cat, Template: cfg.Rename.Template, DryRun: dryRun}
			plan, err := renamer.Plan()
			if err != nil {
				return fmt.Errorf("plan renames: %w", err)
			}

			if dryRun {
				if jsonOutput {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(plan)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Plan: %d to rename, %d unchanged, %d conflicts\n", len(plan.Moves), plan.Unchanged, len(plan.Conflicts))
				for _, mv := range plan.Moves {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s -> %s\n", mv.OldPath, mv.NewPath)
				}
				for _, c := range plan.Conflicts {
					fmt.Fprintf(cmd.OutOrStdout(), "  conflict: %v -> %s\n", c.OldPaths, c.NewPath)
				}
				return nil
			}

			result, err := renamer.Apply(absRoot, plan)
			if err != nil {
				return fmt.Errorf("apply renames: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed %d files, %d conflicts skipped\n", result.FilesRenamed, len(result.Conflicts))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview renames without moving files")
	cmd.Flags().BoolVar(&undo, "undo", false, "reverse the most recent applied rename run")
	return cmd
}
```

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go`, add `root.AddCommand(newRenameCmd())` to `NewRootCmd` (alongside `scan`/`match`/`version`).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/... -v`
Expected: PASS

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "Add rename CLI command"
```

---

### Task 10: `internal/cli` — nfo command

**Files:**
- Create: `internal/cli/nfo.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/nfo_test.go`

**Interfaces:**
- Consumes: `catalog.Open`, `nfo.WriteAll`/`Result` (Task 7).
- Produces: `newNFOCmd() *cobra.Command`, registered in `NewRootCmd`. This is the last CLI task in the plan.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/nfo_test.go
package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestNFOCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"nfo", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when generating NFOs for a library that hasn't been scanned yet")
	}
}

func TestNFOCommandJSONOutputEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	nfoCmd := cli.NewRootCmd("test")
	nfoCmd.SetOut(&out)
	nfoCmd.SetArgs([]string{"nfo", root, "--json"})

	if err := nfoCmd.Execute(); err != nil {
		t.Fatalf("nfo Execute: %v", err)
	}

	var result struct {
		VideoNFOsWritten  int `json:"video_nfos_written"`
		ArtistNFOsWritten int `json:"artist_nfos_written"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.VideoNFOsWritten != 0 || result.ArtistNFOsWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestNFOCommand -v`
Expected: FAIL — `unknown command "nfo"`

- [ ] **Step 3: Implement `nfo.go`**

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/nfo"
)

func newNFOCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "nfo [path]",
		Short: "Write Kodi-format NFO files for confirmed matches",
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

			result, err := nfo.WriteAll(cat, absRoot)
			if err != nil {
				return fmt.Errorf("write nfo: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d video NFOs, %d artist NFOs\n", result.VideoNFOsWritten, result.ArtistNFOsWritten)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
```

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go`, add `root.AddCommand(newNFOCmd())` to `NewRootCmd`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across every package (`pkg/provider`, `internal/catalog`, `internal/config`, `internal/match`, `internal/providers/musicbrainz`, `internal/rename`, `internal/nfo`, `internal/scanner`, `internal/cli`)

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "Add nfo CLI command"
```

---

### Task 11: ADR — rename template, collision handling, undo, and Kodi NFO format choice

**Files:**
- Create: `docs/adr/0007-rename-engine-and-kodi-nfo.md`

**Interfaces:**
- Consumes: decisions made in this plan's design spec (`docs/superpowers/specs/2026-07-29-rename-engine-and-nfo-design.md`) and this plan's Global Constraints.
- Produces: nothing consumed by code; historical record for contributors.

- [ ] **Step 1: Write the ADR**

```markdown
# 7. Rename engine safety model and Kodi NFO format over Plex's movie.nfo workaround

Status: Accepted

## Context

The rename engine is the project's first filesystem-mutating feature — every
prior command (`scan`, `match`) only ever wrote to the catalog. Two things
needed a decision before writing any code: how much safety machinery a
file-moving feature actually needs, and which NFO schema to target first,
given the original project brief said "support Plex first" but Plex has no
purpose-built music-video NFO format (its only path is a legacy Movies
agent reading `movie.nfo`, treating each video as a standalone "movie").

A real-data finding also shaped the rename template: MusicBrainz's returned
`album` field for a confirmed match is whatever release happens to be first
in its API response, not necessarily the canonical studio album (e.g. Alice
Cooper's "Poison" resolved to a 2009 live-compilation release title).

## Decision

**Rename safety**: every applied rename is recorded (`rename_runs` +
`rename_history`), never deleted — only marked `'undone'` after a
successful `rename --undo`, which reverses the most recent applied run in
full. Collisions (two videos computing the same target path) are detected
before any file is touched and excluded from application entirely rather
than auto-resolved or silently overwritten. The default rename template
(`{artist}/{title}`) avoids the noisy `{album}` field.

**NFO format**: target Kodi's native `<musicvideo>`/`<artist>` XML schema
first, not Plex's `movie.nfo` workaround. Kodi's schema is purpose-built for
this content type, and both Jellyfin and Emby's music-video libraries also
read Kodi-format NFO — one implementation reaches three media servers.
Plex's `movie.nfo` support becomes a distinct follow-on, since it needs a
different schema (`<movie>` root, fields like `plot`/`genre` that don't map
cleanly from what the catalog stores) and a different conceptual framing
(each video as a "movie," not a "music video").

## Consequences

- A user on Plex today gets no NFO benefit from this milestone — only
  Kodi/Jellyfin/Emby read what's generated. This is a deliberate sequencing
  choice, not an oversight; Plex support is real future work, not deferred
  indefinitely.
- Undo only ever reverses the single most recent applied run, not an
  arbitrary past one. Simple and matches the realistic "oops, bad template"
  recovery scenario; a run-history browser is possible future work if a
  more granular need emerges.
- Rename collisions are a known, current risk (duplicate files like
  `In Da Club-video.mp4`/`In Da Club-video.ts` in the real library can both
  match to the same song) with no automatic resolution yet — proper
  resolution is M5 (Duplicate Detection)'s job, once the tooling to tell
  which of two files is the one to keep actually exists.
- NFO generation runs independently of rename and is always safe to
  re-run — a future rematch improving a title doesn't require re-renaming
  to get updated NFOs, just `nfo` again.
```

- [ ] **Step 2: Commit**

```bash
git add docs/adr
git commit -m "Add ADR for rename engine safety model and Kodi NFO format choice"
```

---

### Task 12: Real-library dry-run verification

**Files:**
- None (no code changes — this task is verification only).

**Interfaces:**
- Consumes: `rename --dry-run` (Task 9), the real catalog at `/Volumes/Yggdrasil/Music Videos/.musicvideo-manager/catalog.sqlite` (already populated with 293 confirmed matches from the prior milestone).
- Produces: a verified, reported dry-run plan. Nothing downstream in this plan depends on it — this is the milestone's closing safety check.

**This task must NOT run `rename` without `--dry-run`, and must NOT run `nfo`, against the real library.** Both mutate real files on disk and are explicitly out of scope for automated execution of this plan per the Global Constraints — they require a separate, explicit decision after this plan is complete and reviewed.

- [ ] **Step 1: Run the real dry-run**

Run: `go run ./cmd/musicvideo-manager rename "/Volumes/Yggdrasil/Music Videos" --dry-run --json`

Expected: valid JSON with a `Moves` array (one entry per confirmed match whose current path differs from its templated target — expect a large fraction of the 293 confirmed videos here, since the existing filenames still carry `Artist - ` prefixes and YouTube-ID suffixes the default `{artist}/{title}` template strips), an `Unchanged` count, and a `Conflicts` array (expect at least the known `In Da Club-video.mp4`/`In Da Club-video.ts` duplicate to surface here, if both ended up confirmed to the same song).

- [ ] **Step 2: Spot-check a handful of planned moves for correctness**

Pick 5-10 entries from the `Moves` array and manually confirm each `OldPath -> NewPath` mapping looks sensible (e.g. `Bon Jovi/Bon Jovi - Wanted Dead Or Alive Official Music Video [SRvCvsRp5ho].mp4` -> `Bon Jovi/Wanted Dead or Alive.mp4`). Report these in the task's summary.

- [ ] **Step 3: Verify no files were actually touched**

Re-run the exact same dry-run command from Step 1 a second time and confirm the JSON output is byte-for-byte identical to the first run. Since the plan is entirely a function of the current catalog and filesystem state, an identical second output proves the first dry-run changed nothing on disk (any real move would have altered what the second `Plan()` computes).

- [ ] **Step 4: Report**

Summarize: total confirmed videos considered, count of planned moves, count unchanged, count of conflicts (and which files), and the 5-10 spot-checked examples with an assessment of whether the template output looks correct. This is the final task of the plan — no commit needed (no files changed).

---

## Plan Self-Review Notes

- **Spec coverage:** rename template (placeholders, sanitization, empty-segment collapse, default) ✓ (Task 3), collision detection ✓ (Task 4), Plan/Apply/dry-run ✓ (Task 4), undo (last-run-only, marks undone not deleted) ✓ (Task 5), Kodi `<musicvideo>`/`<artist>` NFO generation with correct escaping ✓ (Tasks 6-7), config `rename.template` ✓ (Task 8), CLI (`rename [--dry-run] [--undo]`, `nfo`) ✓ (Tasks 9-10), ADR covering both the safety model and the Plex-vs-Kodi NFO sequencing decision ✓ (Task 11), dry-run-only real-library safety boundary ✓ (Task 12, and called out in Global Constraints). Explicitly out-of-scope spec items (Plex `movie.nfo`, automatic collision resolution, per-run undo browsing) have no task, correctly.
- **Placeholder scan:** none found — every step has complete, runnable code or exact file content.
- **Type consistency:** verified `catalog.RenameHistoryEntry`/`ConfirmedMatch`, `rename.Renamer`/`Plan`/`Result`/`PlannedMove`/`Conflict`/`UndoResult`, and `nfo.MusicVideoNFO`/`ArtistNFO`/`Result` field names and the methods that produce/consume them line up exactly between the tasks that define them and the tasks that consume them (Tasks 1-2 → 4-5 → 9; Tasks 6 → 7 → 10). Confirmed every new list-returning catalog/package method initializes its slice to `[]T{}` rather than a bare `var`, per the Global Constraint drawn from the prior milestone's deferred nil-slice finding.
