# Metadata Matching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Identify catalogued videos by artist/title using filename parsing plus a MusicBrainz lookup, with every provider response cached and every match attempt (confirmed, needs-review, or unmatched) recorded in the catalog.

**Architecture:** A new `internal/match` package parses filenames and orchestrates matching; a new `internal/providers/musicbrainz` package implements the existing `pkg/provider.MetadataProvider` interface against MusicBrainz's recording-search API, rate-limited and cached through new `internal/catalog` tables/methods. A `match` CLI command runs this over a library.

**Tech Stack:** Go, `net/http` (standard library, no new HTTP client dependency), the existing `modernc.org/sqlite` catalog, Cobra CLI — same stack as the Foundation/Scanner milestone.

## Global Constraints

- Module path: `github.com/keif/musicvideo-manager`.
- No cgo — no new dependency may introduce it.
- No AI/model attribution anywhere in code, commits, or comments — every commit message must be plain and descriptive, with no "Co-Authored-By" or similar trailer.
- MusicBrainz usage policy: at most 1 request/second, and every request must send a descriptive `User-Agent` header.
- `provider_cache` entries never expire in this phase (documented limitation, not a bug to fix).
- `matches.video_id` is `UNIQUE` — one current match per video, upserted on rerun, not accumulated as history.
- Catalog/config live at `<library-root>/.musicvideo-manager/{config.yaml,catalog.sqlite}` (unchanged from the prior milestone).
- Do not implement Discogs, TheAudioDB, Last.fm, embedded-tag reading, or any match-review-editing command — all explicitly out of scope for this plan.

---

### Task 1: Extend `pkg/provider.MatchResult` with `ArtistSourceID`

**Files:**
- Modify: `pkg/provider/provider.go`
- Test: `pkg/provider/provider_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `provider.MatchResult` gains a new field `ArtistSourceID string`. Consumed by the MusicBrainz provider (Task 7) and the Matcher (Task 8), which need the artist's MusicBrainz ID separately from the recording's (`SourceID`, already existing, represents the matched song/recording).

The existing `pkg/provider.MetadataProvider` interface and `MatchQuery`/`MatchResult` types were built in the prior milestone as the seam for future providers. `MatchResult.SourceID` was documented as "provider-specific ID (MBID, Discogs ID, etc.)" without distinguishing which entity it identifies. Implementing the MusicBrainz provider surfaces the gap: a recording search result carries both a recording MBID and an artist MBID, and the catalog schema (Task 2) needs to store both (`songs.mbid` and `artists.mbid`). This task closes that gap before any provider or catalog code depends on it.

- [ ] **Step 1: Write the failing test**

Add this test function to the existing `pkg/provider/provider_test.go` (append it — do not remove `TestFakeProviderSatisfiesInterface`, which stays as-is):

```go
func TestMatchResultCarriesArtistSourceID(t *testing.T) {
	result := provider.MatchResult{
		Artist:         "50 Cent",
		Title:          "In Da Club",
		SourceID:       "recording-mbid",
		ArtistSourceID: "artist-mbid",
	}
	if result.ArtistSourceID != "artist-mbid" {
		t.Fatalf("expected ArtistSourceID %q, got %q", "artist-mbid", result.ArtistSourceID)
	}
	if result.SourceID != "recording-mbid" {
		t.Fatalf("expected SourceID %q, got %q", "recording-mbid", result.SourceID)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/provider/... -run TestMatchResultCarriesArtistSourceID -v`
Expected: FAIL — `unknown field ArtistSourceID in struct literal`

- [ ] **Step 3: Add the field**

In `pkg/provider/provider.go`, change the `MatchResult` struct to:

```go
// MatchResult is a single candidate identification returned by a provider.
type MatchResult struct {
	Artist         string
	Title          string
	Album          string
	Year           int
	Confidence     float64 // 0.0-1.0
	SourceID       string  // provider-specific ID for the matched song/recording (MBID, Discogs ID, etc.)
	ArtistSourceID string  // provider-specific ID for the matched artist (MBID, Discogs artist ID, etc.)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/provider/... -v`
Expected: PASS (both `TestFakeProviderSatisfiesInterface` and the new test)

- [ ] **Step 5: Commit**

```bash
git add pkg/provider
git commit -m "Add ArtistSourceID to MatchResult for artist/recording ID separation"
```

---

### Task 2: `internal/catalog` — matching schema migration + Artist repository

**Files:**
- Create: `internal/catalog/migrations/0002_matching.sql`
- Create: `internal/catalog/artist.go`
- Test: `internal/catalog/artist_test.go`

**Interfaces:**
- Consumes: `catalog.Open`/migration runner (already generic — any `NNNN_*.sql` file under `internal/catalog/migrations/` is auto-discovered, sorted by numeric prefix, and applied in a transaction; no changes needed to `catalog.go` itself).
- Produces: `artists`/`songs`/`matches`/`provider_cache` tables (this task's migration creates all four at once, matching the prior milestone's pattern of one migration file per logical schema addition). `catalog.Artist` struct, `(*Catalog) FindArtistByMBID(mbid string) (*Artist, bool, error)`, `(*Catalog) FindArtistByName(name string) (*Artist, bool, error)`, `(*Catalog) UpsertArtist(name, mbid string, at int64) (int64, error)`. Consumed by Task 3 (songs reference `artist_id`) and Task 8 (Matcher).

- [ ] **Step 1: Write the migration SQL**

```sql
-- internal/catalog/migrations/0002_matching.sql
CREATE TABLE artists (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    mbid       TEXT UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE songs (
    id         INTEGER PRIMARY KEY,
    artist_id  INTEGER NOT NULL REFERENCES artists(id),
    title      TEXT NOT NULL,
    album      TEXT,
    year       INTEGER,
    mbid       TEXT UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE matches (
    id             INTEGER PRIMARY KEY,
    video_id       INTEGER NOT NULL UNIQUE REFERENCES videos(id),
    song_id        INTEGER REFERENCES songs(id),
    provider       TEXT NOT NULL,
    confidence     REAL NOT NULL,
    status         TEXT NOT NULL,
    parsed_artist  TEXT,
    parsed_title   TEXT,
    matched_at     INTEGER NOT NULL
);
CREATE INDEX idx_matches_status ON matches(status);

CREATE TABLE provider_cache (
    id            INTEGER PRIMARY KEY,
    provider      TEXT NOT NULL,
    cache_key     TEXT NOT NULL,
    response_json TEXT NOT NULL,
    fetched_at    INTEGER NOT NULL,
    UNIQUE(provider, cache_key)
);
```

- [ ] **Step 2: Write the failing test**

```go
// internal/catalog/artist_test.go
package catalog_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func TestUpsertArtistInsertsNewRow(t *testing.T) {
	cat := openTestCatalog(t)

	id, err := cat.UpsertArtist("50 Cent", "artist-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	found, ok, err := cat.FindArtistByMBID("artist-mbid-1")
	if err != nil {
		t.Fatalf("FindArtistByMBID: %v", err)
	}
	if !ok {
		t.Fatal("expected to find artist by MBID")
	}
	if found.ID != id || found.Name != "50 Cent" {
		t.Fatalf("unexpected artist: %+v", found)
	}
}

func TestUpsertArtistIsIdempotentByMBID(t *testing.T) {
	cat := openTestCatalog(t)

	first, err := cat.UpsertArtist("50 Cent", "artist-mbid-1", 100)
	if err != nil {
		t.Fatalf("first UpsertArtist: %v", err)
	}
	second, err := cat.UpsertArtist("50 Cent", "artist-mbid-1", 200)
	if err != nil {
		t.Fatalf("second UpsertArtist: %v", err)
	}
	if first != second {
		t.Fatalf("expected same artist id on repeat upsert, got %d and %d", first, second)
	}
}

func TestUpsertArtistFallsBackToNameWhenMBIDEmpty(t *testing.T) {
	cat := openTestCatalog(t)

	first, err := cat.UpsertArtist("Unknown Artist", "", 100)
	if err != nil {
		t.Fatalf("first UpsertArtist: %v", err)
	}
	second, err := cat.UpsertArtist("Unknown Artist", "", 200)
	if err != nil {
		t.Fatalf("second UpsertArtist: %v", err)
	}
	if first != second {
		t.Fatalf("expected same artist id when matched by name, got %d and %d", first, second)
	}

	found, ok, err := cat.FindArtistByName("Unknown Artist")
	if err != nil {
		t.Fatalf("FindArtistByName: %v", err)
	}
	if !ok || found.ID != first {
		t.Fatalf("expected FindArtistByName to return id %d, got %+v (ok=%v)", first, found, ok)
	}
}

func TestFindArtistByMBIDNotFound(t *testing.T) {
	cat := openTestCatalog(t)

	_, ok, err := cat.FindArtistByMBID("does-not-exist")
	if err != nil {
		t.Fatalf("FindArtistByMBID: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a nonexistent MBID")
	}
}
```

This reuses the `openTestCatalog` helper already defined in `internal/catalog/video_test.go` (same package `catalog_test`) — no need to redefine it.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/catalog/... -run TestUpsertArtist -v`
Expected: FAIL — `undefined: catalog.Artist` (or similar)

- [ ] **Step 4: Implement `artist.go`**

```go
package catalog

import "database/sql"

// Artist is a single row in the artists table.
type Artist struct {
	ID        int64
	Name      string
	MBID      sql.NullString
	CreatedAt int64
}

const artistColumns = `id, name, mbid, created_at`

func scanArtist(row rowScanner) (*Artist, error) {
	a := &Artist{}
	if err := row.Scan(&a.ID, &a.Name, &a.MBID, &a.CreatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

// FindArtistByMBID returns the artist with the given MusicBrainz ID, if any.
func (c *Catalog) FindArtistByMBID(mbid string) (*Artist, bool, error) {
	row := c.db.QueryRow(`SELECT `+artistColumns+` FROM artists WHERE mbid = ?`, mbid)
	a, err := scanArtist(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return a, true, nil
}

// FindArtistByName returns the first artist with the given exact name, if any.
func (c *Catalog) FindArtistByName(name string) (*Artist, bool, error) {
	row := c.db.QueryRow(`SELECT `+artistColumns+` FROM artists WHERE name = ?`, name)
	a, err := scanArtist(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return a, true, nil
}

// UpsertArtist returns the ID of the artist matching mbid (if non-empty) or
// name, inserting a new row if neither matches an existing one. Existing
// rows are never overwritten by this call.
func (c *Catalog) UpsertArtist(name, mbid string, at int64) (int64, error) {
	if mbid != "" {
		if existing, ok, err := c.FindArtistByMBID(mbid); err != nil {
			return 0, err
		} else if ok {
			return existing.ID, nil
		}
	}
	if existing, ok, err := c.FindArtistByName(name); err != nil {
		return 0, err
	} else if ok {
		return existing.ID, nil
	}

	var mbidValue sql.NullString
	if mbid != "" {
		mbidValue = sql.NullString{String: mbid, Valid: true}
	}
	res, err := c.db.Exec(`INSERT INTO artists (name, mbid, created_at) VALUES (?, ?, ?)`, name, mbidValue, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (all artist tests, plus every pre-existing catalog test — confirming migration `0002` doesn't break anything)

- [ ] **Step 6: Commit**

```bash
git add internal/catalog
git commit -m "Add matching schema migration and artist repository methods"
```

---

### Task 3: `internal/catalog` — Song repository

**Files:**
- Create: `internal/catalog/song.go`
- Test: `internal/catalog/song_test.go`

**Interfaces:**
- Consumes: `artists` table and `Catalog.UpsertArtist` (Task 2, for test setup only — `song.go` itself only needs `artist_id` as a plain `int64`).
- Produces: `catalog.Song` struct, `(*Catalog) FindSongByMBID(mbid string) (*Song, bool, error)`, `(*Catalog) FindSongByArtistAndTitle(artistID int64, title string) (*Song, bool, error)`, `(*Catalog) UpsertSong(artistID int64, title, album string, year int, mbid string, at int64) (int64, error)`. Consumed by Task 8 (Matcher).

- [ ] **Step 1: Write the failing test**

```go
// internal/catalog/song_test.go
package catalog_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func TestUpsertSongInsertsNewRow(t *testing.T) {
	cat := openTestCatalog(t)
	artistID, err := cat.UpsertArtist("50 Cent", "artist-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}

	songID, err := cat.UpsertSong(artistID, "In Da Club", "Get Rich or Die Tryin'", 2003, "song-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if songID == 0 {
		t.Fatal("expected non-zero id")
	}

	var found *catalog.Song
	found, ok, err := cat.FindSongByMBID("song-mbid-1")
	if err != nil {
		t.Fatalf("FindSongByMBID: %v", err)
	}
	if !ok || found.Title != "In Da Club" || found.ArtistID != artistID {
		t.Fatalf("unexpected song: %+v (ok=%v)", found, ok)
	}
	if !found.Album.Valid || found.Album.String != "Get Rich or Die Tryin'" {
		t.Fatalf("unexpected album: %+v", found.Album)
	}
	if !found.Year.Valid || found.Year.Int64 != 2003 {
		t.Fatalf("unexpected year: %+v", found.Year)
	}
}

func TestUpsertSongIsIdempotentByMBID(t *testing.T) {
	cat := openTestCatalog(t)
	artistID, err := cat.UpsertArtist("50 Cent", "artist-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}

	first, err := cat.UpsertSong(artistID, "In Da Club", "", 0, "song-mbid-1", 100)
	if err != nil {
		t.Fatalf("first UpsertSong: %v", err)
	}
	second, err := cat.UpsertSong(artistID, "In Da Club", "", 0, "song-mbid-1", 200)
	if err != nil {
		t.Fatalf("second UpsertSong: %v", err)
	}
	if first != second {
		t.Fatalf("expected same song id on repeat upsert, got %d and %d", first, second)
	}
}

func TestUpsertSongFallsBackToArtistAndTitleWhenMBIDEmpty(t *testing.T) {
	cat := openTestCatalog(t)
	artistID, err := cat.UpsertArtist("Unknown Artist", "", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}

	first, err := cat.UpsertSong(artistID, "Some Title", "", 0, "", 100)
	if err != nil {
		t.Fatalf("first UpsertSong: %v", err)
	}
	second, err := cat.UpsertSong(artistID, "Some Title", "", 0, "", 200)
	if err != nil {
		t.Fatalf("second UpsertSong: %v", err)
	}
	if first != second {
		t.Fatalf("expected same song id when matched by artist+title, got %d and %d", first, second)
	}

	var found *catalog.Song
	found, ok, err := cat.FindSongByArtistAndTitle(artistID, "Some Title")
	if err != nil {
		t.Fatalf("FindSongByArtistAndTitle: %v", err)
	}
	if !ok || found.ID != first {
		t.Fatalf("expected FindSongByArtistAndTitle to return id %d, got %+v (ok=%v)", first, found, ok)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/catalog/... -run TestUpsertSong -v`
Expected: FAIL — `undefined: catalog.Song`

- [ ] **Step 3: Implement `song.go`**

```go
package catalog

import "database/sql"

// Song is a single row in the songs table.
type Song struct {
	ID        int64
	ArtistID  int64
	Title     string
	Album     sql.NullString
	Year      sql.NullInt64
	MBID      sql.NullString
	CreatedAt int64
}

const songColumns = `id, artist_id, title, album, year, mbid, created_at`

func scanSong(row rowScanner) (*Song, error) {
	s := &Song{}
	if err := row.Scan(&s.ID, &s.ArtistID, &s.Title, &s.Album, &s.Year, &s.MBID, &s.CreatedAt); err != nil {
		return nil, err
	}
	return s, nil
}

// FindSongByMBID returns the song with the given MusicBrainz recording ID, if any.
func (c *Catalog) FindSongByMBID(mbid string) (*Song, bool, error) {
	row := c.db.QueryRow(`SELECT `+songColumns+` FROM songs WHERE mbid = ?`, mbid)
	s, err := scanSong(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}

// FindSongByArtistAndTitle returns the first song matching artistID and
// an exact title, if any.
func (c *Catalog) FindSongByArtistAndTitle(artistID int64, title string) (*Song, bool, error) {
	row := c.db.QueryRow(`SELECT `+songColumns+` FROM songs WHERE artist_id = ? AND title = ?`, artistID, title)
	s, err := scanSong(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}

// UpsertSong returns the ID of the song matching mbid (if non-empty) or
// (artistID, title), inserting a new row if neither matches an existing
// one. Existing rows are never overwritten by this call.
func (c *Catalog) UpsertSong(artistID int64, title, album string, year int, mbid string, at int64) (int64, error) {
	if mbid != "" {
		if existing, ok, err := c.FindSongByMBID(mbid); err != nil {
			return 0, err
		} else if ok {
			return existing.ID, nil
		}
	}
	if existing, ok, err := c.FindSongByArtistAndTitle(artistID, title); err != nil {
		return 0, err
	} else if ok {
		return existing.ID, nil
	}

	var mbidValue, albumValue sql.NullString
	var yearValue sql.NullInt64
	if mbid != "" {
		mbidValue = sql.NullString{String: mbid, Valid: true}
	}
	if album != "" {
		albumValue = sql.NullString{String: album, Valid: true}
	}
	if year != 0 {
		yearValue = sql.NullInt64{Int64: int64(year), Valid: true}
	}

	res, err := c.db.Exec(`INSERT INTO songs (artist_id, title, album, year, mbid, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		artistID, title, albumValue, yearValue, mbidValue, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "Add song repository methods"
```

---

### Task 4: `internal/catalog` — Match repository

**Files:**
- Create: `internal/catalog/match.go`
- Test: `internal/catalog/match_test.go`

**Interfaces:**
- Consumes: `Video`/`videoColumns` (from `video.go`, Task 4 of the prior plan), `Catalog.UpsertArtist`/`UpsertSong` (Tasks 2-3, test setup only).
- Produces: `catalog.MatchStatus` type + consts `MatchStatusConfirmed`/`MatchStatusNeedsReview`/`MatchStatusUnmatched`, `catalog.Match` struct, `catalog.ReviewItem` struct, `(*Catalog) UpsertMatch(m *Match) error`, `(*Catalog) ListActiveUnmatched() ([]*Video, error)`, `(*Catalog) ListNeedsReview() ([]ReviewItem, error)`. Consumed by `internal/match` (Task 8) and `internal/cli` (Task 10).

- [ ] **Step 1: Write the failing test**

```go
// internal/catalog/match_test.go
package catalog_test

import (
	"database/sql"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func insertTestVideo(t *testing.T, cat *catalog.Catalog, path string) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: path, FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

func TestUpsertMatchInsertsAndUpdates(t *testing.T) {
	cat := openTestCatalog(t)
	videoID := insertTestVideo(t, cat, "Artist/Song.mp4")

	err := cat.UpsertMatch(&catalog.Match{
		VideoID:      videoID,
		Provider:     "musicbrainz",
		Confidence:   0.9,
		Status:       catalog.MatchStatusConfirmed,
		SongID:       sql.NullInt64{Int64: 1, Valid: true},
		ParsedArtist: sql.NullString{String: "Artist", Valid: true},
		ParsedTitle:  sql.NullString{String: "Song", Valid: true},
		MatchedAt:    100,
	})
	if err != nil {
		t.Fatalf("first UpsertMatch: %v", err)
	}

	unmatched, err := cat.ListActiveUnmatched()
	if err != nil {
		t.Fatalf("ListActiveUnmatched: %v", err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("expected 0 unmatched videos after matching, got %d", len(unmatched))
	}

	// Re-running match for the same video updates the existing row rather
	// than creating a second one (video_id is UNIQUE).
	err = cat.UpsertMatch(&catalog.Match{
		VideoID:    videoID,
		Provider:   "musicbrainz",
		Confidence: 0.5,
		Status:     catalog.MatchStatusNeedsReview,
		MatchedAt:  200,
	})
	if err != nil {
		t.Fatalf("second UpsertMatch: %v", err)
	}

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 needs-review item, got %d", len(items))
	}
	if items[0].VideoPath != "Artist/Song.mp4" {
		t.Fatalf("unexpected video path: %q", items[0].VideoPath)
	}
}

func TestListActiveUnmatchedOnlyReturnsVideosWithoutAMatch(t *testing.T) {
	cat := openTestCatalog(t)
	matchedID := insertTestVideo(t, cat, "Artist/Matched.mp4")
	unmatchedID := insertTestVideo(t, cat, "Artist/Unmatched.mp4")

	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: matchedID, Provider: "musicbrainz", Confidence: 1, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	unmatched, err := cat.ListActiveUnmatched()
	if err != nil {
		t.Fatalf("ListActiveUnmatched: %v", err)
	}
	if len(unmatched) != 1 || unmatched[0].ID != unmatchedID {
		t.Fatalf("expected only the unmatched video, got %+v", unmatched)
	}
}

func TestListNeedsReviewExcludesConfirmedAndUnmatched(t *testing.T) {
	cat := openTestCatalog(t)
	confirmedID := insertTestVideo(t, cat, "Artist/Confirmed.mp4")
	reviewID := insertTestVideo(t, cat, "Artist/Review.mp4")
	unmatchedID := insertTestVideo(t, cat, "Artist/Unmatched.mp4")

	mustUpsert := func(videoID int64, status catalog.MatchStatus) {
		if err := cat.UpsertMatch(&catalog.Match{
			VideoID: videoID, Provider: "musicbrainz", Confidence: 0.5, Status: status, MatchedAt: 100,
		}); err != nil {
			t.Fatalf("UpsertMatch: %v", err)
		}
	}
	mustUpsert(confirmedID, catalog.MatchStatusConfirmed)
	mustUpsert(reviewID, catalog.MatchStatusNeedsReview)
	mustUpsert(unmatchedID, catalog.MatchStatusUnmatched)

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 1 || items[0].VideoPath != "Artist/Review.mp4" {
		t.Fatalf("expected only the review video, got %+v", items)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/catalog/... -run TestUpsertMatch -v`
Expected: FAIL — `undefined: catalog.Match`

- [ ] **Step 3: Implement `match.go`**

```go
package catalog

import "database/sql"

// MatchStatus is the outcome of a single match attempt for a video.
type MatchStatus string

const (
	MatchStatusConfirmed   MatchStatus = "confirmed"
	MatchStatusNeedsReview MatchStatus = "needs_review"
	MatchStatusUnmatched   MatchStatus = "unmatched"
)

// Match is a single row in the matches table: the current best
// identification attempt for one video.
type Match struct {
	ID           int64
	VideoID      int64
	SongID       sql.NullInt64
	Provider     string
	Confidence   float64
	Status       MatchStatus
	ParsedArtist sql.NullString
	ParsedTitle  sql.NullString
	MatchedAt    int64
}

// UpsertMatch inserts a new match row, or replaces the existing row for
// m.VideoID (matches.video_id is UNIQUE — there is exactly one current
// match per video).
func (c *Catalog) UpsertMatch(m *Match) error {
	_, err := c.db.Exec(`INSERT INTO matches (video_id, song_id, provider, confidence, status, parsed_artist, parsed_title, matched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(video_id) DO UPDATE SET
			song_id = excluded.song_id,
			provider = excluded.provider,
			confidence = excluded.confidence,
			status = excluded.status,
			parsed_artist = excluded.parsed_artist,
			parsed_title = excluded.parsed_title,
			matched_at = excluded.matched_at`,
		m.VideoID, m.SongID, m.Provider, m.Confidence, m.Status, m.ParsedArtist, m.ParsedTitle, m.MatchedAt)
	return err
}

// ListActiveUnmatched returns every active video with no row in matches yet.
func (c *Catalog) ListActiveUnmatched() ([]*Video, error) {
	rows, err := c.db.Query(`SELECT ` + videoColumns + ` FROM videos
		WHERE status = 'active' AND id NOT IN (SELECT video_id FROM matches)`)
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

// ReviewItem is a single needs_review match, joined with its video's path
// for display. Nullable fields are surfaced as plain strings (empty if
// NULL) since this is a read-only reporting view, not something composed
// with further NULL-sensitive logic.
type ReviewItem struct {
	VideoPath    string  `json:"video_path"`
	ParsedArtist string  `json:"parsed_artist"`
	ParsedTitle  string  `json:"parsed_title"`
	Confidence   float64 `json:"confidence"`
}

// ListNeedsReview returns every match with status = 'needs_review',
// ordered by video path.
func (c *Catalog) ListNeedsReview() ([]ReviewItem, error) {
	rows, err := c.db.Query(`SELECT v.path, m.parsed_artist, m.parsed_title, m.confidence
		FROM matches m
		JOIN videos v ON v.id = m.video_id
		WHERE m.status = 'needs_review'
		ORDER BY v.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReviewItem
	for rows.Next() {
		var item ReviewItem
		var parsedArtist, parsedTitle sql.NullString
		if err := rows.Scan(&item.VideoPath, &parsedArtist, &parsedTitle, &item.Confidence); err != nil {
			return nil, err
		}
		item.ParsedArtist = parsedArtist.String
		item.ParsedTitle = parsedTitle.String
		out = append(out, item)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "Add match repository methods"
```

---

### Task 5: `internal/catalog` — Provider response cache

**Files:**
- Create: `internal/catalog/providercache.go`
- Test: `internal/catalog/providercache_test.go`

**Interfaces:**
- Consumes: nothing beyond `Catalog`/`Open` (Task 3 of the prior plan).
- Produces: `(*Catalog) GetCachedResponse(providerName, cacheKey string) (string, bool, error)`, `(*Catalog) SetCachedResponse(providerName, cacheKey, responseJSON string, fetchedAt int64) error`. Consumed by the MusicBrainz provider (Task 7).

- [ ] **Step 1: Write the failing test**

```go
// internal/catalog/providercache_test.go
package catalog_test

import "testing"

func TestGetCachedResponseMiss(t *testing.T) {
	cat := openTestCatalog(t)

	_, ok, err := cat.GetCachedResponse("musicbrainz", "some-key")
	if err != nil {
		t.Fatalf("GetCachedResponse: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an uncached key")
	}
}

func TestSetThenGetCachedResponse(t *testing.T) {
	cat := openTestCatalog(t)

	if err := cat.SetCachedResponse("musicbrainz", "some-key", `{"recordings":[]}`, 100); err != nil {
		t.Fatalf("SetCachedResponse: %v", err)
	}

	body, ok, err := cat.GetCachedResponse("musicbrainz", "some-key")
	if err != nil {
		t.Fatalf("GetCachedResponse: %v", err)
	}
	if !ok || body != `{"recordings":[]}` {
		t.Fatalf("unexpected cached response: %q (ok=%v)", body, ok)
	}
}

func TestSetCachedResponseOverwritesExisting(t *testing.T) {
	cat := openTestCatalog(t)

	if err := cat.SetCachedResponse("musicbrainz", "some-key", `{"first":true}`, 100); err != nil {
		t.Fatalf("first SetCachedResponse: %v", err)
	}
	if err := cat.SetCachedResponse("musicbrainz", "some-key", `{"second":true}`, 200); err != nil {
		t.Fatalf("second SetCachedResponse: %v", err)
	}

	body, ok, err := cat.GetCachedResponse("musicbrainz", "some-key")
	if err != nil {
		t.Fatalf("GetCachedResponse: %v", err)
	}
	if !ok || body != `{"second":true}` {
		t.Fatalf("expected overwritten response, got %q (ok=%v)", body, ok)
	}
}

func TestCacheKeysAreScopedPerProvider(t *testing.T) {
	cat := openTestCatalog(t)

	if err := cat.SetCachedResponse("musicbrainz", "shared-key", `{"provider":"mb"}`, 100); err != nil {
		t.Fatalf("SetCachedResponse musicbrainz: %v", err)
	}

	_, ok, err := cat.GetCachedResponse("discogs", "shared-key")
	if err != nil {
		t.Fatalf("GetCachedResponse discogs: %v", err)
	}
	if ok {
		t.Fatal("expected a cache entry for one provider not to be visible under another provider's name")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/catalog/... -run TestGetCachedResponse -v`
Expected: FAIL — `undefined: catalog.GetCachedResponse` (accessed via a `*Catalog` value, so the actual error text will reference the missing method)

- [ ] **Step 3: Implement `providercache.go`**

```go
package catalog

import "database/sql"

// GetCachedResponse returns a previously cached provider response, if one exists.
func (c *Catalog) GetCachedResponse(providerName, cacheKey string) (string, bool, error) {
	var responseJSON string
	err := c.db.QueryRow(`SELECT response_json FROM provider_cache WHERE provider = ? AND cache_key = ?`,
		providerName, cacheKey).Scan(&responseJSON)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return responseJSON, true, nil
}

// SetCachedResponse stores (or overwrites) a provider response.
// provider_cache entries never expire in this phase — see ADR 0006.
func (c *Catalog) SetCachedResponse(providerName, cacheKey, responseJSON string, fetchedAt int64) error {
	_, err := c.db.Exec(`INSERT INTO provider_cache (provider, cache_key, response_json, fetched_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, cache_key) DO UPDATE SET
			response_json = excluded.response_json,
			fetched_at = excluded.fetched_at`,
		providerName, cacheKey, responseJSON, fetchedAt)
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (every catalog test — this task completes the catalog-layer work for this plan)

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "Add provider response cache"
```

---

### Task 6: `internal/match` — filename parsing

**Files:**
- Create: `internal/match/filename.go`
- Test: `internal/match/filename_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `match.ParseFilename(filename, folder string) (artist, title string, confidence float64)`. Consumed by the Matcher (Task 8).

- [ ] **Step 1: Write the failing test**

```go
// internal/match/filename_test.go
package match_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/match"
)

func TestParseFilenameDashDelimited(t *testing.T) {
	cases := []struct {
		filename       string
		folder         string
		wantArtist     string
		wantTitle      string
		minConfidence  float64
	}{
		{"50 Cent - In Da Club-video", "50 Cent", "50 Cent", "In Da Club", 0.8},
		{"AC DC - Back In Black-video", "ACDC", "AC DC", "Back In Black", 0.8},
		{"a-ha - Take On Me-video", "A-Ha", "a-ha", "Take On Me", 0.8},
		{"Beck - Loser Official Music Video [YgSPaXgAdzE]", "Beck", "Beck", "Loser", 0.8},
		{"Bryan Adams - Summer Of 69 Official Music Video [9f06QZCVUHg]", "Bryan Adams", "Bryan Adams", "Summer Of 69", 0.8},
		{"Sixpence None The Richer - Kiss Me Official HQ [8N-qO3sPMjc]", "Sixpence None the Richer", "Sixpence None The Richer", "Kiss Me", 0.8},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			artist, title, confidence := match.ParseFilename(tc.filename, tc.folder)
			if artist != tc.wantArtist {
				t.Errorf("artist: got %q, want %q", artist, tc.wantArtist)
			}
			if title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", title, tc.wantTitle)
			}
			if confidence < tc.minConfidence {
				t.Errorf("confidence: got %f, want at least %f", confidence, tc.minConfidence)
			}
		})
	}
}

func TestParseFilenameNoDelimiterFallsBackToFolder(t *testing.T) {
	artist, title, confidence := match.ParseFilename("SomeRandomTitle", "The Folder Artist")
	if artist != "The Folder Artist" {
		t.Errorf("expected folder fallback artist, got %q", artist)
	}
	if title != "SomeRandomTitle" {
		t.Errorf("expected filename as title, got %q", title)
	}
	if confidence <= 0 || confidence >= 0.8 {
		t.Errorf("expected a medium confidence between 0 and 0.8, got %f", confidence)
	}
}

func TestParseFilenameEmptyReturnsZeroConfidence(t *testing.T) {
	artist, title, confidence := match.ParseFilename("", "")
	if confidence != 0 {
		t.Errorf("expected confidence 0 for empty input, got %f", confidence)
	}
	if artist != "" || title != "" {
		t.Errorf("expected empty artist/title, got artist=%q title=%q", artist, title)
	}
}

func TestParseFilenameNoDelimiterNoFolderReturnsZeroConfidence(t *testing.T) {
	_, _, confidence := match.ParseFilename("NoDelimiterHere", "")
	if confidence != 0 {
		t.Errorf("expected confidence 0 when there's no delimiter and no folder, got %f", confidence)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/match/... -v`
Expected: FAIL — `undefined: match.ParseFilename` (the `internal/match` package doesn't exist yet)

- [ ] **Step 3: Implement `filename.go`**

```go
package match

import (
	"regexp"
	"strings"
)

var dashPattern = regexp.MustCompile(`^(.+?)\s+-\s+(.+)$`)

var suffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*\[[^\]]*\]\s*$`),                        // trailing [video id]
	regexp.MustCompile(`(?i)\s*\(\s*official\s*(music\s*)?video\s*\)\s*$`), // (Official [Music] Video)
	regexp.MustCompile(`(?i)\s*official\s*(music\s*)?video\s*$`),       // bare "Official Music Video"
	regexp.MustCompile(`(?i)\s*-\s*video\s*$`),                         // "-video" suffix
	regexp.MustCompile(`(?i)\s*official\s*hq\s*$`),                     // "Official HQ"
	regexp.MustCompile(`(?i)\s*extended\s*version\s*$`),                // "Extended Version"
}

// ParseFilename guesses an artist and title from a video's base filename
// (extension already stripped by the caller) and its parent folder name.
// It tries progressively weaker patterns and returns the confidence of
// whichever one matched; confidence 0 means no usable signal was found at
// all, and callers should treat that as a signal to skip provider lookup
// entirely rather than querying with an empty/meaningless guess.
func ParseFilename(filename, folder string) (artist, title string, confidence float64) {
	name := strings.TrimSpace(filename)
	folder = strings.TrimSpace(folder)

	if name == "" {
		return "", "", 0
	}

	if m := dashPattern.FindStringSubmatch(name); m != nil {
		a := strings.TrimSpace(m[1])
		t := cleanTitle(m[2])
		if a != "" && t != "" {
			return a, t, 0.9
		}
	}

	if folder != "" {
		return folder, cleanTitle(name), 0.5
	}

	return "", "", 0
}

func cleanTitle(s string) string {
	for {
		stripped := s
		for _, p := range suffixPatterns {
			stripped = p.ReplaceAllString(stripped, "")
		}
		stripped = strings.TrimSpace(stripped)
		if stripped == s {
			break
		}
		s = stripped
	}
	return s
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/match/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/match
git commit -m "Add filename parsing for artist/title guessing"
```

---

### Task 7: `internal/providers/musicbrainz` — MusicBrainz provider

**Files:**
- Create: `internal/providers/musicbrainz/musicbrainz.go`
- Test: `internal/providers/musicbrainz/musicbrainz_test.go`

**Interfaces:**
- Consumes: `pkg/provider.MetadataProvider`/`MatchQuery`/`MatchResult` (Task 1 of this plan for `ArtistSourceID`, prior milestone for the rest), `catalog.GetCachedResponse`/`SetCachedResponse` (Task 5).
- Produces: `musicbrainz.MusicBrainzProvider` struct (`Catalog`, `HTTPClient`, `UserAgent`, `BaseURL`, `MinInterval` fields — the last two overridable, used by tests to point at an `httptest.Server` and use a millisecond-scale rate limit), `musicbrainz.New(cat *catalog.Catalog, userAgent string) *MusicBrainzProvider`, `(*MusicBrainzProvider) Name() string`, `(*MusicBrainzProvider) Match(ctx, provider.MatchQuery) ([]provider.MatchResult, error)` — satisfies `provider.MetadataProvider`. Consumed by `internal/cli` (Task 10).

- [ ] **Step 1: Write the failing test**

```go
// internal/providers/musicbrainz/musicbrainz_test.go
package musicbrainz_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/musicbrainz"
	"github.com/keif/musicvideo-manager/pkg/provider"
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

const sampleRecordingJSON = `{
  "recordings": [
    {
      "id": "recording-mbid-1",
      "title": "In Da Club",
      "score": 100,
      "artist-credit": [{"name": "50 Cent", "artist": {"id": "artist-mbid-1", "name": "50 Cent"}}],
      "releases": [{"title": "Get Rich or Die Tryin'", "date": "2003-02-06"}]
    }
  ]
}`

func TestMatchCacheMissCallsHTTPThenCachesForNextCall(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleRecordingJSON))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent/1.0")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	results, err := p.Match(context.Background(), provider.MatchQuery{Artist: "50 Cent", Title: "In Da Club"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0]
	if got.Artist != "50 Cent" || got.Title != "In Da Club" {
		t.Fatalf("unexpected artist/title: %+v", got)
	}
	if got.Confidence != 1.0 {
		t.Fatalf("expected confidence 1.0, got %f", got.Confidence)
	}
	if got.SourceID != "recording-mbid-1" || got.ArtistSourceID != "artist-mbid-1" {
		t.Fatalf("unexpected IDs: %+v", got)
	}
	if got.Album != "Get Rich or Die Tryin'" || got.Year != 2003 {
		t.Fatalf("unexpected album/year: %+v", got)
	}

	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", n)
	}

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "50 Cent", Title: "In Da Club"}); err != nil {
		t.Fatalf("second Match: %v", err)
	}
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected still 1 HTTP request after cache hit, got %d", n)
	}
}

func TestMatchSendsUserAgentHeader(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "musicvideo-manager/test (https://example.com)")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist", Title: "Title"}); err != nil {
		t.Fatalf("Match: %v", err)
	}
	if gotUserAgent != "musicvideo-manager/test (https://example.com)" {
		t.Fatalf("unexpected User-Agent: %q", gotUserAgent)
	}
}

func TestMatchEnforcesRateLimit(t *testing.T) {
	var timestamps []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = 50 * time.Millisecond

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist One", Title: "Title One"}); err != nil {
		t.Fatalf("first Match: %v", err)
	}
	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist Two", Title: "Title Two"}); err != nil {
		t.Fatalf("second Match: %v", err)
	}

	if len(timestamps) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(timestamps))
	}
	if elapsed := timestamps[1].Sub(timestamps[0]); elapsed < 50*time.Millisecond {
		t.Fatalf("expected at least 50ms between requests, got %v", elapsed)
	}
}

func TestMatchEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	results, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Nonexistent", Title: "Song"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestMatchNonOKStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist", Title: "Title"}); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestName(t *testing.T) {
	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	if p.Name() != "musicbrainz" {
		t.Fatalf("expected Name() %q, got %q", "musicbrainz", p.Name())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/providers/musicbrainz/... -v`
Expected: FAIL — `undefined: musicbrainz.New` (the package doesn't exist yet)

- [ ] **Step 3: Implement `musicbrainz.go`**

```go
package musicbrainz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/pkg/provider"
)

const (
	defaultBaseURL     = "https://musicbrainz.org/ws/2"
	defaultMinInterval = time.Second
)

// MusicBrainzProvider implements pkg/provider.MetadataProvider against
// MusicBrainz's recording search API, respecting its usage policy: at
// most one request per second, and a descriptive User-Agent header.
// Every response is cached via Catalog so a repeated query never hits
// the network twice.
type MusicBrainzProvider struct {
	Catalog     *catalog.Catalog
	HTTPClient  *http.Client
	UserAgent   string
	BaseURL     string
	MinInterval time.Duration

	mu          sync.Mutex
	lastRequest time.Time
}

// New returns a MusicBrainzProvider configured for the real MusicBrainz API.
func New(cat *catalog.Catalog, userAgent string) *MusicBrainzProvider {
	return &MusicBrainzProvider{
		Catalog:     cat,
		HTTPClient:  http.DefaultClient,
		UserAgent:   userAgent,
		BaseURL:     defaultBaseURL,
		MinInterval: defaultMinInterval,
	}
}

func (p *MusicBrainzProvider) Name() string { return "musicbrainz" }

func (p *MusicBrainzProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error) {
	cacheKey := computeCacheKey(q.Artist, q.Title)

	if cached, ok, err := p.Catalog.GetCachedResponse(p.Name(), cacheKey); err != nil {
		return nil, fmt.Errorf("check cache: %w", err)
	} else if ok {
		return parseRecordingSearchResponse([]byte(cached))
	}

	p.waitForRateLimit()

	body, err := p.search(ctx, q.Artist, q.Title)
	if err != nil {
		return nil, fmt.Errorf("musicbrainz search: %w", err)
	}

	if err := p.Catalog.SetCachedResponse(p.Name(), cacheKey, string(body), time.Now().Unix()); err != nil {
		return nil, fmt.Errorf("cache response: %w", err)
	}

	return parseRecordingSearchResponse(body)
}

func (p *MusicBrainzProvider) waitForRateLimit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.lastRequest.IsZero() {
		if elapsed := time.Since(p.lastRequest); elapsed < p.MinInterval {
			time.Sleep(p.MinInterval - elapsed)
		}
	}
	p.lastRequest = time.Now()
}

func (p *MusicBrainzProvider) search(ctx context.Context, artist, title string) ([]byte, error) {
	query := fmt.Sprintf(`artist:%s AND recording:%s`, luceneQuote(artist), luceneQuote(title))

	u := p.BaseURL + "/recording?query=" + url.QueryEscape(query) + "&fmt=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", p.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func luceneQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func computeCacheKey(artist, title string) string {
	normalized := strings.ToLower(strings.TrimSpace(artist)) + "\x00" + strings.ToLower(strings.TrimSpace(title))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

type recordingSearchResponse struct {
	Recordings []recording `json:"recordings"`
}

type recording struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Score        int            `json:"score"`
	ArtistCredit []artistCredit `json:"artist-credit"`
	Releases     []release      `json:"releases"`
}

type artistCredit struct {
	Name   string       `json:"name"`
	Artist artistDetail `json:"artist"`
}

type artistDetail struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type release struct {
	Title string `json:"title"`
	Date  string `json:"date"`
}

func parseRecordingSearchResponse(body []byte) ([]provider.MatchResult, error) {
	var parsed recordingSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse musicbrainz response: %w", err)
	}

	results := make([]provider.MatchResult, 0, len(parsed.Recordings))
	for _, rec := range parsed.Recordings {
		artistName, artistSourceID := "", ""
		if len(rec.ArtistCredit) > 0 {
			artistName = rec.ArtistCredit[0].Name
			artistSourceID = rec.ArtistCredit[0].Artist.ID
		}
		album, year := "", 0
		if len(rec.Releases) > 0 {
			album = rec.Releases[0].Title
			year = parseYear(rec.Releases[0].Date)
		}
		results = append(results, provider.MatchResult{
			Artist:         artistName,
			Title:          rec.Title,
			Album:          album,
			Year:           year,
			Confidence:     float64(rec.Score) / 100.0,
			SourceID:       rec.ID,
			ArtistSourceID: artistSourceID,
		})
	}
	return results, nil
}

func parseYear(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y, err := strconv.Atoi(dateStr[:4])
	if err != nil {
		return 0
	}
	return y
}
```

- [ ] **Step 4: Fetch dependencies**

None — this task only uses the Go standard library (`net/http`, `encoding/json`, `crypto/sha256`) plus already-vendored `internal/catalog` and `pkg/provider`. No `go get` needed.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/providers/musicbrainz/... -v`
Expected: PASS (all 6 tests; `TestMatchEnforcesRateLimit` takes ~50ms, everything else is near-instant since `MinInterval` is overridden to millisecond scale in every test)

- [ ] **Step 6: Commit**

```bash
git add internal/providers
git commit -m "Add MusicBrainz metadata provider"
```

---

### Task 8: `internal/match` — Matcher orchestration

**Files:**
- Create: `internal/match/match.go`
- Test: `internal/match/match_test.go`

**Interfaces:**
- Consumes: `ParseFilename` (Task 6), `catalog.ListActiveUnmatched`/`UpsertMatch`/`UpsertArtist`/`UpsertSong` (Tasks 2-4), `pkg/provider.MetadataProvider`/`MatchQuery`/`MatchResult` (Task 1 + prior milestone).
- Produces: `match.MatchErrorEntry` struct, `match.Result` struct, `match.Matcher{Catalog, Provider, ConfidenceThreshold}`, `(*Matcher) Run(ctx context.Context) (Result, error)`. Consumed by `internal/cli` (Task 10).

This task uses a **fake** `provider.MetadataProvider` in its tests (no real MusicBrainz/network dependency) to test orchestration logic in isolation — the same pattern the prior milestone's `Reconciler` tests used for `Hash`/`Probe`.

- [ ] **Step 1: Write the failing test**

```go
// internal/match/match_test.go
package match_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/match"
	"github.com/keif/musicvideo-manager/pkg/provider"
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

func insertActiveVideo(t *testing.T, cat *catalog.Catalog, path string) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: path, FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

type fakeProvider struct {
	name      string
	results   map[string][]provider.MatchResult // keyed by "Artist|Title"
	err       error
	callCount int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error) {
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return f.results[q.Artist+"|"+q.Title], nil
}

func TestRunConfirmsHighConfidenceMatch(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "50 Cent/50 Cent - In Da Club-video.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{
		"50 Cent|In Da Club": {{Artist: "50 Cent", Title: "In Da Club", Confidence: 0.95, SourceID: "song-1", ArtistSourceID: "artist-1"}},
	}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Confirmed != 1 || result.NeedsReview != 0 || result.Unmatched != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no review items, got %+v", items)
	}
}

func TestRunFlagsLowConfidenceAsNeedsReview(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "50 Cent/50 Cent - In Da Club-video.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{
		"50 Cent|In Da Club": {{Artist: "50 Cent", Title: "In Da Club", Confidence: 0.4, SourceID: "song-1"}},
	}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.NeedsReview != 1 || result.Confirmed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 1 || items[0].Confidence != 0.4 {
		t.Fatalf("unexpected review items: %+v", items)
	}
}

func TestRunMarksUnmatchedWhenProviderReturnsNoCandidates(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "50 Cent/50 Cent - In Da Club-video.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Unmatched != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunSkipsProviderForUnparseableFilename(t *testing.T) {
	cat := openTestCatalog(t)
	// No " - " delimiter and inserted at the library root (no parent
	// folder), so ParseFilename returns confidence 0.
	insertActiveVideo(t, cat, "NoDelimiterHere.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Unmatched != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fp.callCount != 0 {
		t.Fatalf("expected provider not to be called for an unparseable filename, got %d calls", fp.callCount)
	}
}

func TestRunRecordsProviderErrorWithoutAbortingBatch(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "Bad Artist/Bad Artist - Bad Song-video.mp4")
	insertActiveVideo(t, cat, "Good Artist/Good Artist - Good Song-video.mp4")

	fp := &fakeProvider{
		name: "fake",
		err:  errors.New("simulated provider failure"),
	}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run should not fail the whole batch on a provider error: %v", err)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 recorded errors, got %+v", result.Errors)
	}

	unmatched, err := cat.ListActiveUnmatched()
	if err != nil {
		t.Fatalf("ListActiveUnmatched: %v", err)
	}
	if len(unmatched) != 2 {
		t.Fatalf("expected both videos to remain unmatched (retryable), got %d", len(unmatched))
	}
}

func TestRunEmptyLibraryReturnsZeroResult(t *testing.T) {
	cat := openTestCatalog(t)
	fp := &fakeProvider{name: "fake"}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Confirmed != 0 || result.NeedsReview != 0 || result.Unmatched != 0 || len(result.Errors) != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/match/... -run TestRun -v`
Expected: FAIL — `undefined: match.Matcher`

- [ ] **Step 3: Implement `match.go`**

```go
package match

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/pkg/provider"
)

// Matcher identifies catalogued videos against a metadata provider.
type Matcher struct {
	Catalog             *catalog.Catalog
	Provider            provider.MetadataProvider
	ConfidenceThreshold float64
}

// MatchErrorEntry is a single video that failed to match due to a
// provider error. It is not persisted to the catalog: the video is left
// without a matches row, so it's naturally retried on the next Run.
type MatchErrorEntry struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Result summarizes what a single Run call did.
type Result struct {
	Confirmed   int               `json:"confirmed"`
	NeedsReview int               `json:"needs_review"`
	Unmatched   int               `json:"unmatched"`
	Errors      []MatchErrorEntry `json:"errors"`
}

// Run matches every active video in the catalog that doesn't already have
// a matches row.
func (m *Matcher) Run(ctx context.Context) (Result, error) {
	videos, err := m.Catalog.ListActiveUnmatched()
	if err != nil {
		return Result{}, fmt.Errorf("list active unmatched: %w", err)
	}

	result := Result{}
	now := time.Now().Unix()

	for _, v := range videos {
		base := filepath.Base(v.Path)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		folder := filepath.Dir(v.Path)
		if folder == "." {
			folder = ""
		}

		artist, title, confidence := ParseFilename(name, folder)
		if confidence == 0 {
			if err := m.Catalog.UpsertMatch(&catalog.Match{
				VideoID:   v.ID,
				Provider:  m.Provider.Name(),
				Status:    catalog.MatchStatusUnmatched,
				MatchedAt: now,
			}); err != nil {
				return Result{}, fmt.Errorf("upsert unmatched %s: %w", v.Path, err)
			}
			result.Unmatched++
			continue
		}

		candidates, err := m.Provider.Match(ctx, provider.MatchQuery{Filename: base, Artist: artist, Title: title})
		if err != nil {
			result.Errors = append(result.Errors, MatchErrorEntry{Path: v.Path, Message: err.Error()})
			continue
		}

		if err := m.applyResult(v.ID, artist, title, candidates, now, &result); err != nil {
			return Result{}, fmt.Errorf("apply result %s: %w", v.Path, err)
		}
	}

	return result, nil
}

func (m *Matcher) applyResult(videoID int64, parsedArtist, parsedTitle string, candidates []provider.MatchResult, now int64, result *Result) error {
	entry := &catalog.Match{
		VideoID:      videoID,
		Provider:     m.Provider.Name(),
		ParsedArtist: nullString(parsedArtist),
		ParsedTitle:  nullString(parsedTitle),
		MatchedAt:    now,
	}

	if len(candidates) == 0 {
		entry.Status = catalog.MatchStatusUnmatched
		result.Unmatched++
		return m.Catalog.UpsertMatch(entry)
	}

	best := candidates[0]
	entry.Confidence = best.Confidence

	if best.Confidence < m.ConfidenceThreshold {
		entry.Status = catalog.MatchStatusNeedsReview
		result.NeedsReview++
		return m.Catalog.UpsertMatch(entry)
	}

	artistID, err := m.Catalog.UpsertArtist(best.Artist, best.ArtistSourceID, now)
	if err != nil {
		return fmt.Errorf("upsert artist: %w", err)
	}
	songID, err := m.Catalog.UpsertSong(artistID, best.Title, best.Album, best.Year, best.SourceID, now)
	if err != nil {
		return fmt.Errorf("upsert song: %w", err)
	}

	entry.Status = catalog.MatchStatusConfirmed
	entry.SongID = sql.NullInt64{Int64: songID, Valid: true}
	result.Confirmed++
	return m.Catalog.UpsertMatch(entry)
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/match/... -v`
Expected: PASS (all filename tests from Task 6 plus all Matcher tests from this task)

- [ ] **Step 5: Commit**

```bash
git add internal/match
git commit -m "Add matcher orchestration"
```

---

### Task 9: `internal/config` — match confidence threshold

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.MatchConfig{ConfidenceThreshold float64}`, `Config.Match MatchConfig` field, `Default()` now populates `Match.ConfidenceThreshold = 0.7`. Consumed by `internal/cli` (Task 10).

- [ ] **Step 1: Write the failing test**

Append this test function to the existing `internal/config/config_test.go` (the four existing test functions stay exactly as they are):

```go
func TestDefaultIncludesMatchConfidenceThreshold(t *testing.T) {
	cfg := config.Default("My Library")
	if cfg.Match.ConfidenceThreshold != 0.7 {
		t.Fatalf("expected default confidence threshold 0.7, got %f", cfg.Match.ConfidenceThreshold)
	}
}

func TestLoadOrCreateParsesExistingMatchSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "schema_version: 1\nlibrary:\n  name: Existing\nscan:\n  extensions: [mp4]\n  workers: 4\nmatch:\n  confidence_threshold: 0.85\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Ignored Default")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Match.ConfidenceThreshold != 0.85 {
		t.Fatalf("expected confidence threshold 0.85, got %f", cfg.Match.ConfidenceThreshold)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/... -run "TestDefaultIncludesMatchConfidenceThreshold|TestLoadOrCreateParsesExistingMatchSection" -v`
Expected: FAIL — `cfg.Match undefined (type config.Config has no field or method Match)`

- [ ] **Step 3: Add `MatchConfig` and wire it into `Config`/`Default`**

In `internal/config/config.go`, change the `Config` struct to add a `Match` field:

```go
// Config is the per-library configuration stored at
// <library-root>/.musicvideo-manager/config.yaml.
type Config struct {
	SchemaVersion int         `yaml:"schema_version"`
	Library       Library     `yaml:"library"`
	Scan          ScanConfig  `yaml:"scan"`
	Match         MatchConfig `yaml:"match"`
}
```

Add the new `MatchConfig` type immediately after `ScanConfig`:

```go
type MatchConfig struct {
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
}
```

Change `Default` to populate it:

```go
// Default returns the default configuration for a newly discovered library.
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
	}
}
```

`Load`, `Save`, and `LoadOrCreate` are unchanged — YAML marshaling already handles the new field automatically via the struct tag.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (all 6 tests — the 4 pre-existing plus the 2 new ones)

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "Add match confidence threshold to config"
```

---

### Task 10: `internal/cli` — match command

**Files:**
- Create: `internal/cli/match.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/match_test.go`

**Interfaces:**
- Consumes: `config.LoadOrCreate` (prior milestone, now returns `cfg.Match.ConfidenceThreshold` too), `catalog.Open`/`ListNeedsReview` (Task 4), `musicbrainz.New` (Task 7), `match.Matcher`/`Result` (Task 8).
- Produces: `newMatchCmd() *cobra.Command`, registered in `NewRootCmd`. This is the last task in the plan — nothing downstream depends on it.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/match_test.go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestMatchCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"match", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when matching a library that hasn't been scanned yet")
	}
}

func TestMatchCommandJSONOutput(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".musicvideo-manager")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Simulate a completed scan by opening the catalog directly and
	// inserting one active video with an unparseable filename (no " - "
	// delimiter, no parent folder), so match completes without needing
	// real network access to MusicBrainz.
	catPath := filepath.Join(stateDir, "catalog.sqlite")
	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	// Creating the catalog file directly via the scan command against an
	// empty directory is the simplest way to get a real, migrated
	// catalog.sqlite on disk without duplicating catalog.Open here.
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}
	if _, err := os.Stat(catPath); err != nil {
		t.Fatalf("expected catalog.sqlite to exist after scan: %v", err)
	}

	var out bytes.Buffer
	matchCmd := cli.NewRootCmd("test")
	matchCmd.SetOut(&out)
	matchCmd.SetArgs([]string{"match", root, "--json"})

	if err := matchCmd.Execute(); err != nil {
		t.Fatalf("match Execute: %v", err)
	}

	var result struct {
		Confirmed   int `json:"confirmed"`
		NeedsReview int `json:"needs_review"`
		Unmatched   int `json:"unmatched"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	// The scanned directory is empty (no video files), so there's nothing
	// to match -- this test exercises the command's plumbing (config load,
	// catalog open, Matcher wiring, JSON encoding), not matching logic
	// itself, which is covered by internal/match's own tests.
	if result.Confirmed != 0 || result.NeedsReview != 0 || result.Unmatched != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}

func TestMatchCommandListReview(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	listCmd := cli.NewRootCmd("test")
	listCmd.SetOut(&out)
	listCmd.SetArgs([]string{"match", root, "--list-review", "--json"})

	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list-review Execute: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("expected valid JSON array output, got error %v; output: %s", err, out.String())
	}
	if len(items) != 0 {
		t.Fatalf("expected an empty review queue for an empty library, got %+v", items)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestMatchCommand -v`
Expected: FAIL — `unknown command "match"` (the `match` subcommand doesn't exist yet)

- [ ] **Step 3: Implement `match.go`**

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
	"github.com/keif/musicvideo-manager/internal/match"
	"github.com/keif/musicvideo-manager/internal/providers/musicbrainz"
)

const musicBrainzUserAgent = "musicvideo-manager/dev (https://github.com/keif/musicvideo-manager)"

func newMatchCmd() *cobra.Command {
	var jsonOutput bool
	var listReview bool

	cmd := &cobra.Command{
		Use:   "match [path]",
		Short: "Identify catalogued videos against MusicBrainz",
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

			if listReview {
				return printReviewQueue(cmd, cat, jsonOutput)
			}

			matcher := &match.Matcher{
				Catalog:             cat,
				Provider:            musicbrainz.New(cat, musicBrainzUserAgent),
				ConfidenceThreshold: cfg.Match.ConfidenceThreshold,
			}

			result, err := matcher.Run(cmd.Context())
			if err != nil {
				return fmt.Errorf("match: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Matched: %d confirmed, %d needs review, %d unmatched, %d errors\n",
				result.Confirmed, result.NeedsReview, result.Unmatched, len(result.Errors))
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.Path, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&listReview, "list-review", false, "list videos needing manual review instead of matching")
	return cmd
}

func printReviewQueue(cmd *cobra.Command, cat *catalog.Catalog, jsonOutput bool) error {
	items, err := cat.ListNeedsReview()
	if err != nil {
		return fmt.Errorf("list needs review: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	for _, item := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n  parsed: %s - %s (confidence %.2f)\n",
			item.VideoPath, item.ParsedArtist, item.ParsedTitle, item.Confidence)
	}
	return nil
}
```

`catalog.ReviewItem` (from Task 4) already carries the `json` tags `printReviewQueue`'s `--json` path needs — no catalog changes required here.

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go`, change `NewRootCmd` to also register `match`:

```go
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "musicvideo-manager",
		Short: "Organize, enrich, and manage a local music video collection",
	}
	root.AddCommand(newScanCmd())
	root.AddCommand(newMatchCmd())
	root.AddCommand(newVersionCmd(version))
	return root
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across every package (`pkg/provider`, `internal/catalog`, `internal/config`, `internal/match`, `internal/providers/musicbrainz`, `internal/scanner`, `internal/cli`)

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 6: Manual smoke test against a real library**

Run: `go run ./cmd/musicvideo-manager match "/Volumes/Yggdrasil/Music Videos" --json | head -20`
Expected: this library was already scanned in the prior milestone (624 videos catalogued). A real MusicBrainz match run will take real wall-clock time (bounded by the 1 req/sec rate limit on cache misses) — this is a real, live run against the real MusicBrainz API, not a mock. Report the actual JSON summary. Then run `go run ./cmd/musicvideo-manager match "/Volumes/Yggdrasil/Music Videos" --list-review | head -20` and report a representative excerpt of the review queue.

- [ ] **Step 7: Commit**

```bash
git add internal/cli
git commit -m "Add match and match --list-review CLI commands"
```

---

### Task 11: ADR — MusicBrainz rate limiting, caching, and ID separation

**Files:**
- Create: `docs/adr/0006-musicbrainz-provider-integration.md`

**Interfaces:**
- Consumes: decisions made in this plan's design spec (`docs/superpowers/specs/2026-07-28-metadata-matching-design.md`) and this plan's Global Constraints.
- Produces: nothing consumed by code; historical record for contributors.

- [ ] **Step 1: Write the ADR**

```markdown
# 6. MusicBrainz provider: rate limiting, response caching, and artist/recording ID separation

Status: Accepted

## Context

MusicBrainz is the first real implementation of the `pkg/provider.MetadataProvider`
interface (previously just an interface with a fake test double). Its API
usage policy requires at most one request per second (unauthenticated) and
a descriptive `User-Agent` header identifying the calling application —
violating either can get an IP rate-limited or blocked. Separately, the
project's own stated goal is to "never repeatedly hit APIs": every query
should be cacheable so re-running `match` costs nothing once a video has
been looked up.

While implementing the provider, `MatchResult.SourceID` (built in the
prior milestone as a single generic provider ID field, documented as
"MBID, Discogs ID, etc.") turned out to be ambiguous: a MusicBrainz
recording search result carries both a recording MBID and an artist MBID,
and the catalog schema needs to store both (`songs.mbid` and
`artists.mbid`) to avoid creating duplicate artist rows across different
songs by the same artist.

## Decision

`MusicBrainzProvider` enforces the rate limit itself via an internal
mutex-guarded "time since last request" check before every outbound call,
overridable in tests via a `MinInterval` field so the test suite runs in
milliseconds rather than real seconds. Every response — including empty
results — is cached in the catalog's `provider_cache` table, keyed by a
hash of the normalized (artist, title) query, before parsing; a cache hit
skips both the network call and the rate limiter entirely.

`pkg/provider.MatchResult` gained a second ID field, `ArtistSourceID`,
alongside the existing `SourceID` (which now explicitly represents the
matched song/recording). The Matcher upserts artists by `ArtistSourceID`
first (falling back to name if empty) and songs by `SourceID` (falling
back to artist+title if empty), so the same artist across multiple songs
resolves to one `artists` row rather than one per song.

## Consequences

- Future providers (Discogs, TheAudioDB, Last.fm) inherit the same
  `ArtistSourceID`/`SourceID` split for free — no further interface
  changes needed for that dimension.
- `provider_cache` has no TTL or invalidation in this phase: a cached
  MusicBrainz response is kept forever, even if the underlying MusicBrainz
  data later changes. This is a deliberate MVP deferral — clearing or
  refreshing the cache requires manual SQL against `catalog.sqlite` for
  now, since no CLI command for it exists yet. Worth a `doctor`-style
  command in a later milestone.
- The rate limiter is per-provider-instance, not global or persisted
  across process runs. Two concurrent `match` invocations against the same
  library would each maintain their own independent rate limit and could
  jointly exceed MusicBrainz's 1 req/sec policy. Not a concern today since
  nothing in this project runs `match` concurrently against the same
  library, but worth remembering if that ever changes.
```

- [ ] **Step 2: Commit**

```bash
git add docs/adr
git commit -m "Add ADR for MusicBrainz rate limiting, caching, and ID separation"
```

---

## Plan Self-Review Notes

- **Spec coverage:** package layout ✓ (Task 2/6/7/8), schema (`artists`/`songs`/`matches`/`provider_cache`) ✓ (Task 2), filename parsing (3-tier pattern, real-data-validated) ✓ (Task 6), MusicBrainz provider (rate limit, User-Agent, caching, JSON parsing) ✓ (Task 7), Matcher orchestration (skip-on-unparseable, error-tolerant, confidence threshold, upsert-not-history) ✓ (Task 8), config `match.confidence_threshold` ✓ (Task 9), CLI (`match`, `match --list-review`, both with `--json`) ✓ (Task 10), testing (unit + fake-provider + httptest-mocked provider + real-library smoke test) ✓ (all tasks), ADR for the MusicBrainz-specific decisions and the `ArtistSourceID` interface change ✓ (Task 11). Explicitly-deferred spec items (embedded tags, Discogs/TheAudioDB/Last.fm, review-editing command, cache TTL) have no task, correctly — they're out of scope per the spec's Non-goals.
- **Placeholder scan:** none found — every step has complete, runnable code or exact file content.
- **Type consistency:** verified `catalog.Artist`/`Song`/`Match`/`ReviewItem`/`MatchStatus` field names and the `UpsertArtist`/`UpsertSong`/`UpsertMatch`/`ListActiveUnmatched`/`ListNeedsReview`/`GetCachedResponse`/`SetCachedResponse` signatures match exactly between the tasks that define them (2-5) and the tasks that consume them (7, 8, 10). `provider.MatchResult.ArtistSourceID` (Task 1) is consumed consistently by Task 7 (produced from MusicBrainz's artist-credit JSON) and Task 8 (passed to `UpsertArtist`). Caught and fixed one gap during this review: Task 4's `ReviewItem` struct as originally drafted had no `json` tags, which Task 10's `--json` output path needs — fixed by adding the tags directly to Task 4's struct definition (rather than patching them in via a cross-task note in Task 10), so `ReviewItem` is fully correct the first time it's defined.
