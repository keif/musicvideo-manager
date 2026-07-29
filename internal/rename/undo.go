package rename

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// UndoResult is the outcome of reversing a rename run.
type UndoResult struct {
	FilesRestored int `json:"files_restored"`
}

// Undo reverses the most recently applied rename run: every file moved by
// that run is moved back to its original path, the catalog is updated to
// match, and the run is marked 'undone' so it can't be undone again.
func Undo(cat *catalog.Catalog, root string) (UndoResult, error) {
	runID, ok, err := cat.GetLastAppliedRenameRunID()
	if err != nil {
		return UndoResult{}, fmt.Errorf("get last applied rename run: %w", err)
	}
	if !ok {
		return UndoResult{}, fmt.Errorf("no applied rename run to undo")
	}

	history, err := cat.ListRenameHistory(runID)
	if err != nil {
		return UndoResult{}, fmt.Errorf("list rename history: %w", err)
	}

	restored := 0
	for _, entry := range history {
		oldFull := filepath.Join(root, filepath.FromSlash(entry.OldPath))
		newFull := filepath.Join(root, filepath.FromSlash(entry.NewPath))

		if err := os.MkdirAll(filepath.Dir(oldFull), 0o755); err != nil {
			return UndoResult{}, fmt.Errorf("create directory for %s: %w", entry.OldPath, err)
		}
		if err := os.Rename(newFull, oldFull); err != nil {
			return UndoResult{}, fmt.Errorf("restore %s from %s: %w", entry.OldPath, entry.NewPath, err)
		}
		if err := cat.UpdateVideoPath(entry.VideoID, entry.OldPath); err != nil {
			return UndoResult{}, fmt.Errorf("update catalog path for video %d: %w", entry.VideoID, err)
		}
		restored++
	}

	if err := cat.MarkRenameRunUndone(runID); err != nil {
		return UndoResult{}, fmt.Errorf("mark rename run undone: %w", err)
	}

	return UndoResult{FilesRestored: restored}, nil
}
