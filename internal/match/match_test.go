package match_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/match"
	"github.com/keif/musicvideo-manager/pkg/provider"
)

func openTestCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return cat
}

func insertActiveVideo(t *testing.T, cat *catalog.Catalog, path string) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: path, FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

type fakeProvider struct {
	name      string
	results   map[string][]provider.MatchResult // keyed by "Artist|Title"
	err       error
	callCount int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error) {
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return f.results[q.Artist+"|"+q.Title], nil
}

func TestRunConfirmsHighConfidenceMatch(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "50 Cent/50 Cent - In Da Club-video.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{
		"50 Cent|In Da Club": {{Artist: "50 Cent", Title: "In Da Club", Confidence: 0.95, SourceID: "song-1", ArtistSourceID: "artist-1"}},
	}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Confirmed != 1 || result.NeedsReview != 0 || result.Unmatched != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no review items, got %+v", items)
	}
}

func TestRunFlagsLowConfidenceAsNeedsReview(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "50 Cent/50 Cent - In Da Club-video.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{
		"50 Cent|In Da Club": {{Artist: "50 Cent", Title: "In Da Club", Confidence: 0.4, SourceID: "song-1"}},
	}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.NeedsReview != 1 || result.Confirmed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 1 || items[0].Confidence != 0.4 {
		t.Fatalf("unexpected review items: %+v", items)
	}
}

func TestRunMarksUnmatchedWhenProviderReturnsNoCandidates(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "50 Cent/50 Cent - In Da Club-video.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Unmatched != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunSkipsProviderForUnparseableFilename(t *testing.T) {
	cat := openTestCatalog(t)
	// No " - " delimiter and inserted at the library root (no parent
	// folder), so ParseFilename returns confidence 0.
	insertActiveVideo(t, cat, "NoDelimiterHere.mp4")

	fp := &fakeProvider{name: "fake", results: map[string][]provider.MatchResult{}}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Unmatched != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fp.callCount != 0 {
		t.Fatalf("expected provider not to be called for an unparseable filename, got %d calls", fp.callCount)
	}
}

func TestRunRecordsProviderErrorWithoutAbortingBatch(t *testing.T) {
	cat := openTestCatalog(t)
	insertActiveVideo(t, cat, "Bad Artist/Bad Artist - Bad Song-video.mp4")
	insertActiveVideo(t, cat, "Good Artist/Good Artist - Good Song-video.mp4")

	fp := &fakeProvider{
		name: "fake",
		err:  errors.New("simulated provider failure"),
	}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run should not fail the whole batch on a provider error: %v", err)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 recorded errors, got %+v", result.Errors)
	}

	unmatched, err := cat.ListActiveUnmatched()
	if err != nil {
		t.Fatalf("ListActiveUnmatched: %v", err)
	}
	if len(unmatched) != 2 {
		t.Fatalf("expected both videos to remain unmatched (retryable), got %d", len(unmatched))
	}
}

func TestRunEmptyLibraryReturnsZeroResult(t *testing.T) {
	cat := openTestCatalog(t)
	fp := &fakeProvider{name: "fake"}
	m := &match.Matcher{Catalog: cat, Provider: fp, ConfidenceThreshold: 0.7}

	result, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Confirmed != 0 || result.NeedsReview != 0 || result.Unmatched != 0 || len(result.Errors) != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
