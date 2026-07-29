# Artist Artwork (fanart.tv) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Download and cache artist-level artwork (thumb, fanart background, HD logo, banner) from fanart.tv for every distinct artist among confirmed matches, keyed by the MusicBrainz artist IDs already in the catalog.

**Architecture:** `internal/providers/fanarttv` mirrors the existing MusicBrainz provider's cache-check → rate-limit → fetch → cache-store shape, reusing the catalog's existing `provider_cache` table. `internal/artwork` orchestrates: one artwork fetch per distinct artist folder, downloading each available image type only if it isn't already on disk. A new `artwork` CLI command drives it.

**Tech Stack:** Go, `net/http`/`encoding/json` (standard library, no new dependency), the existing catalog and provider-cache infrastructure, Cobra CLI — same stack as every prior milestone.

## Global Constraints

- Module path: `github.com/keif/musicvideo-manager`.
- No cgo.
- No AI/model attribution anywhere in code, commits, or comments — every commit message plain and descriptive, no "Co-Authored-By" or similar trailer.
- The fanart.tv API key comes from the `FANARTTV_API_KEY` environment variable only — never read from or written to `config.yaml`, matching the existing ADR 0002 decision that provider credentials never live in the per-library config.
- Only artists with a non-empty MusicBrainz ID are queried (skip, don't error, if empty).
- fanart.tv's JSON response is cached via the **existing** `GetCachedResponse`/`SetCachedResponse` catalog methods (`provider="fanarttv"`) — no new cache table. This applies to both real artwork responses and "artist has no fanart.tv entry" (cache an empty result so that negative outcome isn't re-queried either).
- An artwork file that already exists on disk is never overwritten — only missing files are downloaded. This is different from the NFO writer's always-overwrite behavior, and deliberately so (small XML vs. multi-MB images).
- Real fanart.tv JSON field names (verified against the API's real documentation, not guessed): `artistthumb`, `artistbackground`, `hdmusiclogo`, `musicbanner`, each an array of `{id, url, lang, likes}` objects.
- fanart.tv does not publish a strict numeric rate limit the way MusicBrainz does (soft, per-key, rarely enforced) — the client still uses a conservative, non-zero, overridable `MinInterval` as polite default behavior, not because a specific limit is being complied with.

---

### Task 1: `internal/catalog` — confirmed artist/video query

**Files:**
- Modify: `internal/catalog/match.go`
- Test: `internal/catalog/match_test.go`

**Interfaces:**
- Consumes: `matches`/`videos`/`songs`/`artists` tables and the `MatchStatusConfirmed` const (all from the Metadata Matching milestone, already in this file).
- Produces: `catalog.ConfirmedArtistVideo{VideoPath, Artist, ArtistMBID string}`, `(*Catalog) ListConfirmedArtistVideos() ([]ConfirmedArtistVideo, error)`. Consumed by `internal/artwork` (Task 3).

- [ ] **Step 1: Write the failing test**

Append to `internal/catalog/match_test.go`:

```go
func TestListConfirmedArtistVideos(t *testing.T) {
	cat := openTestCatalog(t)

	videoID := insertTestVideo(t, cat, "Bon Jovi/Wanted Dead or Alive.mp4")
	artistID, err := cat.UpsertArtist("Bon Jovi", "artist-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Wanted Dead or Alive", "Slippery When Wet", 1986, "song-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0].VideoPath != "Bon Jovi/Wanted Dead or Alive.mp4" || rows[0].Artist != "Bon Jovi" || rows[0].ArtistMBID != "artist-mbid-1" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func TestListConfirmedArtistVideosHandlesEmptyMBID(t *testing.T) {
	cat := openTestCatalog(t)

	videoID := insertTestVideo(t, cat, "Unknown Artist/Song.mp4")
	artistID, err := cat.UpsertArtist("Unknown Artist", "", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Song", "", 0, "", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 1 || rows[0].ArtistMBID != "" {
		t.Fatalf("expected 1 row with empty ArtistMBID, got %+v", rows)
	}
}

func TestListConfirmedArtistVideosMultipleVideosSameArtist(t *testing.T) {
	cat := openTestCatalog(t)

	artistID, err := cat.UpsertArtist("Metallica", "artist-mbid-2", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	for _, title := range []string{"One", "Enter Sandman"} {
		videoID := insertTestVideo(t, cat, "Metallica/"+title+".mp4")
		songID, err := cat.UpsertSong(artistID, title, "", 0, "song-"+title, 100)
		if err != nil {
			t.Fatalf("UpsertSong: %v", err)
		}
		if err := cat.UpsertMatch(&catalog.Match{
			VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
			Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
		}); err != nil {
			t.Fatalf("UpsertMatch: %v", err)
		}
	}

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (one per video, same artist), got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.ArtistMBID != "artist-mbid-2" {
			t.Fatalf("expected consistent artist MBID across both rows, got %+v", r)
		}
	}
}

func TestListConfirmedArtistVideosEmpty(t *testing.T) {
	cat := openTestCatalog(t)

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}
```

This reuses the already-defined `openTestCatalog`/`insertTestVideo` helpers from this same test file (Metadata Matching milestone) — do not redefine them.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/catalog/... -run TestListConfirmedArtistVideos -v`
Expected: FAIL — `undefined: catalog.ConfirmedArtistVideo` (or similar)

- [ ] **Step 3: Implement the change**

Append to `internal/catalog/match.go`:

```go
// ConfirmedArtistVideo is one confirmed-match video's artist info -- the
// input the artwork writer uses to derive one artwork fetch per distinct
// artist folder (grouping by folder happens in the writer, mirroring how
// the NFO writer deduplicates artist.nfo per folder).
type ConfirmedArtistVideo struct {
	VideoPath  string
	Artist     string
	ArtistMBID string
}

// ListConfirmedArtistVideos returns one row per confirmed-match video,
// carrying its artist's name and MusicBrainz ID, ordered by video path.
func (c *Catalog) ListConfirmedArtistVideos() ([]ConfirmedArtistVideo, error) {
	rows, err := c.db.Query(`SELECT v.path, a.name, a.mbid
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

	out := []ConfirmedArtistVideo{}
	for rows.Next() {
		var cav ConfirmedArtistVideo
		var mbid sql.NullString
		if err := rows.Scan(&cav.VideoPath, &cav.Artist, &mbid); err != nil {
			return nil, err
		}
		cav.ArtistMBID = mbid.String
		out = append(out, cav)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/catalog/... -v`
Expected: PASS (new tests plus every pre-existing catalog test)

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "Add confirmed artist/video query for artwork lookups"
```

---

### Task 2: `internal/providers/fanarttv` — client

**Files:**
- Create: `internal/providers/fanarttv/fanarttv.go`
- Test: `internal/providers/fanarttv/fanarttv_test.go`

**Interfaces:**
- Consumes: `catalog.GetCachedResponse`/`SetCachedResponse` (Metadata Matching milestone).
- Produces: `fanarttv.Artwork{ThumbURL, BackgroundURL, LogoURL, BannerURL string}`, `fanarttv.Client{Catalog, APIKey, HTTPClient, BaseURL, MinInterval}`, `fanarttv.New(cat *catalog.Catalog, apiKey string) *Client`, `(*Client) ArtworkForArtist(ctx context.Context, artistMBID string) (Artwork, error)`. Consumed by `internal/artwork` (Task 3) and `internal/cli` (Task 4).

This task's tests use `httptest.Server` mocks, NOT live network calls — `BaseURL` and `MinInterval` are overridable struct fields for exactly this reason, mirroring the MusicBrainz provider's test pattern.

- [ ] **Step 1: Write the failing test**

```go
// internal/providers/fanarttv/fanarttv_test.go
package fanarttv_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/fanarttv"
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

const sampleArtistJSON = `{
  "name": "Bon Jovi",
  "mbid_id": "artist-mbid-1",
  "artistthumb": [{"id": "1", "url": "https://example.com/thumb.jpg", "lang": "", "likes": "5"}],
  "artistbackground": [{"id": "2", "url": "https://example.com/bg.jpg", "lang": "", "likes": "3"}],
  "hdmusiclogo": [{"id": "3", "url": "https://example.com/logo.png", "lang": "en", "likes": "2"}],
  "musicbanner": [{"id": "4", "url": "https://example.com/banner.jpg", "lang": "en", "likes": "1"}]
}`

func TestArtworkForArtistCacheMissCallsHTTPThenCaches(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if r.URL.Query().Get("api_key") != "test-key" {
			t.Errorf("expected api_key=test-key, got %q", r.URL.Query().Get("api_key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleArtistJSON))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	c := fanarttv.New(cat, "test-key")
	c.BaseURL = server.URL
	c.MinInterval = time.Millisecond

	art, err := c.ArtworkForArtist(context.Background(), "artist-mbid-1")
	if err != nil {
		t.Fatalf("ArtworkForArtist: %v", err)
	}
	if art.ThumbURL != "https://example.com/thumb.jpg" {
		t.Fatalf("unexpected ThumbURL: %q", art.ThumbURL)
	}
	if art.BackgroundURL != "https://example.com/bg.jpg" {
		t.Fatalf("unexpected BackgroundURL: %q", art.BackgroundURL)
	}
	if art.LogoURL != "https://example.com/logo.png" {
		t.Fatalf("unexpected LogoURL: %q", art.LogoURL)
	}
	if art.BannerURL != "https://example.com/banner.jpg" {
		t.Fatalf("unexpected BannerURL: %q", art.BannerURL)
	}

	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", n)
	}

	if _, err := c.ArtworkForArtist(context.Background(), "artist-mbid-1"); err != nil {
		t.Fatalf("second ArtworkForArtist: %v", err)
	}
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected still 1 HTTP request after cache hit, got %d", n)
	}
}

func TestArtworkForArtistNotFoundReturnsEmptyArtworkNoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"error","error message":"No results found"}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	c := fanarttv.New(cat, "test-key")
	c.BaseURL = server.URL
	c.MinInterval = time.Millisecond

	art, err := c.ArtworkForArtist(context.Background(), "unknown-mbid")
	if err != nil {
		t.Fatalf("expected no error for a 404 (no artwork), got: %v", err)
	}
	if art.ThumbURL != "" || art.BackgroundURL != "" || art.LogoURL != "" || art.BannerURL != "" {
		t.Fatalf("expected empty Artwork, got %+v", art)
	}
}

func TestArtworkForArtistNotFoundIsCached(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"error"}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	c := fanarttv.New(cat, "test-key")
	c.BaseURL = server.URL
	c.MinInterval = time.Millisecond

	if _, err := c.ArtworkForArtist(context.Background(), "unknown-mbid"); err != nil {
		t.Fatalf("first ArtworkForArtist: %v", err)
	}
	if _, err := c.ArtworkForArtist(context.Background(), "unknown-mbid"); err != nil {
		t.Fatalf("second ArtworkForArtist: %v", err)
	}
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected the not-found result to be cached (1 request total), got %d", n)
	}
}

func TestArtworkForArtistNonNotFoundErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	c := fanarttv.New(cat, "test-key")
	c.BaseURL = server.URL
	c.MinInterval = time.Millisecond

	if _, err := c.ArtworkForArtist(context.Background(), "some-mbid"); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestArtworkForArtistEnforcesRateLimit(t *testing.T) {
	var timestamps []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		_, _ = w.Write([]byte(`{"artistthumb": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	c := fanarttv.New(cat, "test-key")
	c.BaseURL = server.URL
	c.MinInterval = 50 * time.Millisecond

	if _, err := c.ArtworkForArtist(context.Background(), "mbid-one"); err != nil {
		t.Fatalf("first ArtworkForArtist: %v", err)
	}
	if _, err := c.ArtworkForArtist(context.Background(), "mbid-two"); err != nil {
		t.Fatalf("second ArtworkForArtist: %v", err)
	}

	if len(timestamps) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(timestamps))
	}
	if elapsed := timestamps[1].Sub(timestamps[0]); elapsed < 50*time.Millisecond {
		t.Fatalf("expected at least 50ms between requests, got %v", elapsed)
	}
}

func TestName(t *testing.T) {
	cat := openTestCatalog(t)
	c := fanarttv.New(cat, "test-key")
	if c.Name() != "fanarttv" {
		t.Fatalf("expected Name() %q, got %q", "fanarttv", c.Name())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/providers/fanarttv/... -v`
Expected: FAIL — `undefined: fanarttv.New` (the package doesn't exist yet)

- [ ] **Step 3: Implement `fanarttv.go`**

```go
package fanarttv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

const (
	defaultBaseURL     = "https://webservice.fanart.tv/v3/music"
	defaultMinInterval = 500 * time.Millisecond
	providerName       = "fanarttv"
)

// Client fetches artist artwork from fanart.tv, keyed by MusicBrainz
// artist ID. Every response is cached via Catalog so a repeated query
// never hits the network twice, including "no artwork found" results.
type Client struct {
	Catalog     *catalog.Catalog
	APIKey      string
	HTTPClient  *http.Client
	BaseURL     string
	MinInterval time.Duration

	mu          sync.Mutex
	lastRequest time.Time
}

// New returns a Client configured for the real fanart.tv API.
func New(cat *catalog.Catalog, apiKey string) *Client {
	return &Client{
		Catalog:     cat,
		APIKey:      apiKey,
		HTTPClient:  http.DefaultClient,
		BaseURL:     defaultBaseURL,
		MinInterval: defaultMinInterval,
	}
}

func (c *Client) Name() string { return providerName }

// Artwork holds up to one URL per artwork type for a single artist.
type Artwork struct {
	ThumbURL      string
	BackgroundURL string
	LogoURL       string
	BannerURL     string
}

// ArtworkForArtist returns the available artwork URLs for the given
// MusicBrainz artist ID. An artist with no fanart.tv entry returns a
// zero-value Artwork and a nil error -- this is a normal outcome, not a
// failure, and is cached the same as a real result so it isn't re-queried.
func (c *Client) ArtworkForArtist(ctx context.Context, artistMBID string) (Artwork, error) {
	if cached, ok, err := c.Catalog.GetCachedResponse(providerName, artistMBID); err != nil {
		return Artwork{}, fmt.Errorf("check cache: %w", err)
	} else if ok {
		return parseArtistResponse([]byte(cached))
	}

	c.waitForRateLimit()

	body, notFound, err := c.fetch(ctx, artistMBID)
	if err != nil {
		return Artwork{}, fmt.Errorf("fanart.tv fetch: %w", err)
	}
	if notFound {
		body = []byte("{}")
	}

	if err := c.Catalog.SetCachedResponse(providerName, artistMBID, string(body), time.Now().Unix()); err != nil {
		return Artwork{}, fmt.Errorf("cache response: %w", err)
	}

	return parseArtistResponse(body)
}

func (c *Client) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lastRequest.IsZero() {
		if elapsed := time.Since(c.lastRequest); elapsed < c.MinInterval {
			time.Sleep(c.MinInterval - elapsed)
		}
	}
	c.lastRequest = time.Now()
}

func (c *Client) fetch(ctx context.Context, artistMBID string) (body []byte, notFound bool, err error) {
	u := c.BaseURL + "/" + artistMBID + "?api_key=" + c.APIKey

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return body, false, nil
}

type artistResponse struct {
	ArtistThumb      []imageEntry `json:"artistthumb"`
	ArtistBackground []imageEntry `json:"artistbackground"`
	HDMusicLogo      []imageEntry `json:"hdmusiclogo"`
	MusicBanner      []imageEntry `json:"musicbanner"`
}

type imageEntry struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes string `json:"likes"`
}

func parseArtistResponse(body []byte) (Artwork, error) {
	var parsed artistResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Artwork{}, fmt.Errorf("parse fanart.tv response: %w", err)
	}

	art := Artwork{}
	if len(parsed.ArtistThumb) > 0 {
		art.ThumbURL = parsed.ArtistThumb[0].URL
	}
	if len(parsed.ArtistBackground) > 0 {
		art.BackgroundURL = parsed.ArtistBackground[0].URL
	}
	if len(parsed.HDMusicLogo) > 0 {
		art.LogoURL = parsed.HDMusicLogo[0].URL
	}
	if len(parsed.MusicBanner) > 0 {
		art.BannerURL = parsed.MusicBanner[0].URL
	}
	return art, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/providers/fanarttv/... -v`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/providers
git commit -m "Add fanart.tv artist artwork client"
```

---

### Task 3: `internal/artwork` — writer orchestration

**Files:**
- Create: `internal/artwork/writer.go`
- Test: `internal/artwork/writer_test.go`

**Interfaces:**
- Consumes: `catalog.ListConfirmedArtistVideos` (Task 1), `fanarttv.Client`/`Artwork`/`ArtworkForArtist` (Task 2).
- Produces: `artwork.ArtworkErrorEntry{Artist, Message string}`, `artwork.Result{ArtistsProcessed, ImagesWritten, ImagesSkipped, NoArtwork int, Errors []ArtworkErrorEntry}`, `artwork.WriteAll(ctx context.Context, cat *catalog.Catalog, client *fanarttv.Client, root string) (Result, error)`. Consumed by `internal/cli` (Task 4).

This task's tests use a real temporary directory and a real `httptest.Server` serving both the fanart.tv JSON response and the raw image bytes it links to — exercising the real `Client`/`downloadFile` code paths end-to-end, not a hand-rolled fake.

- [ ] **Step 1: Write the failing test**

```go
// internal/artwork/writer_test.go
package artwork_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keif/musicvideo-manager/internal/artwork"
	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/fanarttv"
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

func setupConfirmedVideo(t *testing.T, cat *catalog.Catalog, root, relPath, artist, artistMBID, title string) {
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
	artistID, err := cat.UpsertArtist(artist, artistMBID, 1)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, title, "", 0, "", 1)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "test", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
}

// newArtworkTestServer serves a fanart.tv-shaped JSON response (with image
// URLs pointing back at itself) at /music/{mbid}, and raw image bytes at
// /images/{name}. Self-referencing the server's own address via r.Host
// sidesteps the chicken-and-egg problem of needing the server's URL before
// httptest.NewServer returns it.
func newArtworkTestServer(t *testing.T, hasArtwork bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/music/", func(w http.ResponseWriter, r *http.Request) {
		if !hasArtwork {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":"error"}`))
			return
		}
		base := "http://" + r.Host
		body := fmt.Sprintf(`{
			"artistthumb": [{"id":"1","url":"%s/images/thumb.jpg","lang":"","likes":"1"}],
			"artistbackground": [{"id":"2","url":"%s/images/fanart.jpg","lang":"","likes":"1"}],
			"hdmusiclogo": [{"id":"3","url":"%s/images/logo.png","lang":"","likes":"1"}],
			"musicbanner": [{"id":"4","url":"%s/images/banner.jpg","lang":"","likes":"1"}]
		}`, base, base, base, base)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake-image-bytes:" + r.URL.Path))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func newTestClient(t *testing.T, cat *catalog.Catalog, server *httptest.Server) *fanarttv.Client {
	t.Helper()
	c := fanarttv.New(cat, "test-key")
	c.BaseURL = server.URL + "/music"
	c.MinInterval = time.Millisecond
	return c
}

func TestWriteAllWritesAllFourArtworkTypes(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Bon Jovi/Wanted Dead or Alive.mp4", "Bon Jovi", "artist-mbid-1", "Wanted Dead or Alive")

	server := newArtworkTestServer(t, true)
	client := newTestClient(t, cat, server)

	result, err := artwork.WriteAll(context.Background(), cat, client, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ArtistsProcessed != 1 || result.ImagesWritten != 4 || result.ImagesSkipped != 0 || result.NoArtwork != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	for _, name := range []string{"folder.jpg", "fanart.jpg", "clearlogo.png", "banner.jpg"} {
		if _, err := os.Stat(filepath.Join(root, "Bon Jovi", name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestWriteAllSkipsExistingFiles(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Bon Jovi/Wanted Dead or Alive.mp4", "Bon Jovi", "artist-mbid-1", "Wanted Dead or Alive")

	if err := os.WriteFile(filepath.Join(root, "Bon Jovi", "folder.jpg"), []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	server := newArtworkTestServer(t, true)
	client := newTestClient(t, cat, server)

	result, err := artwork.WriteAll(context.Background(), cat, client, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ImagesWritten != 3 || result.ImagesSkipped != 1 {
		t.Fatalf("expected 3 written + 1 skipped, got %+v", result)
	}

	content, err := os.ReadFile(filepath.Join(root, "Bon Jovi", "folder.jpg"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "already here" {
		t.Fatalf("expected existing file to be left untouched, got %q", content)
	}
}

func TestWriteAllOneArtistFetchPerFolderNotPerVideo(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Metallica/One.mp4", "Metallica", "artist-mbid-2", "One")
	setupConfirmedVideo(t, cat, root, "Metallica/Enter Sandman.mp4", "Metallica", "artist-mbid-2", "Enter Sandman")

	var requestCount int
	server := newArtworkTestServer(t, true)
	origHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/music/artist-mbid-2" {
			requestCount++
		}
		origHandler.ServeHTTP(w, r)
	})
	client := newTestClient(t, cat, server)

	result, err := artwork.WriteAll(context.Background(), cat, client, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ArtistsProcessed != 1 {
		t.Fatalf("expected 1 artist processed (one folder, two videos), got %d", result.ArtistsProcessed)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 fanart.tv request for the shared folder, got %d", requestCount)
	}
}

func TestWriteAllSkipsArtistWithNoMBID(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Unknown Artist/Song.mp4", "Unknown Artist", "", "Song")

	server := newArtworkTestServer(t, true)
	client := newTestClient(t, cat, server)

	result, err := artwork.WriteAll(context.Background(), cat, client, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ArtistsProcessed != 0 {
		t.Fatalf("expected 0 artists processed (no MBID), got %d", result.ArtistsProcessed)
	}
}

func TestWriteAllCountsNoArtworkArtist(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Obscure Band/Song.mp4", "Obscure Band", "artist-mbid-3", "Song")

	server := newArtworkTestServer(t, false)
	client := newTestClient(t, cat, server)

	result, err := artwork.WriteAll(context.Background(), cat, client, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ArtistsProcessed != 1 || result.NoArtwork != 1 || result.ImagesWritten != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestWriteAllEmptyLibrary(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)

	server := newArtworkTestServer(t, true)
	client := newTestClient(t, cat, server)

	result, err := artwork.WriteAll(context.Background(), cat, client, root)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ArtistsProcessed != 0 || result.ImagesWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/artwork/... -v`
Expected: FAIL — `undefined: artwork.WriteAll` (the `internal/artwork` package doesn't exist yet)

- [ ] **Step 3: Implement `writer.go`**

```go
package artwork

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/fanarttv"
)

// ArtworkErrorEntry is a single artist whose artwork fetch or a specific
// image download failed. Not persisted -- recorded in the run's Result only.
type ArtworkErrorEntry struct {
	Artist  string `json:"artist"`
	Message string `json:"message"`
}

// Result summarizes a WriteAll run.
type Result struct {
	ArtistsProcessed int                 `json:"artists_processed"`
	ImagesWritten    int                 `json:"images_written"`
	ImagesSkipped    int                 `json:"images_skipped"`
	NoArtwork        int                 `json:"no_artwork"`
	Errors           []ArtworkErrorEntry `json:"errors"`
}

// WriteAll fetches and writes fanart.tv artwork for every distinct artist
// folder among confirmed matches, rooted at root. Existing image files are
// never overwritten -- this only ever fills gaps.
func WriteAll(ctx context.Context, cat *catalog.Catalog, client *fanarttv.Client, root string) (Result, error) {
	videos, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		return Result{}, fmt.Errorf("list confirmed artist videos: %w", err)
	}

	result := Result{Errors: []ArtworkErrorEntry{}}
	processedDirs := map[string]bool{}

	for _, v := range videos {
		artistDir := filepath.Dir(filepath.Join(root, filepath.FromSlash(v.VideoPath)))
		if processedDirs[artistDir] {
			continue
		}
		processedDirs[artistDir] = true

		if v.ArtistMBID == "" {
			continue
		}

		result.ArtistsProcessed++

		art, err := client.ArtworkForArtist(ctx, v.ArtistMBID)
		if err != nil {
			result.Errors = append(result.Errors, ArtworkErrorEntry{Artist: v.Artist, Message: err.Error()})
			continue
		}

		if art.ThumbURL == "" && art.BackgroundURL == "" && art.LogoURL == "" && art.BannerURL == "" {
			result.NoArtwork++
			continue
		}

		targets := []struct {
			url      string
			filename string
		}{
			{art.ThumbURL, "folder.jpg"},
			{art.BackgroundURL, "fanart.jpg"},
			{art.LogoURL, "clearlogo.png"},
			{art.BannerURL, "banner.jpg"},
		}

		for _, t := range targets {
			if t.url == "" {
				continue
			}
			targetPath := filepath.Join(artistDir, t.filename)
			if _, err := os.Stat(targetPath); err == nil {
				result.ImagesSkipped++
				continue
			}

			if err := downloadFile(ctx, client.HTTPClient, t.url, targetPath); err != nil {
				result.Errors = append(result.Errors, ArtworkErrorEntry{Artist: v.Artist, Message: fmt.Sprintf("download %s: %v", t.filename, err)})
				continue
			}
			result.ImagesWritten++
		}
	}

	return result, nil
}

func downloadFile(ctx context.Context, httpClient *http.Client, url, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(targetPath)
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/artwork/... -v`
Expected: PASS (all 6 tests, plus Task 2's fanarttv tests still passing via `go test ./internal/providers/... ./internal/artwork/... -v`)

- [ ] **Step 5: Commit**

```bash
git add internal/artwork
git commit -m "Add artwork writer orchestration"
```

---

### Task 4: `internal/cli` — artwork command

**Files:**
- Create: `internal/cli/artwork.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/artwork_test.go`

**Interfaces:**
- Consumes: `catalog.Open`, `fanarttv.New` (Task 2), `artwork.WriteAll`/`Result` (Task 3), the `FANARTTV_API_KEY` environment variable.
- Produces: `newArtworkCmd() *cobra.Command`, registered in `NewRootCmd`. This is the last code task in the plan.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/artwork_test.go
package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestArtworkCommandRequiresAPIKey(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "")

	root := t.TempDir()
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"artwork", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when FANARTTV_API_KEY is not set")
	}
}

func TestArtworkCommandRequiresPriorScan(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "test-key")

	root := t.TempDir()
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"artwork", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when the library hasn't been scanned yet")
	}
}

func TestArtworkCommandJSONOutputEmptyLibrary(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "test-key")

	root := t.TempDir()
	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	artworkCmd := cli.NewRootCmd("test")
	artworkCmd.SetOut(&out)
	artworkCmd.SetArgs([]string{"artwork", root, "--json"})

	// An empty library has no confirmed matches, so WriteAll never makes a
	// real network call -- this exercises the command's plumbing (env var
	// check, config/catalog wiring, JSON encoding) without needing a mock
	// fanart.tv server.
	if err := artworkCmd.Execute(); err != nil {
		t.Fatalf("artwork Execute: %v", err)
	}

	var result struct {
		ArtistsProcessed int `json:"artists_processed"`
		ImagesWritten    int `json:"images_written"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.ArtistsProcessed != 0 || result.ImagesWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestArtworkCommand -v`
Expected: FAIL — `unknown command "artwork"`

- [ ] **Step 3: Implement `artwork.go`**

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/artwork"
	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/fanarttv"
)

func newArtworkCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "artwork [path]",
		Short: "Download artist artwork from fanart.tv for confirmed matches",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey := os.Getenv("FANARTTV_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("FANARTTV_API_KEY environment variable is not set")
			}

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

			client := fanarttv.New(cat, apiKey)
			result, err := artwork.WriteAll(cmd.Context(), cat, client, absRoot)
			if err != nil {
				return fmt.Errorf("write artwork: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Processed %d artists: %d images written, %d skipped, %d with no artwork, %d errors\n",
				result.ArtistsProcessed, result.ImagesWritten, result.ImagesSkipped, result.NoArtwork, len(result.Errors))
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.Artist, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
```

Note the API key check runs before any catalog/filesystem work — fail fast, consistent with the Rename Engine milestone's `--dry-run`/`--undo` mutual-exclusion check.

- [ ] **Step 4: Register the command in `root.go`**

In `internal/cli/root.go`, add `root.AddCommand(newArtworkCmd())` to `NewRootCmd` (alongside `scan`/`match`/`rename`/`nfo`/`version`).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across every package (`pkg/provider`, `internal/catalog`, `internal/config`, `internal/match`, `internal/providers/musicbrainz`, `internal/providers/fanarttv`, `internal/rename`, `internal/nfo`, `internal/artwork`, `internal/scanner`, `internal/cli`)

Run: `go build ./... && go vet ./...`
Expected: both exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "Add artwork CLI command"
```

---

### Task 5: ADR — fanart.tv artwork caching, skip-if-exists, and API key handling

**Files:**
- Create: `docs/adr/0008-fanarttv-artist-artwork.md`

**Interfaces:**
- Consumes: decisions made in this plan's design spec (`docs/superpowers/specs/2026-07-29-fanarttv-artwork-design.md`) and this plan's Global Constraints.
- Produces: nothing consumed by code; historical record for contributors.

- [ ] **Step 1: Write the ADR**

```markdown
# 8. fanart.tv artist artwork: cache reuse, skip-if-exists, and API key handling

Status: Accepted

## Context

This is the project's first artwork-downloading feature. Two prior
milestones already established patterns this one reuses directly: the
Metadata Matching milestone's `provider_cache` table (any provider's JSON
response, keyed by provider name + cache key) and its MusicBrainz client
shape (cache-check -> rate-limit -> fetch -> cache-store), and the Rename
Engine milestone's precedent of never storing provider credentials in the
per-library `config.yaml` (ADR 0002).

fanart.tv's music-artist API is keyed by MusicBrainz artist ID, which
every artist in the catalog already carries (100% coverage as of this
milestone, since `UpsertArtist` from Metadata Matching prefers MBID). This
made fanart.tv a natural first artwork source, needing no new
identification step.

Unlike MusicBrainz, fanart.tv does not publish a strict numeric rate limit
(it's soft, per-key, and rarely enforced in practice) and requires a
registered personal API key rather than anonymous access.

## Decision

The fanart.tv client (`internal/providers/fanarttv`) reuses the existing
`provider_cache` table and `GetCachedResponse`/`SetCachedResponse` catalog
methods rather than introducing a new cache table -- a fanart.tv JSON
response is architecturally the same shape of thing MusicBrainz's is (a
cacheable provider response keyed by provider name + key), just for a
different provider. Both real responses and "artist has no fanart.tv
entry" (HTTP 404) are cached, so a negative result isn't re-queried either.

The client's `MinInterval` field defaults to a conservative, non-zero
value as polite default behavior, not because a specific published fanart.tv
limit is being complied with -- the exact number is left as an
easily-adjustable default rather than asserted as fact.

The API key is read from the `FANARTTV_API_KEY` environment variable only,
never from `config.yaml`, extending ADR 0002's reasoning: a library's
config travels with the library (colocated in `.musicvideo-manager/`,
possibly backed up or shared), and a provider credential should not travel
with it.

Downloaded image bytes are cached implicitly by their presence on disk --
the writer checks `os.Stat` before downloading and skips if the target
file already exists, never overwriting. This differs from the NFO writer's
always-overwrite behavior: regenerating a small XML file is cheap, but
re-downloading a multi-megabyte image on every run is real, avoidable cost
and bandwidth.

## Consequences

- Running `artwork` before the real fanart.tv rate limit (if one is ever
  encountered) is understood requires checking a live `Retry-After`
  response, which this pass does not implement -- a 429 response is
  currently treated as a generic non-200 error rather than automatically
  retried. Acceptable for a first pass; worth revisiting if real usage
  ever hits it.
- Because artwork files are only ever filled in when missing, there is no
  way in this pass to force a refresh of artwork that later turns out to
  be wrong or outdated short of manually deleting the file first. A future
  `--force` flag is straightforward to add if that need arises.
- Artwork is fetched once per distinct artist *folder*, not per artist
  *name* -- consistent with how `nfo`'s `artist.nfo` is deduplicated, and
  correct because Kodi/Jellyfin/Emby read these files from the folder a
  video lives in, not from a name string.
```

- [ ] **Step 2: Commit**

```bash
git add docs/adr
git commit -m "Add ADR for fanart.tv artist artwork caching and API key handling"
```

---

### Task 6: Real verification against the live library

**Files:**
- None (no code changes — this task is verification only, mirroring Task 12 of the Rename Engine plan).

**Interfaces:**
- Consumes: `artwork` (Task 4), a real `FANARTTV_API_KEY` (you obtained one before this plan started), the real catalog at `/Volumes/Yggdrasil/Music Videos/.musicvideo-manager/catalog.sqlite` (already populated with 293 confirmed matches, 167 distinct artists, all with MusicBrainz IDs).
- Produces: a verified, reported real artwork-download run. Nothing downstream in this plan depends on it — this is the milestone's closing verification.

Unlike the Rename Engine milestone's closing task, running this for real is safe: `artwork` only ever adds new files, never moves or deletes anything, so there is no dry-run-only restriction here.

- [ ] **Step 1: Confirm the API key is available**

Run: `echo "FANARTTV_API_KEY is set: $([ -n "$FANARTTV_API_KEY" ] && echo yes || echo no)"`
Expected: `yes`. If `no`, STOP and report back — do not proceed with a fabricated or placeholder key.

- [ ] **Step 2: Run the real artwork download**

Run: `FANARTTV_API_KEY="$FANARTTV_API_KEY" go run ./cmd/musicvideo-manager artwork "/Volumes/Yggdrasil/Music Videos" --json`

Expected: valid JSON with `artists_processed` around 167 (one per distinct artist folder), some mix of `images_written`/`no_artwork` depending on how many of these artists fanart.tv actually has entries for, and ideally 0 or few `errors`. This is a real, live run against fanart.tv — respect the client's built-in rate-limit spacing; do not modify the code to remove it for a "faster" run.

- [ ] **Step 3: Spot-check a handful of downloaded images**

Pick 3-5 artist folders that received images and open a couple of the actual files (e.g. `file "/Volumes/Yggdrasil/Music Videos/<artist>/folder.jpg"` to confirm it's a real, non-empty image file and not an error page or truncated download; visually inspecting one or two if you have a way to render an image is a bonus but not required).

- [ ] **Step 4: Verify idempotency**

Run the exact same command from Step 2 a second time. Expected: `images_written` should now be `0` (or very close to it) since the files from the first run already exist, and `images_skipped` should roughly match the first run's `images_written` count.

- [ ] **Step 5: Report**

Summarize: total artists processed, images written/skipped/no-artwork/errors from both runs, the spot-checked file names and their file sizes (confirming they're real images, not empty/error files), and confirmation the second run was idempotent. This is the final task of the plan — no commit needed (no files changed).

---

## Plan Self-Review Notes

- **Spec coverage:** confirmed-artist-video query ✓ (Task 1), fanart.tv client with cache reuse and verified-real JSON field names ✓ (Task 2), writer with skip-if-exists and one-fetch-per-folder ✓ (Task 3), `FANARTTV_API_KEY` env-var-only credential handling ✓ (Task 2/4), `artwork` CLI command ✓ (Task 4), ADR covering the cache-reuse/skip-if-exists/API-key decisions ✓ (Task 5), real (not dry-run, since this is additive-safe) verification against the live library ✓ (Task 6). Explicitly out-of-scope spec items (local video thumbnails, album artwork, other providers, force-refresh, any UI) have no task, correctly.
- **Placeholder scan:** none found — every step has complete, runnable code or exact file content. The one place this plan names a genuine unresolved fact (fanart.tv's exact rate limit) is handled as a documented, deliberate default rather than a TBD — the architectural commitment (non-zero, overridable) is concrete even though the precise number isn't asserted as verified fact.
- **Type consistency:** verified `catalog.ConfirmedArtistVideo`, `fanarttv.Artwork`/`Client`, and `artwork.Result`/`ArtworkErrorEntry` field names and the functions that produce/consume them line up exactly between the tasks that define them (1, 2, 3) and the tasks that consume them (3, 4). `WriteAll`'s signature includes `ctx context.Context` as its first parameter — a small, deliberate addition beyond what the design spec's abbreviated signature showed, matching the established pattern from `scanner.Run(ctx, ...)` and `match.Matcher.Run(ctx)` for any orchestration function that makes network calls, since `fanarttv.Client.ArtworkForArtist` needs a context to pass through to its HTTP requests.
- **Real-codebase cross-check:** independently confirmed against the actual current source (not memory) that every signature this plan's tests rely on is exact: `catalog.Video` field names (`Path`, `FileSize`, `MTime`, `PartialHash`, `Status`, `FirstSeenAt`, `LastSeenAt`, `LastScannedAt`), `catalog.StatusActive`, `(*Catalog) InsertVideo(v *Video) (int64, error)`, `catalog.Match` field names including `SongID sql.NullInt64`, `UpsertArtist(name, mbid string, at int64) (int64, error)`, `UpsertSong(artistID int64, title, album string, year int, mbid string, at int64) (int64, error)`, and `cli.NewRootCmd(version string) *cobra.Command`. Also confirmed `internal/cli`'s existing test files declare no `TestMain`, so Task 4's test file needed none — the plan text was corrected in place to drop an unnecessary conditional `TestMain` block and its now-unused `os` import.
