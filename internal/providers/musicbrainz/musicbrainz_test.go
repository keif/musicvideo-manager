package musicbrainz_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/musicbrainz"
	"github.com/keif/musicvideo-manager/pkg/provider"
)

// recordRequestTimeTransport records the instant each request is handed
// to the transport -- immediately before it goes out over the network --
// then delegates to http.DefaultTransport. Used to measure rate-limiter
// spacing at the point the code under test actually controls, rather than
// at an httptest server (see TestMatchEnforcesRateLimit).
type recordRequestTimeTransport struct {
	timestamps *[]time.Time
}

func (t recordRequestTimeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*t.timestamps = append(*t.timestamps, time.Now())
	return http.DefaultTransport.RoundTrip(req)
}

func openTestCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return cat
}

const sampleRecordingJSON = `{
  "recordings": [
    {
      "id": "recording-mbid-1",
      "title": "In Da Club",
      "score": 100,
      "artist-credit": [{"name": "50 Cent", "artist": {"id": "artist-mbid-1", "name": "50 Cent"}}],
      "releases": [{"title": "Get Rich or Die Tryin'", "date": "2003-02-06"}]
    }
  ]
}`

func TestMatchCacheMissCallsHTTPThenCachesForNextCall(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleRecordingJSON))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent/1.0")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	results, err := p.Match(context.Background(), provider.MatchQuery{Artist: "50 Cent", Title: "In Da Club"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0]
	if got.Artist != "50 Cent" || got.Title != "In Da Club" {
		t.Fatalf("unexpected artist/title: %+v", got)
	}
	if got.Confidence != 1.0 {
		t.Fatalf("expected confidence 1.0, got %f", got.Confidence)
	}
	if got.SourceID != "recording-mbid-1" || got.ArtistSourceID != "artist-mbid-1" {
		t.Fatalf("unexpected IDs: %+v", got)
	}
	if got.Album != "Get Rich or Die Tryin'" || got.Year != 2003 {
		t.Fatalf("unexpected album/year: %+v", got)
	}

	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", n)
	}

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "50 Cent", Title: "In Da Club"}); err != nil {
		t.Fatalf("second Match: %v", err)
	}
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("expected still 1 HTTP request after cache hit, got %d", n)
	}
}

func TestMatchSendsUserAgentHeader(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "musicvideo-manager/test (https://example.com)")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist", Title: "Title"}); err != nil {
		t.Fatalf("Match: %v", err)
	}
	if gotUserAgent != "musicvideo-manager/test (https://example.com)" {
		t.Fatalf("unexpected User-Agent: %q", gotUserAgent)
	}
}

func TestMatchEnforcesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	// Timestamps are recorded by a RoundTripper, at the instant each
	// request is actually sent, not by the httptest handler after a full
	// network round trip. The rate limiter only guarantees spacing
	// between its own client-side timestamps (musicbrainz.go's
	// waitForRateLimit runs before the request is sent) -- measuring at
	// the server instead is subject to per-request network/connection
	// jitter (e.g. the first request's fresh-connection setup is often
	// slower than the second's warm-connection reuse), which can shrink
	// the observed gap below the guaranteed interval and made this test
	// intermittently fail in CI even though the rate limiter was correct.
	var timestamps []time.Time
	client := &http.Client{Transport: recordRequestTimeTransport{&timestamps}}

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = 50 * time.Millisecond
	p.HTTPClient = client

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist One", Title: "Title One"}); err != nil {
		t.Fatalf("first Match: %v", err)
	}
	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist Two", Title: "Title Two"}); err != nil {
		t.Fatalf("second Match: %v", err)
	}

	if len(timestamps) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(timestamps))
	}
	if elapsed := timestamps[1].Sub(timestamps[0]); elapsed < 50*time.Millisecond {
		t.Fatalf("expected at least 50ms between requests, got %v", elapsed)
	}
}

func TestMatchEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	results, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Nonexistent", Title: "Song"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestMatchNonOKStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist", Title: "Title"}); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestSearchQueryUsesLooseUnquotedTerms(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	// "Don t Speak" is what ParseFilename produces from a source filename
	// whose apostrophe was stripped at download time ("Don't Speak" ->
	// "Don t Speak"). MusicBrainz's index still has the apostrophe, so an
	// exact quoted phrase search never matches it; an unquoted,
	// space-separated term group does (verified live against the API).
	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "No Doubt", Title: "Don t Speak"}); err != nil {
		t.Fatalf("Match: %v", err)
	}

	want := `artist:(No Doubt) AND recording:(Don t Speak)`
	if gotQuery != want {
		t.Fatalf("query: got %q, want %q", gotQuery, want)
	}
}

func TestSearchQueryEscapesLuceneSpecialCharacters(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	p.BaseURL = server.URL
	p.MinInterval = time.Millisecond

	if _, err := p.Match(context.Background(), provider.MatchQuery{Artist: "Artist", Title: "Song (Live)"}); err != nil {
		t.Fatalf("Match: %v", err)
	}

	want := `artist:(Artist) AND recording:(Song \(Live\))`
	if gotQuery != want {
		t.Fatalf("query: got %q, want %q", gotQuery, want)
	}
}

func TestName(t *testing.T) {
	cat := openTestCatalog(t)
	p := musicbrainz.New(cat, "test-agent")
	if p.Name() != "musicbrainz" {
		t.Fatalf("expected Name() %q, got %q", "musicbrainz", p.Name())
	}
}
