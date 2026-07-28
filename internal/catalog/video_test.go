package catalog_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
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

func TestInsertAndListActive(t *testing.T) {
	cat := openTestCatalog(t)

	v := &catalog.Video{
		Path: "Artist/Song.mp4", FileSize: 1000, MTime: 111, PartialHash: "abc",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	}
	id, err := cat.InsertVideo(v)
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active video, got %d", len(active))
	}
	if active[0].Path != "Artist/Song.mp4" {
		t.Fatalf("unexpected path %q", active[0].Path)
	}
	if active[0].DurationSeconds.Valid {
		t.Fatal("expected DurationSeconds to be NULL for a video with no probe data")
	}
}

func TestMarkMissingExcludesFromListActive(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "a.mp4", FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	if err := cat.MarkMissing(id); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected 0 active videos after MarkMissing, got %d", len(active))
	}
}

func TestFindMissingByHashOnlyMatchesMissingRows(t *testing.T) {
	cat := openTestCatalog(t)
	activeID, err := cat.InsertVideo(&catalog.Video{
		Path: "active.mp4", FileSize: 500, MTime: 1, PartialHash: "samehash",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo active: %v", err)
	}
	missingID, err := cat.InsertVideo(&catalog.Video{
		Path: "missing.mp4", FileSize: 500, MTime: 2, PartialHash: "samehash",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo missing: %v", err)
	}
	if err := cat.MarkMissing(missingID); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	candidates, err := cat.FindMissingByHash(500, "samehash")
	if err != nil {
		t.Fatalf("FindMissingByHash: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != missingID {
		t.Fatalf("expected only the missing row (id=%d), got %+v", missingID, candidates)
	}
	_ = activeID
}

func TestUpdatePathReactivatesRow(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "old/Path.mp4", FileSize: 10, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	if err := cat.MarkMissing(id); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	if err := cat.UpdatePath(id, "new/Path.mp4", 99, 100); err != nil {
		t.Fatalf("UpdatePath: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Path != "new/Path.mp4" || active[0].MTime != 99 {
		t.Fatalf("unexpected state after UpdatePath: %+v", active)
	}
}

func TestTouchSeenUpdatesTimestampsAndReactivates(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "a.mp4", FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	if err := cat.TouchSeen(id, 500); err != nil {
		t.Fatalf("TouchSeen: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].LastSeenAt != 500 || active[0].LastScannedAt != 500 {
		t.Fatalf("unexpected state after TouchSeen: %+v", active)
	}
}

func TestUpdateProbeSetsMetadataFields(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.InsertVideo(&catalog.Video{
		Path: "a.mp4", FileSize: 1, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}

	v := &catalog.Video{
		ID: id, FileSize: 2000, MTime: 222, PartialHash: "newhash", Status: catalog.StatusActive,
		DurationSeconds: sqlNullFloat(120.5), Width: sqlNullInt(1920), Height: sqlNullInt(1080),
		VideoCodec: sqlNullString("h264"), AudioCodec: sqlNullString("aac"), Container: sqlNullString("mov,mp4,m4a"),
		Bitrate: sqlNullInt(5000000), FPS: sqlNullFloat(29.97), LastSeenAt: 300, LastScannedAt: 300,
	}
	if err := cat.UpdateProbe(v); err != nil {
		t.Fatalf("UpdateProbe: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active video, got %d", len(active))
	}
	got := active[0]
	if !got.DurationSeconds.Valid || got.DurationSeconds.Float64 != 120.5 {
		t.Fatalf("unexpected DurationSeconds: %+v", got.DurationSeconds)
	}
	if !got.VideoCodec.Valid || got.VideoCodec.String != "h264" {
		t.Fatalf("unexpected VideoCodec: %+v", got.VideoCodec)
	}
}

func sqlNullFloat(v float64) sql.NullFloat64 { return sql.NullFloat64{Float64: v, Valid: true} }
func sqlNullInt(v int64) sql.NullInt64       { return sql.NullInt64{Int64: v, Valid: true} }
func sqlNullString(v string) sql.NullString  { return sql.NullString{String: v, Valid: true} }
