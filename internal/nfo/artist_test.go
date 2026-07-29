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
