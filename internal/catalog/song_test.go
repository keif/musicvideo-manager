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
