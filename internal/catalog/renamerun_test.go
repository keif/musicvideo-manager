// internal/catalog/renamerun_test.go
package catalog_test

import "testing"

func TestRenameRunLifecycle(t *testing.T) {
	cat := openTestCatalog(t)

	id, err := cat.StartRenameRun(100)
	if err != nil {
		t.Fatalf("StartRenameRun: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	if err := cat.RecordRenameHistory(id, 1, "old/path.mp4", "new/path.mp4"); err != nil {
		t.Fatalf("RecordRenameHistory: %v", err)
	}
	if err := cat.RecordRenameHistory(id, 2, "old/other.mp4", "new/other.mp4"); err != nil {
		t.Fatalf("RecordRenameHistory: %v", err)
	}

	if err := cat.FinishRenameRun(id, 2, 200); err != nil {
		t.Fatalf("FinishRenameRun: %v", err)
	}

	gotID, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if !ok || gotID != id {
		t.Fatalf("expected last applied run %d, got %d (ok=%v)", id, gotID, ok)
	}

	history, err := cat.ListRenameHistory(id)
	if err != nil {
		t.Fatalf("ListRenameHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].OldPath != "old/path.mp4" || history[0].NewPath != "new/path.mp4" {
		t.Fatalf("unexpected first entry: %+v", history[0])
	}

	if err := cat.MarkRenameRunUndone(id); err != nil {
		t.Fatalf("MarkRenameRunUndone: %v", err)
	}

	_, ok, err = cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID after undo: %v", err)
	}
	if ok {
		t.Fatal("expected no applied run after marking undone")
	}
}

func TestGetLastAppliedRenameRunIDNoneExists(t *testing.T) {
	cat := openTestCatalog(t)

	_, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		t.Fatalf("GetLastAppliedRenameRunID: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when no rename run exists")
	}
}

func TestListRenameHistoryEmpty(t *testing.T) {
	cat := openTestCatalog(t)
	id, err := cat.StartRenameRun(100)
	if err != nil {
		t.Fatalf("StartRenameRun: %v", err)
	}

	history, err := cat.ListRenameHistory(id)
	if err != nil {
		t.Fatalf("ListRenameHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 history entries, got %d", len(history))
	}
}
