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
		{"AFI - Miss Murder Official Music Video Long Version [YU4hhNKsPog]", "AFI", "AFI", "Miss Murder", 0.8},
		{"Alanis Morissette - Ironic Official 4K Music Video [Jne9t8sHpUc]", "Alanis Morissette", "Alanis Morissette", "Ironic", 0.8},
		{"Backstreet Boys - I Want It That Way Official HD Video [4fndeDfaWCg]", "Backstreet Boys", "Backstreet Boys", "I Want It That Way", 0.8},
		{"Some Artist - Some Song Lyric Video", "Some Artist", "Some Artist", "Some Song", 0.8},
		{"Some Artist - Some Song Music Video", "Some Artist", "Some Artist", "Some Song", 0.8},
		{"Gotye - Somebody That I Used To Know feat. Kimbra Official Music Video [8UVNT4wvIGY]", "Gotye", "Gotye", "Somebody That I Used To Know", 0.8},
		{"D12 - My Band ft. Cameo Official Music Video [ttWQK5VXskA]", "D12", "D12", "My Band", 0.8},
		{"Blackstreet - No Diggity Official Music Video ft. Dr. Dre Queen Pen [3KL9mRus19o]", "Blackstreet", "Blackstreet", "No Diggity", 0.8},
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

// Older scene-rip filenames glue the artist directly onto the title with a
// bare hyphen ("Artist-Title...", no surrounding spaces), so dashPattern
// never fires. When the filename starts with the folder's own artist name,
// ParseFilename should strip that prefix instead of feeding the whole
// filename (artist + title + rip tags) to the provider as the title.
func TestParseFilenameArtistPrefixNoSpaces(t *testing.T) {
	cases := []struct {
		filename   string
		folder     string
		wantArtist string
		wantTitle  string
	}{
		{"Britney Spears-Toxic (MV) 1080i-CtrlHD", "Britney Spears", "Britney Spears", "Toxic"},
		{"Eminem-Like Toy Soldiers (MV) 1080i-CtrlHD", "Eminem", "Eminem", "Like Toy Soldiers"},
		{"Justin Timberlake-Cry Me A River (MV) 1080i-CtrlHD", "Justin Timberlake", "Justin Timberlake", "Cry Me A River"},
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
			if confidence <= 0.5 {
				t.Errorf("expected confidence above the generic folder-fallback 0.5, got %f", confidence)
			}
		})
	}
}

// Some bare-hyphen scene rips put the featured artist between the primary
// artist and the title ("Beyonce Feat. Sean Paul-Baby Boy..."), so after
// stripArtistPrefix removes "Beyonce" the leftover still isn't the title --
// it's "Feat. Sean Paul-Baby Boy", with the real title after the next
// hyphen.
func TestParseFilenameArtistPrefixWithFeatClause(t *testing.T) {
	cases := []struct {
		filename   string
		folder     string
		wantArtist string
		wantTitle  string
	}{
		{"Beyonce Feat. Sean Paul-Baby Boy (MV) 1080i-CtrlHD", "Beyonce", "Beyonce", "Baby Boy"},
		{"Kid Rock Feat. Sheryl Crow-Picture (MV) 1080i-CtrlHD", "Kid Rock", "Kid Rock", "Picture"},
		{"Justin Timberlake Feat. Timberland-Sexy Back (MV) 1080i-CtrlHD", "Justin Timberlake", "Justin Timberlake", "Sexy Back"},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			artist, title, _ := match.ParseFilename(tc.filename, tc.folder)
			if artist != tc.wantArtist {
				t.Errorf("artist: got %q, want %q", artist, tc.wantArtist)
			}
			if title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", title, tc.wantTitle)
			}
		})
	}
}
