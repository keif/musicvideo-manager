package match

import (
	"regexp"
	"strings"
)

var dashPattern = regexp.MustCompile(`^(.+?)\s+-\s+(.+)$`)

var suffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*\[[^\]]*\]\s*$`),                            // trailing [video id]
	regexp.MustCompile(`(?i)\s*\(\s*official\s*(music\s*)?video\s*\)\s*$`), // (Official [Music] Video)
	regexp.MustCompile(`(?i)\s*official\s*(music\s*)?video\s*$`),          // bare "Official Music Video"
	regexp.MustCompile(`(?i)\s*-\s*video\s*$`),                            // "-video" suffix
	regexp.MustCompile(`(?i)\s*official\s*hq\s*$`),                        // "Official HQ"
	regexp.MustCompile(`(?i)\s*extended\s*version\s*$`),                   // "Extended Version"
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
		return folder, cleanTitle(name), 0.5
	}

	return "", "", 0
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
