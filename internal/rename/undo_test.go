package rename_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/rename"
)

func TestUndoReversesLastAppliedRun(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := r.Apply(root, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	result, err := rename.Undo(cat, root)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.FilesRestored != 1 {
		t.Fatalf("expected 1 file restored, got %d", result.FilesRestored)
	}

	if _, err := os.Stat(filepath.Join(root, "Old Artist Name", "Song-video.mp4")); err != nil {
		t.Fatalf("expected original file restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Bon Jovi", "Wanted Dead or Alive.mp4")); err == nil {
		t.Fatal("expected renamed file to no longer exist after undo")
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != videoID || active[0].Path != "Old Artist Name/Song-video.mp4" {
		t.Fatalf("expected catalog path restored, got %+v", active)
	}
}

func TestUndoTwiceErrorsOnSecondCall(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := r.Apply(root, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := rename.Undo(cat, root); err != nil {
		t.Fatalf("first Undo: %v", err)
	}
	if _, err := rename.Undo(cat, root); err == nil {
		t.Fatal("expected an error undoing when no applied run remains")
	}
}

func TestUndoNoAppliedRunErrors(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)

	if _, err := rename.Undo(cat, root); err == nil {
		t.Fatal("expected an error when there is nothing to undo")
	}
}

// TestUndoSkipsRestoreWhenOriginalPathOccupied proves that Undo does not
// overwrite a file that has since appeared at a rename's original path
// (e.g. an unrelated file dropped into the library after the rename ran).
// The restore for that entry must be skipped and counted in FilesSkipped,
// the renamed file must remain at its post-rename path, and the run must
// still be marked undone rather than left in a failed/incomplete state.
func TestUndoSkipsRestoreWhenOriginalPathOccupied(t *testing.T) {
	root := t.TempDir()
	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Old Artist Name/Song-video.mp4", "Bon Jovi", "Wanted Dead or Alive")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := r.Apply(root, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Something else now occupies the original path.
	origDir := filepath.Join(root, "Old Artist Name")
	if err := os.MkdirAll(origDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	origPath := filepath.Join(origDir, "Song-video.mp4")
	occupantContent := []byte("unrelated file now living here")
	if err := os.WriteFile(origPath, occupantContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := rename.Undo(cat, root)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.FilesRestored != 0 {
		t.Fatalf("expected 0 files restored, got %d", result.FilesRestored)
	}
	if result.FilesSkipped != 1 {
		t.Fatalf("expected 1 file skipped, got %d", result.FilesSkipped)
	}

	// The occupant at the original path must be untouched.
	gotContent, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(gotContent) != string(occupantContent) {
		t.Fatalf("expected occupant file content unchanged, got %q", gotContent)
	}

	// The renamed file must remain at its post-rename path since the
	// restore was skipped.
	if _, err := os.Stat(filepath.Join(root, "Bon Jovi", "Wanted Dead or Alive.mp4")); err != nil {
		t.Fatalf("expected renamed file to remain in place after skipped restore: %v", err)
	}

	// The catalog path must remain the post-rename path for the skipped entry.
	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != videoID || active[0].Path != "Bon Jovi/Wanted Dead or Alive.mp4" {
		t.Fatalf("expected catalog path unchanged for skipped restore, got %+v", active)
	}

	// The run must still be marked undone -- a partial undo is a
	// completed undo attempt, not a failure of the whole operation.
	if _, err := rename.Undo(cat, root); err == nil {
		t.Fatal("expected an error undoing again since the run was already marked undone")
	}
}

// TestUndoRestoresCaseOnlyRenameOnCaseInsensitiveFilesystem proves that
// undoing a case-only rename (e.g. "Artist/song.mp4" -> "Artist/Song.mp4")
// actually restores the original casing rather than being falsely skipped.
// The existence guard added to TestUndoSkipsRestoreWhenOriginalPathOccupied
// has the same case-insensitive-filesystem blind spot as Apply's guard: on
// a case-insensitive filesystem, os.Lstat(oldFull) resolves to the very
// file being restored (just spelled with the post-rename case), not to some
// other file that has since occupied the original path. See
// TestApplyPerformsCaseOnlyRenameOnCaseInsensitiveFilesystem for the
// filesystem-portability rationale reused here.
func TestUndoRestoresCaseOnlyRenameOnCaseInsensitiveFilesystem(t *testing.T) {
	root := t.TempDir()

	probePath := filepath.Join(root, "case-probe.tmp")
	if err := os.WriteFile(probePath, []byte("probe"), 0o644); err != nil {
		t.Fatalf("WriteFile probe: %v", err)
	}
	_, statErr := os.Stat(filepath.Join(root, "CASE-PROBE.tmp"))
	if err := os.Remove(probePath); err != nil {
		t.Fatalf("Remove probe: %v", err)
	}
	if statErr != nil {
		t.Skipf("filesystem at %s is case-sensitive (probe file under a different case was not found: %v); "+
			"a case-only rename cannot collide at the OS level here, so this test cannot exercise the fix", root, statErr)
	}

	cat := openTestCatalog(t)
	videoID := setupConfirmedVideo(t, cat, root, "Artist/song.mp4", "Artist", "Song")

	r := &rename.Renamer{Catalog: cat, Template: "{artist}/{title}"}
	plan, err := r.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := r.Apply(root, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	result, err := rename.Undo(cat, root)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.FilesSkipped != 0 {
		t.Fatalf("expected the case-only restore NOT to be skipped, got %+v", result)
	}
	if result.FilesRestored != 1 {
		t.Fatalf("expected 1 file restored, got %d", result.FilesRestored)
	}

	entries, err := os.ReadDir(filepath.Join(root, "Artist"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "song.mp4" {
		t.Fatalf("expected exactly one entry named song.mp4, got %+v", entries)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != videoID || active[0].Path != "Artist/song.mp4" {
		t.Fatalf("expected catalog path restored to the original case, got %+v", active)
	}
}
