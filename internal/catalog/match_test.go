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

func TestListConfirmedMatches(t *testing.T) {
	cat := openTestCatalog(t)

	confirmedID := insertTestVideo(t, cat, "Artist/Confirmed.mp4")
	reviewID := insertTestVideo(t, cat, "Artist/Review.mp4")

	artistID, err := cat.UpsertArtist("Artist", "artist-mbid", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Title", "Album", 2001, "song-mbid", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}

	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: confirmedID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch confirmed: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: reviewID, Provider: "musicbrainz", Confidence: 0.4, Status: catalog.MatchStatusNeedsReview, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch review: %v", err)
	}

	confirmed, err := cat.ListConfirmedMatches()
	if err != nil {
		t.Fatalf("ListConfirmedMatches: %v", err)
	}
	if len(confirmed) != 1 {
		t.Fatalf("expected 1 confirmed match, got %d: %+v", len(confirmed), confirmed)
	}
	got := confirmed[0]
	if got.VideoID != confirmedID || got.VideoPath != "Artist/Confirmed.mp4" {
		t.Fatalf("unexpected video fields: %+v", got)
	}
	if got.Artist != "Artist" || got.Title != "Title" || got.Album != "Album" || got.Year != 2001 {
		t.Fatalf("unexpected song/artist fields: %+v", got)
	}
}

func TestListConfirmedMatchesHandlesNullAlbumAndYear(t *testing.T) {
	cat := openTestCatalog(t)
	videoID := insertTestVideo(t, cat, "Artist/Song.mp4")

	artistID, err := cat.UpsertArtist("Artist", "", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Title", "", 0, "", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	confirmed, err := cat.ListConfirmedMatches()
	if err != nil {
		t.Fatalf("ListConfirmedMatches: %v", err)
	}
	if len(confirmed) != 1 {
		t.Fatalf("expected 1 confirmed match, got %d", len(confirmed))
	}
	if confirmed[0].Album != "" || confirmed[0].Year != 0 {
		t.Fatalf("expected empty album and zero year, got %+v", confirmed[0])
	}
}

func TestListConfirmedMatchesEmpty(t *testing.T) {
	cat := openTestCatalog(t)

	confirmed, err := cat.ListConfirmedMatches()
	if err != nil {
		t.Fatalf("ListConfirmedMatches: %v", err)
	}
	if len(confirmed) != 0 {
		t.Fatalf("expected 0 confirmed matches, got %d", len(confirmed))
	}
}

func TestListConfirmedArtistVideos(t *testing.T) {
	cat := openTestCatalog(t)

	videoID := insertTestVideo(t, cat, "Bon Jovi/Wanted Dead or Alive.mp4")
	artistID, err := cat.UpsertArtist("Bon Jovi", "artist-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Wanted Dead or Alive", "Slippery When Wet", 1986, "song-mbid-1", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0].VideoPath != "Bon Jovi/Wanted Dead or Alive.mp4" || rows[0].Artist != "Bon Jovi" || rows[0].ArtistMBID != "artist-mbid-1" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func TestListConfirmedArtistVideosHandlesEmptyMBID(t *testing.T) {
	cat := openTestCatalog(t)

	videoID := insertTestVideo(t, cat, "Unknown Artist/Song.mp4")
	artistID, err := cat.UpsertArtist("Unknown Artist", "", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, "Song", "", 0, "", 100)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
		Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 1 || rows[0].ArtistMBID != "" {
		t.Fatalf("expected 1 row with empty ArtistMBID, got %+v", rows)
	}
}

func TestListConfirmedArtistVideosMultipleVideosSameArtist(t *testing.T) {
	cat := openTestCatalog(t)

	artistID, err := cat.UpsertArtist("Metallica", "artist-mbid-2", 100)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	for _, title := range []string{"One", "Enter Sandman"} {
		videoID := insertTestVideo(t, cat, "Metallica/"+title+".mp4")
		songID, err := cat.UpsertSong(artistID, title, "", 0, "song-"+title, 100)
		if err != nil {
			t.Fatalf("UpsertSong: %v", err)
		}
		if err := cat.UpsertMatch(&catalog.Match{
			VideoID: videoID, SongID: sql.NullInt64{Int64: songID, Valid: true},
			Provider: "musicbrainz", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 100,
		}); err != nil {
			t.Fatalf("UpsertMatch: %v", err)
		}
	}

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (one per video, same artist), got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.ArtistMBID != "artist-mbid-2" {
			t.Fatalf("expected consistent artist MBID across both rows, got %+v", r)
		}
	}
}

func TestListConfirmedArtistVideosEmpty(t *testing.T) {
	cat := openTestCatalog(t)

	rows, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		t.Fatalf("ListConfirmedArtistVideos: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}
