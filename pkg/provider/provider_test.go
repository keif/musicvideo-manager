package provider_test

import (
	"context"
	"testing"

	"github.com/keif/musicvideo-manager/pkg/provider"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error) {
	return []provider.MatchResult{
		{Artist: q.Artist, Title: q.Title, Confidence: 1.0, SourceID: "fake-1"},
	}, nil
}

func TestFakeProviderSatisfiesInterface(t *testing.T) {
	var p provider.MetadataProvider = fakeProvider{}

	results, err := p.Match(context.Background(), provider.MatchQuery{
		Filename: "Artist - Title.mp4",
		Artist:   "Artist",
		Title:    "Title",
	})
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SourceID != "fake-1" {
		t.Fatalf("expected SourceID %q, got %q", "fake-1", results[0].SourceID)
	}
	if p.Name() != "fake" {
		t.Fatalf("expected Name() %q, got %q", "fake", p.Name())
	}
}
