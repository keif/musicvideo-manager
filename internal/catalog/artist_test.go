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

	var found *catalog.Artist
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
