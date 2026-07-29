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
