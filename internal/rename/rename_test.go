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

// TestApplyBlocksMoveOntoUntrackedFile proves that Apply refuses to
// os.Rename a confirmed match's file on top of a file at its computed
// target path that Plan() had no visibility into (e.g. one of the
// unmatched/needs-review videos, or any other untracked file already
// physically present in the library). The move must appear in
// Result.Blocked, the confirmed match's own file must be left in place at
// its original path, and the untracked file's content must be unchanged.
func TestApplyBlocksMoveOntoUntrackedFile(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	// An untracked file already occupies the confirmed match's computed
	// target path. Plan() has no way to know about this since it only
	// reasons about other confirmed matches.
	targetDir := filepath.Join(root, "Bon Jovi")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	targetPath := filepath.Join(targetDir, "Wanted Dead or Alive.mp4")
	untrackedContent := []byte("untracked file that must survive")
	if err := os.WriteFile(targetPath, untrackedContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Moves) != 1 {
		t.Fatalf("expected 1 planned move (Plan can't see the untracked file), got %+v", plan.Moves)
	}

	result, err := r.Apply(root, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.FilesRenamed != 0 {
		t.Fatalf("expected 0 files renamed, got %d", result.FilesRenamed)
	}
	if len(result.Blocked) != 1 || result.Blocked[0].VideoID != videoID {
		t.Fatalf("expected the move to appear in Blocked, got %+v", result.Blocked)
	}

	// The confirmed match's own file must still be at its original path.
	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err != nil {
		t.Fatalf("expected confirmed match's file left at original path: %v", err)
	}

	// The untracked file at the target path must be untouched.
	gotContent, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(gotContent) != string(untrackedContent) {
		t.Fatalf("expected untracked file content unchanged, got %q", gotContent)
	}

	// The catalog must not have been updated for the blocked move.
	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Path != "Old Artist Name/Song-video.mp4" {
		t.Fatalf("expected catalog path unchanged for blocked move, got %+v", active)
	}
}

// TestApplyContinuesBatchAfterBlockedMove proves that one move being
// blocked by an untracked file at its target path does not abort the rest
// of the batch -- matching the codebase's established error-tolerance
// pattern of recording per-item failures and continuing.
func TestApplyContinuesBatchAfterBlockedMove(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	blockedID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")
	okID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Other-video.mp4", "Moby", "Porcelain")

	targetDir := filepath.Join(root, "Bon Jovi")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Wanted Dead or Alive.mp4"), []byte("untracked"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Moves) != 2 {
		t.Fatalf("expected 2 planned moves, got %+v", plan.Moves)
	}

	result, err := r.Apply(root, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.FilesRenamed != 1 {
		t.Fatalf("expected 1 file renamed despite the blocked move, got %d", result.FilesRenamed)
	}
	if len(result.Blocked) != 1 || result.Blocked[0].VideoID != blockedID {
		t.Fatalf("expected 1 blocked move for the untracked-file collision, got %+v", result.Blocked)
	}

	if _, err := os.Stat(filepath.Join(root, "Moby", "Porcelain.mp4")); err != nil {
		t.Fatalf("expected the unblocked move to complete: %v", err)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, v := range active {
		if v.ID == okID && v.Path != "Moby/Porcelain.mp4" {
			t.Fatalf("expected catalog updated for the successful move, got %+v", v)
		}
		if v.ID == blockedID && v.Path != "Old Artist Name/Song-video.mp4" {
			t.Fatalf("expected catalog unchanged for the blocked move, got %+v", v)
		}
	}
}

func sqlNullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
