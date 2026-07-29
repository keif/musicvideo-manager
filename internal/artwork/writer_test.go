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
