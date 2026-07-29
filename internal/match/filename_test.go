package match_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/match"
)

func TestParseFilenameDashDelimited(t *testing.T) {
	cases := []struct {
		filename      string
		folder        string
		wantArtist    string
		wantTitle     string
		minConfidence float64
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
