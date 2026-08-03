package match

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/pkg/provider"
)

// Matcher identifies catalogued videos against a metadata provider.
type Matcher struct {
	Catalog             *catalog.Catalog
	Provider            provider.MetadataProvider
	ConfidenceThreshold float64
}

// MatchErrorEntry is a single video that failed to match due to a
// provider error. It is not persisted to the catalog: the video is left
// without a matches row, so it's naturally retried on the next Run.
type MatchErrorEntry struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Result summarizes what a single Run call did.
type Result struct {
	Confirmed   int               `json:"confirmed"`
	NeedsReview int               `json:"needs_review"`
	Unmatched   int               `json:"unmatched"`
	Errors      []MatchErrorEntry `json:"errors"`
}

// Run matches every active video in the catalog that doesn't already have
// a matches row.
func (m *Matcher) Run(ctx context.Context) (Result, error) {
	videos, err := m.Catalog.ListActiveUnmatched()
	if err != nil {
		return Result{}, fmt.Errorf("list active unmatched: %w", err)
	}

	result := Result{}
	now := time.Now().Unix()

	for _, v := range videos {
		base := filepath.Base(v.Path)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		folder := filepath.Dir(v.Path)
		if folder == "." {
			folder = ""
		}

		artist, title, confidence := ParseFilename(name, folder)
		if confidence == 0 {
			if err := m.Catalog.UpsertMatch(&catalog.Match{
				VideoID:   v.ID,
				Provider:  m.Provider.Name(),
				Status:    catalog.MatchStatusUnmatched,
				MatchedAt: now,
			}); err != nil {
				return Result{}, fmt.Errorf("upsert unmatched %s: %w", v.Path, err)
			}
			result.Unmatched++
			continue
		}

		candidates, err := m.Provider.Match(ctx, provider.MatchQuery{Filename: base, Artist: artist, Title: title})
		if err != nil {
			result.Errors = append(result.Errors, MatchErrorEntry{Path: v.Path, Message: err.Error()})
			continue
		}

		if err := m.applyResult(v.ID, artist, title, candidates, now, &result); err != nil {
			return Result{}, fmt.Errorf("apply result %s: %w", v.Path, err)
		}
	}

	return result, nil
}

func (m *Matcher) applyResult(videoID int64, parsedArtist, parsedTitle string, candidates []provider.MatchResult, now int64, result *Result) error {
	entry := &catalog.Match{
		VideoID:      videoID,
		Provider:     m.Provider.Name(),
		ParsedArtist: nullString(parsedArtist),
		ParsedTitle:  nullString(parsedTitle),
		MatchedAt:    now,
	}

	if len(candidates) == 0 {
		entry.Status = catalog.MatchStatusUnmatched
		result.Unmatched++
		return m.Catalog.UpsertMatch(entry)
	}

	// The provider.MetadataProvider interface makes no guarantee that
	// candidates are sorted by confidence, so pick the best explicitly
	// rather than assuming index 0 is it.
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Confidence > best.Confidence {
			best = c
		}
	}
	entry.Confidence = best.Confidence

	if best.Confidence < m.ConfidenceThreshold || !looksLikeRealMatch(parsedArtist, parsedTitle, best) {
		entry.Status = catalog.MatchStatusNeedsReview
		result.NeedsReview++
		return m.Catalog.UpsertMatch(entry)
	}

	artistID, err := m.Catalog.UpsertArtist(best.Artist, best.ArtistSourceID, now)
	if err != nil {
		return fmt.Errorf("upsert artist: %w", err)
	}
	songID, err := m.Catalog.UpsertSong(artistID, best.Title, best.Album, best.Year, best.SourceID, now)
	if err != nil {
		return fmt.Errorf("upsert song: %w", err)
	}

	entry.Status = catalog.MatchStatusConfirmed
	entry.SongID = sql.NullInt64{Int64: songID, Valid: true}
	result.Confirmed++
	return m.Catalog.UpsertMatch(entry)
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// tokenPattern splits on Unicode letter/number runs, so accented names
// ("Mötley Crüe") tokenize as whole words rather than fragmenting on the
// diacritics.
var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)

// junkWords are generic filler terms common in scraped video titles/artist
// strings. Left in, they'd let two otherwise-unrelated matches appear to
// "overlap" just because both happen to contain e.g. "Official Music
// Video" -- exactly the noise looksLikeRealMatch exists to see past.
var junkWords = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "of": true, "with": true, "vs": true,
	"official": true, "video": true, "music": true, "hd": true, "4k": true, "hq": true, "mv": true,
	"remaster": true, "remastered": true, "version": true, "lyric": true, "lyrics": true,
	"feat": true, "ft": true, "featuring": true,
}

// significantTokens lowercases and splits s into words, dropping junk
// filler words and bare numbers -- both inflate coincidental overlap
// between two otherwise-unrelated strings.
func significantTokens(s string) map[string]bool {
	tokens := map[string]bool{}
	for _, w := range tokenPattern.FindAllString(strings.ToLower(s), -1) {
		if junkWords[w] {
			continue
		}
		if _, err := strconv.Atoi(w); err == nil {
			continue
		}
		tokens[w] = true
	}
	return tokens
}

func hasSharedToken(a, b map[string]bool) bool {
	for w := range a {
		if b[w] {
			return true
		}
	}
	return false
}

// looksLikeRealMatch guards against the provider's loosened, unquoted
// query (see musicbrainz.go's search) confidently scoring a completely
// unrelated recording above the confirmation threshold when fed garbled
// input -- e.g. a reversed "Title - Artist" filename that ParseFilename
// has no way to detect. It requires at least one significant word in
// common between the parsed and matched artist, AND separately between
// the parsed and matched title: checking the two fields independently
// stops filler words shared only because both sides happen to contain
// "Official Music Video" junk from papering over a wrong match.
func looksLikeRealMatch(parsedArtist, parsedTitle string, candidate provider.MatchResult) bool {
	return hasSharedToken(significantTokens(parsedArtist), significantTokens(candidate.Artist)) &&
		hasSharedToken(significantTokens(parsedTitle), significantTokens(candidate.Title))
}
