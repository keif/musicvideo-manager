package provider

import "context"

// MatchQuery describes what's known about a file when asking a provider
// to identify it.
type MatchQuery struct {
	Filename string
	Artist   string // best-effort guess from filename parsing; may be empty
	Title    string
}

// MatchResult is a single candidate identification returned by a provider.
type MatchResult struct {
	Artist     string
	Title      string
	Album      string
	Year       int
	Confidence float64 // 0.0-1.0
	SourceID   string  // provider-specific ID (MBID, Discogs ID, etc.)
}

// MetadataProvider is implemented by each metadata source (MusicBrainz,
// Discogs, TheAudioDB, Last.fm, ...). No implementations exist yet; this
// interface is the seam future provider sub-projects build against.
type MetadataProvider interface {
	Name() string
	Match(ctx context.Context, q MatchQuery) ([]MatchResult, error)
}
