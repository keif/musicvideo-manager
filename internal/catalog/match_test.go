package catalog_test

import (
	"database/sql"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func insertTestVideo(t *testing.T, cat *catalog.Catalog, path string) int64 {
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

func TestUpsertMatchInsertsAndUpdates(t *testing.T) {
	cat := openTestCatalog(t)
	videoID := insertTestVideo(t, cat, "Artist/Song.mp4")

	err := cat.UpsertMatch(&catalog.Match{
		VideoID:      videoID,
		Provider:     "musicbrainz",
		Confidence:   0.9,
		Status:       catalog.MatchStatusConfirmed,
		SongID:       sql.NullInt64{Int64: 1, Valid: true},
		ParsedArtist: sql.NullString{String: "Artist", Valid: true},
		ParsedTitle:  sql.NullString{String: "Song", Valid: true},
		MatchedAt:    100,
	})
	if err != nil {
		t.Fatalf("first UpsertMatch: %v", err)
	}

	unmatched, err := cat.ListActiveUnmatched()
	if err != nil {
		t.Fatalf("ListActiveUnmatched: %v", err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("expected 0 unmatched videos after matching, got %d", len(unmatched))
	}

	// Re-running match for the same video updates the existing row rather
	// than creating a second one (video_id is UNIQUE).
	err = cat.UpsertMatch(&catalog.Match{
		VideoID:    videoID,
		Provider:   "musicbrainz",
		Confidence: 0.5,
		Status:     catalog.MatchStatusNeedsReview,
		MatchedAt:  200,
	})
	if err != nil {
		t.Fatalf("second UpsertMatch: %v", err)
	}

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 needs-review item, got %d", len(items))
	}
	if items[0].VideoPath != "Artist/Song.mp4" {
		t.Fatalf("unexpected video path: %q", items[0].VideoPath)
	}
}

func TestListActiveUnmatchedOnlyReturnsVideosWithoutAMatch(t *testing.T) {
	cat := openTestCatalog(t)
	matchedID := insertTestVideo(t, cat, "Artist/Matched.mp4")
	unmatchedID := insertTestVideo(t, cat, "Artist/Unmatched.mp4")

	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: matchedID, Provider: "musicbrainz", Confidence: 1, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	unmatched, err := cat.ListActiveUnmatched()
	if err != nil {
		t.Fatalf("ListActiveUnmatched: %v", err)
	}
	if len(unmatched) != 1 || unmatched[0].ID != unmatchedID {
		t.Fatalf("expected only the unmatched video, got %+v", unmatched)
	}
}

func TestListNeedsReviewExcludesConfirmedAndUnmatched(t *testing.T) {
	cat := openTestCatalog(t)
	confirmedID := insertTestVideo(t, cat, "Artist/Confirmed.mp4")
	reviewID := insertTestVideo(t, cat, "Artist/Review.mp4")
	unmatchedID := insertTestVideo(t, cat, "Artist/Unmatched.mp4")

	mustUpsert := func(videoID int64, status catalog.MatchStatus) {
		if err := cat.UpsertMatch(&catalog.Match{
			VideoID: videoID, Provider: "musicbrainz", Confidence: 0.5, Status: status, MatchedAt: 100,
		}); err != nil {
			t.Fatalf("UpsertMatch: %v", err)
		}
	}
	mustUpsert(confirmedID, catalog.MatchStatusConfirmed)
	mustUpsert(reviewID, catalog.MatchStatusNeedsReview)
	mustUpsert(unmatchedID, catalog.MatchStatusUnmatched)

	items, err := cat.ListNeedsReview()
	if err != nil {
		t.Fatalf("ListNeedsReview: %v", err)
	}
	if len(items) != 1 || items[0].VideoPath != "Artist/Review.mp4" {
		t.Fatalf("expected only the review video, got %+v", items)
	}
}
