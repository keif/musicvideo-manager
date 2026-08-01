package musicbrainz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/pkg/provider"
)

const (
	defaultBaseURL     = "https://musicbrainz.org/ws/2"
	defaultMinInterval = time.Second
)

// MusicBrainzProvider implements pkg/provider.MetadataProvider against
// MusicBrainz's recording search API, respecting its usage policy: at
// most one request per second, and a descriptive User-Agent header.
// Every response is cached via Catalog so a repeated query never hits
// the network twice.
type MusicBrainzProvider struct {
	Catalog     *catalog.Catalog
	HTTPClient  *http.Client
	UserAgent   string
	BaseURL     string
	MinInterval time.Duration

	mu          sync.Mutex
	lastRequest time.Time
}

// New returns a MusicBrainzProvider configured for the real MusicBrainz API.
func New(cat *catalog.Catalog, userAgent string) *MusicBrainzProvider {
	return &MusicBrainzProvider{
		Catalog:     cat,
		HTTPClient:  http.DefaultClient,
		UserAgent:   userAgent,
		BaseURL:     defaultBaseURL,
		MinInterval: defaultMinInterval,
	}
}

func (p *MusicBrainzProvider) Name() string { return "musicbrainz" }

func (p *MusicBrainzProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error) {
	cacheKey := computeCacheKey(q.Artist, q.Title)

	if cached, ok, err := p.Catalog.GetCachedResponse(p.Name(), cacheKey); err != nil {
		return nil, fmt.Errorf("check cache: %w", err)
	} else if ok {
		return parseRecordingSearchResponse([]byte(cached))
	}

	p.waitForRateLimit()

	body, err := p.search(ctx, q.Artist, q.Title)
	if err != nil {
		return nil, fmt.Errorf("musicbrainz search: %w", err)
	}

	if err := p.Catalog.SetCachedResponse(p.Name(), cacheKey, string(body), time.Now().Unix()); err != nil {
		return nil, fmt.Errorf("cache response: %w", err)
	}

	return parseRecordingSearchResponse(body)
}

func (p *MusicBrainzProvider) waitForRateLimit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.lastRequest.IsZero() {
		if elapsed := time.Since(p.lastRequest); elapsed < p.MinInterval {
			time.Sleep(p.MinInterval - elapsed)
		}
	}
	p.lastRequest = time.Now()
}

func (p *MusicBrainzProvider) search(ctx context.Context, artist, title string) ([]byte, error) {
	// Parenthesized, unquoted term groups rather than exact quoted
	// phrases: source filenames routinely lose punctuation MusicBrainz's
	// index still has (apostrophes stripped by download tools, "&"
	// dropped from artist names, ...), and an exact phrase match on
	// "Don t Speak" never finds the indexed "Don't Speak". A loose term
	// query does, and still ranks by the same score/confidence-threshold
	// gate the caller already applies, so a weaker match just scores
	// lower rather than false-confirming.
	query := fmt.Sprintf(`artist:(%s) AND recording:(%s)`, luceneEscape(artist), luceneEscape(title))

	u := p.BaseURL + "/recording?query=" + url.QueryEscape(query) + "&fmt=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", p.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// luceneSpecial matches characters with special meaning in Lucene query
// syntax (MusicBrainz search) that must be escaped so they're treated as
// literal text rather than query operators when embedded, unquoted,
// inside a parenthesized term group.
var luceneSpecial = regexp.MustCompile(`([+\-&|!(){}\[\]^"~*?:\\/])`)

func luceneEscape(s string) string {
	return luceneSpecial.ReplaceAllString(s, `\$1`)
}

func computeCacheKey(artist, title string) string {
	normalized := strings.ToLower(strings.TrimSpace(artist)) + "\x00" + strings.ToLower(strings.TrimSpace(title))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

type recordingSearchResponse struct {
	Recordings []recording `json:"recordings"`
}

type recording struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Score        int            `json:"score"`
	ArtistCredit []artistCredit `json:"artist-credit"`
	Releases     []release      `json:"releases"`
}

type artistCredit struct {
	Name   string       `json:"name"`
	Artist artistDetail `json:"artist"`
}

type artistDetail struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type release struct {
	Title string `json:"title"`
	Date  string `json:"date"`
}

func parseRecordingSearchResponse(body []byte) ([]provider.MatchResult, error) {
	var parsed recordingSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse musicbrainz response: %w", err)
	}

	results := make([]provider.MatchResult, 0, len(parsed.Recordings))
	for _, rec := range parsed.Recordings {
		artistName, artistSourceID := "", ""
		if len(rec.ArtistCredit) > 0 {
			artistName = rec.ArtistCredit[0].Name
			artistSourceID = rec.ArtistCredit[0].Artist.ID
		}
		album, year := "", 0
		if len(rec.Releases) > 0 {
			album = rec.Releases[0].Title
			year = parseYear(rec.Releases[0].Date)
		}
		results = append(results, provider.MatchResult{
			Artist:         artistName,
			Title:          rec.Title,
			Album:          album,
			Year:           year,
			Confidence:     float64(rec.Score) / 100.0,
			SourceID:       rec.ID,
			ArtistSourceID: artistSourceID,
		})
	}
	return results, nil
}

func parseYear(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y, err := strconv.Atoi(dateStr[:4])
	if err != nil {
		return 0
	}
	return y
}
