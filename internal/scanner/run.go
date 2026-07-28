package scanner

import (
	"context"
	"fmt"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// RunSummary is the JSON- and human-printable outcome of a full scan run.
type RunSummary struct {
	FilesScanned int              `json:"files_scanned"`
	FilesAdded   int              `json:"files_added"`
	FilesUpdated int              `json:"files_updated"`
	FilesMoved   int              `json:"files_moved"`
	FilesMissing int              `json:"files_missing"`
	Errors       []ScanErrorEntry `json:"errors"`
}

// Run performs one full scan of root: reconciling discovered files against
// the catalog, and recording the run and any per-file errors in scan_runs /
// scan_errors.
func Run(ctx context.Context, cat *catalog.Catalog, root string, extensions []string, workers int) (RunSummary, error) {
	startedAt := time.Now().Unix()
	runID, err := cat.StartScanRun(startedAt)
	if err != nil {
		return RunSummary{}, fmt.Errorf("start scan run: %w", err)
	}

	rec := &Reconciler{Catalog: cat, Hash: ComputeHash, Probe: Probe, Workers: workers}
	result, err := rec.Reconcile(ctx, root, extensions)
	finishedAt := time.Now().Unix()
	if err != nil {
		return RunSummary{}, fmt.Errorf("reconcile: %w", err)
	}

	for _, e := range result.Errors {
		if err := cat.RecordScanError(runID, e.Path, e.Message, finishedAt); err != nil {
			return RunSummary{}, fmt.Errorf("record scan error: %w", err)
		}
	}

	scanned := result.Added + result.Updated + result.Moved + result.Unchanged + len(result.Errors)
	counts := catalog.ScanRunCounts{
		Scanned: scanned, Added: result.Added, Updated: result.Updated,
		Moved: result.Moved, Missing: result.Missing, Errors: len(result.Errors),
	}
	if err := cat.FinishScanRun(runID, counts, finishedAt); err != nil {
		return RunSummary{}, fmt.Errorf("finish scan run: %w", err)
	}

	return RunSummary{
		FilesScanned: scanned,
		FilesAdded:   result.Added,
		FilesUpdated: result.Updated,
		FilesMoved:   result.Moved,
		FilesMissing: result.Missing,
		Errors:       result.Errors,
	}, nil
}
