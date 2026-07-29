package rename_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/rename"
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

func setupConfirmedVideo(t *testing.T, cat *catalog.Catalog, root, relPath, artist, title string) int64 {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	videoID, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: 5, MTime: 1, PartialHash: "h",
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	artistID, err := cat.UpsertArtist(artist, "", 1)
	if err != nil {
		t.Fatalf("UpsertArtist: %v", err)
	}
	songID, err := cat.UpsertSong(artistID, title, "", 0, "", 1)
	if err != nil {
		t.Fatalf("UpsertSong: %v", err)
	}
	if err := cat.UpsertMatch(&catalog.Match{
		VideoID: videoID, SongID: sqlNullInt64(songID),
		Provider: "test", Confidence: 1.0, Status: catalog.MatchStatusConfirmed, MatchedAt: 1,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
	return videoID
}

func TestPlanComputesMoveForConfirmedMatch(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Moves) != 1 {
		t.Fatalf("expected 1 planned move, got %d: %+v", len(plan.Moves), plan.Moves)
	}
	if plan.Moves[0].NewPath != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("unexpected new path: %q", plan.Moves[0].NewPath)
	}
	if plan.Unchanged != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanSkipsAlreadyCorrectPath(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Bon Jovi/Wanted Dead or Alive.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Moves) != 0 || plan.Unchanged != 1 {
		t.Fatalf("expected 0 moves and 1 unchanged, got %+v", plan)
	}
}

func TestPlanDetectsCollision(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Song/Song.mp4", "Same Artist", "Same Title")
	setupConfirmedVideo(t, cat, root, "Song/Song.ts", "Same Artist", "Same Title")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Conflicts) != 0 {
		// Different extensions (.mp4 vs .ts) mean different target paths;
		// this asserts the non-collision case explicitly so the real
		// collision test below is unambiguous.
		t.Fatalf("expected no conflict for differing extensions, got %+v", plan.Conflicts)
	}
}

func TestPlanDetectsRealCollision(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "A/Song.mp4", "Same Artist", "Same Title")
	setupConfirmedVideo(t, cat, root, "B/Song.mp4", "Same Artist", "Same Title")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %+v", len(plan.Conflicts), plan.Conflicts)
	}
	if len(plan.Moves) != 0 {
		t.Fatalf("expected 0 moves (both sides of the conflict excluded), got %+v", plan.Moves)
	}
}

// TestPlanDetectsCollisionWithAlreadyCorrectVideo covers a case the naive
// "group only the videos that need to move" algorithm misses: one video is
// already sitting at its correct, final path (so it's "unchanged" on its
// own), but a second video's computed target also lands on that same path.
// Applying the plan as computed would silently overwrite the first video's
// file via os.Rename. Both videos must be treated as being in conflict over
// that path: the second video must not appear in Moves.
func TestPlanDetectsCollisionWithAlreadyCorrectVideo(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Same Artist/Same Title.mp4", "Same Artist", "Same Title")
	setupConfirmedVideo(t, cat, root, "Elsewhere/Other.mp4", "Same Artist", "Same Title")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %+v", len(plan.Conflicts), plan.Conflicts)
	}
	if len(plan.Moves) != 0 {
		t.Fatalf("expected 0 moves (the incoming video must not clobber the resident file), got %+v", plan.Moves)
	}
	if plan.Conflicts[0].NewPath != "Same Artist/Same Title.mp4" {
		t.Fatalf("unexpected conflict target: %+v", plan.Conflicts[0])
	}
}

func TestApplyMovesFileAndUpdatesCatalog(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := r.Apply(root, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.FilesRenamed != 1 {
		t.Fatalf("expected 1 file renamed, got %d", result.FilesRenamed)
	}

	if _, err := os.Stat(filepath.Join(root, "Bon Jovi", "Wanted Dead or Alive.mp4")); err != nil {
		t.Fatalf("expected renamed file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err == nil {
		t.Fatal("expected old file to no longer exist")
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != videoID || active[0].Path != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("expected catalog path updated, got %+v", active)
	}

	runID, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if !ok {
		t.Fatal("expected an applied rename run to exist")
	}
	history, err := cat.ListRenameHistory(runID)
	if err != nil {
		t.Fatalf("ListRenameHistory: %v", err)
	}
	if len(history) != 1 || history[0].OldPath != "Old Artist Name/Song-video.mp4" || history[0].NewPath != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("unexpected history: %+v", history)
	}
}

func TestApplyDryRunTouchesNothing(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}", DryRun: true}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := r.Apply(root, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.FilesRenamed != 0 {
		t.Fatalf("expected 0 files renamed in dry-run, got %d", result.FilesRenamed)
	}

	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err != nil {
		t.Fatalf("expected original file to still exist untouched: %v", err)
	}
	_, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if ok {
		t.Fatal("expected no rename run to be recorded for a dry run")
	}
}

func sqlNullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
