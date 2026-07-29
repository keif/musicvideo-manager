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
