package catalog

import "database/sql"

// RenameRunStatus is the lifecycle state of an applied rename batch.
type RenameRunStatus string

const (
	RenameRunStatusApplied RenameRunStatus = "applied"
	RenameRunStatusUndone  RenameRunStatus = "undone"
)

// RenameHistoryEntry is a single file move within a rename run.
type RenameHistoryEntry struct {
	ID          int64
	RenameRunID int64
	VideoID     int64
	OldPath     string
	NewPath     string
}

// StartRenameRun records the start of a rename apply and returns its ID.
func (c *Catalog) StartRenameRun(startedAt int64) (int64, error) {
	res, err := c.db.Exec(`INSERT INTO rename_runs (started_at) VALUES (?)`, startedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRenameRun records the file count and completion time for a rename run.
func (c *Catalog) FinishRenameRun(id int64, filesRenamed int, finishedAt int64) error {
	_, err := c.db.Exec(`UPDATE rename_runs SET finished_at=?, files_renamed=? WHERE id=?`, finishedAt, filesRenamed, id)
	return err
}

// RecordRenameHistory logs a single file move within a rename run.
func (c *Catalog) RecordRenameHistory(renameRunID, videoID int64, oldPath, newPath string) error {
	_, err := c.db.Exec(`INSERT INTO rename_history (rename_run_id, video_id, old_path, new_path) VALUES (?, ?, ?, ?)`,
		renameRunID, videoID, oldPath, newPath)
	return err
}

// GetLastAppliedRenameRunID returns the ID of the most recent rename run
// with status = 'applied', or ok=false if none exists.
func (c *Catalog) GetLastAppliedRenameRunID() (int64, bool, error) {
	var id int64
	err := c.db.QueryRow(`SELECT id FROM rename_runs WHERE status = 'applied' ORDER BY id DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// ListRenameHistory returns every history entry for a rename run, in the
// order they were recorded.
func (c *Catalog) ListRenameHistory(renameRunID int64) ([]RenameHistoryEntry, error) {
	rows, err := c.db.Query(`SELECT id, rename_run_id, video_id, old_path, new_path FROM rename_history WHERE rename_run_id = ? ORDER BY id`, renameRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RenameHistoryEntry{}
	for rows.Next() {
		var e RenameHistoryEntry
		if err := rows.Scan(&e.ID, &e.RenameRunID, &e.VideoID, &e.OldPath, &e.NewPath); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkRenameRunUndone flips a rename run's status to 'undone' so it is
// never targeted by a future undo.
func (c *Catalog) MarkRenameRunUndone(id int64) error {
	_, err := c.db.Exec(`UPDATE rename_runs SET status = 'undone' WHERE id = ?`, id)
	return err
}
