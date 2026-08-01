package match

import (
	"regexp"
	"strings"
)

var dashPattern = regexp.MustCompile(`^(.+?)\s+-\s+(.+)$`)

// artistPrefixSeparators are the characters trimmed after stripping a
// leading artist-name prefix in stripArtistPrefix (hyphen, colon, and the
// en/em dashes some scene rips use in place of a plain "-").
const artistPrefixSeparators = " \t-–—:"

// suffixPatterns is applied in order within every cleanTitle pass, so a
// pattern that unlocks another (e.g. stripping "Long Version" so "Official
// Music Video" becomes the new trailing text) must come first in the list
// rather than waiting for the next outer-loop pass to pick it up.
var suffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*\[[^\]]*\]\s*$`),                                    // trailing [video id]
	regexp.MustCompile(`(?i)\s*(extended|long|full|clean|explicit)\s*version\s*$`), // "(Extended|Long|...) Version"
	regexp.MustCompile(`(?i)\s*\(\s*official(?:\s+\S+){0,3}\s+video\s*\)\s*$`),     // (Official [...] Video), any quality/descriptor words
	regexp.MustCompile(`(?i)\s*official(?:\s+\S+){0,3}\s+video\s*$`),              // bare Official [...] Video (HD/4K/Music/Long/etc.)
	regexp.MustCompile(`(?i)\s*-\s*video\s*$`),                                     // "-video" suffix
	regexp.MustCompile(`(?i)\s*official\s*hq\s*$`),                                 // "Official HQ"
	regexp.MustCompile(`(?i)\s*lyrics?\s*video\s*$`),                               // bare "Lyric(s) Video"
	regexp.MustCompile(`(?i)\s*(music\s*video|video\s*clip|videoclip)\s*$`),        // bare "Music Video" / "Videoclip"
	regexp.MustCompile(`(?i)\s*\(\s*(mv|hd|4k|hq)\s*\)\s*$`),                       // trailing "(MV)"/"(HD)"/"(4K)"/"(HQ)"
	regexp.MustCompile(`(?i)[\s\-]+mv\s*$`),                                        // trailing bare "-MV" marker
	regexp.MustCompile(`\s*~[^~]*~\s*$`),                                           // trailing "~TAG~" scene marker
	regexp.MustCompile(`(?i)[\s\-]*\b\d{3,4}[ip]\b.*$`),                            // trailing resolution/rip tag onward (1080i-CtrlHD, 720p...)
}

// ParseFilename guesses an artist and title from a video's base filename
// (extension already stripped by the caller) and its parent folder name.
// It tries progressively weaker patterns and returns the confidence of
// whichever one matched; confidence 0 means no usable signal was found at
// all, and callers should treat that as a signal to skip provider lookup
// entirely rather than querying with an empty/meaningless guess.
func ParseFilename(filename, folder string) (artist, title string, confidence float64) {
	name := strings.TrimSpace(filename)
	folder = strings.TrimSpace(folder)

	if name == "" {
		return "", "", 0
	}

	if m := dashPattern.FindStringSubmatch(name); m != nil {
		a := strings.TrimSpace(m[1])
		t := cleanTitle(m[2])
		if a != "" && t != "" {
			return a, t, 0.9
		}
	}

	if folder != "" {
		if rest, ok := stripArtistPrefix(name, folder); ok {
			return folder, cleanTitle(rest), 0.7
		}
		return folder, cleanTitle(name), 0.5
	}

	return "", "", 0
}

// stripArtistPrefix removes a leading "<artist><separator>" prefix from
// name, for scene-rip filenames that glue the artist directly onto the
// title with a bare hyphen ("Britney Spears-Toxic (MV) 1080i-CtrlHD")
// rather than the spaced " - " dashPattern expects. It reports ok=false if
// name doesn't actually start with the artist name, or nothing but
// separators is left afterward.
func stripArtistPrefix(name, artist string) (string, bool) {
	if len(name) <= len(artist) || !strings.EqualFold(name[:len(artist)], artist) {
		return "", false
	}
	rest := strings.TrimLeft(name[len(artist):], artistPrefixSeparators)
	if rest == "" {
		return "", false
	}
	return rest, true
}

func cleanTitle(s string) string {
	for {
		stripped := s
		for _, p := range suffixPatterns {
			stripped = p.ReplaceAllString(stripped, "")
		}
		stripped = strings.TrimSpace(stripped)
		if stripped == s {
			break
		}
		s = stripped
	}
	return s
}
