package nfo_test

import (
	"database/sql"
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

func sqlNullInt64ForTest(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
