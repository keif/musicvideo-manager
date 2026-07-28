package catalog

// ScanRunCounts summarizes the outcome of a single scan run.
type ScanRunCounts struct {
	Scanned int
	Added   int
	Updated int
	Moved   int
	Missing int
	Errors  int
}

// StartScanRun records the start of a scan and returns its ID.
func (c *Catalog) StartScanRun(startedAt int64) (int64, error) {
	res, err := c.db.Exec(`INSERT INTO scan_runs (started_at) VALUES (?)`, startedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScanRun records the outcome counts and completion time for a scan run.
func (c *Catalog) FinishScanRun(id int64, counts ScanRunCounts, finishedAt int64) error {
	_, err := c.db.Exec(`UPDATE scan_runs SET finished_at=?, files_scanned=?, files_added=?,
		files_updated=?, files_moved=?, files_missing=?, errors_count=? WHERE id=?`,
		finishedAt, counts.Scanned, counts.Added, counts.Updated, counts.Moved, counts.Missing, counts.Errors, id)
	return err
}

// RecordScanError logs a single file-level failure encountered during a scan run.
func (c *Catalog) RecordScanError(scanRunID int64, path, message string, occurredAt int64) error {
	_, err := c.db.Exec(`INSERT INTO scan_errors (scan_run_id, path, message, occurred_at) VALUES (?, ?, ?, ?)`,
		scanRunID, path, message, occurredAt)
	return err
}
