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
