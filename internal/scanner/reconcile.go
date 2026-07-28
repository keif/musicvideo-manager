package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

type HashFunc func(path string) (string, error)
type ProbeFunc func(ctx context.Context, path string) (ProbeResult, error)

// Reconciler compares a library's on-disk state against the catalog and
// applies the difference.
type Reconciler struct {
	Catalog *catalog.Catalog
	Hash    HashFunc
	Probe   ProbeFunc
	Workers int
}

// ScanErrorEntry is a single file that failed to hash or probe during a
// reconcile pass. It does not abort the run.
type ScanErrorEntry struct {
	Path    string
	Message string
}

// Result summarizes what a single Reconcile call did.
type Result struct {
	Added     int
	Updated   int
	Moved     int
	Missing   int
	Unchanged int
	Errors    []ScanErrorEntry
}

type job struct {
	root     string
	relPath  string
	info     FileInfo
	existing *catalog.Video // nil if no active catalog row at this path
}

func (j job) fullPath() string {
	return filepath.Join(j.root, filepath.FromSlash(j.relPath))
}

type probed struct {
	job  job
	hash string
	pr   ProbeResult
	err  error
}

// Reconcile walks root, classifies every discovered file as unchanged,
// updated, moved, or newly added, marks any previously-active catalog rows
// that were not seen as missing, and persists all of it to the catalog.
func (r *Reconciler) Reconcile(ctx context.Context, root string, extensions []string) (Result, error) {
	discovered, err := Discover(root, extensions)
	if err != nil {
		return Result{}, fmt.Errorf("discover: %w", err)
	}

	active, err := r.Catalog.ListActive()
	if err != nil {
		return Result{}, fmt.Errorf("list active: %w", err)
	}
	byPath := make(map[string]*catalog.Video, len(active))
	for _, v := range active {
		byPath[v.Path] = v
	}

	now := time.Now().Unix()
	result := Result{}
	seenPaths := make(map[string]bool, len(discovered))
	var toProcess []job

	for relPath, info := range discovered {
		seenPaths[relPath] = true
		existing, found := byPath[relPath]
		if found && existing.FileSize == info.Size && existing.MTime == info.MTime {
			if err := r.Catalog.TouchSeen(existing.ID, now); err != nil {
				return Result{}, fmt.Errorf("touch seen %s: %w", relPath, err)
			}
			result.Unchanged++
			continue
		}
		toProcess = append(toProcess, job{root: root, relPath: relPath, info: info, existing: existing})
	}

	// Mark unseen active rows missing before processing new/changed files.
	// This must happen first so that move/rename detection (which matches
	// against status = 'missing' rows via FindMissingByHash) can see a
	// same-pass rename's old-path row: the old path is absent from
	// `discovered`, but its catalog row is still 'active' until this loop
	// runs. If this ran after processJobs instead, a rename would be
	// misclassified as an Added + Missing pair rather than a single Moved.
	//
	// newlyMissing tracks the IDs marked here so that, if move detection
	// below consumes one of them as a rename candidate, it can be removed
	// from the missing count instead of being double-counted as both
	// Missing and Moved. Candidates already missing from a prior scan pass
	// (the "resurrected" case) are not in this set and are left alone.
	newlyMissing := make(map[int64]bool, len(byPath))
	for relPath, v := range byPath {
		if !seenPaths[relPath] {
			if err := r.Catalog.MarkMissing(v.ID); err != nil {
				return Result{}, fmt.Errorf("mark missing %s: %w", relPath, err)
			}
			newlyMissing[v.ID] = true
		}
	}

	if err := r.processJobs(ctx, toProcess, now, &result, newlyMissing); err != nil {
		return Result{}, err
	}

	result.Missing = len(newlyMissing)

	return result, nil
}

func (r *Reconciler) processJobs(ctx context.Context, jobs []job, now int64, result *Result, newlyMissing map[int64]bool) error {
	if len(jobs) == 0 {
		return nil
	}

	workers := r.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	jobsCh := make(chan job)
	resultsCh := make(chan probed)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				hash, herr := r.Hash(j.fullPath())
				if herr != nil {
					resultsCh <- probed{job: j, err: herr}
					continue
				}
				pr, perr := r.Probe(ctx, j.fullPath())
				resultsCh <- probed{job: j, hash: hash, pr: pr, err: perr}
			}
		}()
	}

	go func() {
		for _, j := range jobs {
			jobsCh <- j
		}
		close(jobsCh)
	}()
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Drain resultsCh to completion even after a fatal applyResult error:
	// workers and the job-feeder goroutine above send on these channels
	// unbuffered, so returning early here while they're still in flight
	// would leave them permanently blocked on a send nobody is reading —
	// a goroutine leak. Recording the first error and continuing to drain
	// lets every goroutine finish and every channel close normally.
	var firstErr error
	for p := range resultsCh {
		if p.err != nil {
			result.Errors = append(result.Errors, ScanErrorEntry{Path: p.job.relPath, Message: p.err.Error()})
			continue
		}
		if err := r.applyResult(p, now, result, newlyMissing); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Reconciler) applyResult(p probed, now int64, result *Result, newlyMissing map[int64]bool) error {
	if p.job.existing != nil {
		v := &catalog.Video{
			ID: p.job.existing.ID, Path: p.job.relPath,
			FileSize: p.job.info.Size, MTime: p.job.info.MTime, PartialHash: p.hash,
			Status:          catalog.StatusActive,
			DurationSeconds: nullFloat(p.pr.DurationSeconds),
			Width:           nullInt(int64(p.pr.Width)),
			Height:          nullInt(int64(p.pr.Height)),
			VideoCodec:      nullString(p.pr.VideoCodec),
			AudioCodec:      nullString(p.pr.AudioCodec),
			Container:       nullString(p.pr.Container),
			Bitrate:         nullInt(p.pr.Bitrate),
			FPS:             nullFloat(p.pr.FPS),
			LastSeenAt:      now,
			LastScannedAt:   now,
		}
		if err := r.Catalog.UpdateProbe(v); err != nil {
			return fmt.Errorf("update probe %s: %w", p.job.relPath, err)
		}
		result.Updated++
		return nil
	}

	candidates, err := r.Catalog.FindMissingByHash(p.job.info.Size, p.hash)
	if err != nil {
		return fmt.Errorf("find missing by hash %s: %w", p.job.relPath, err)
	}
	if len(candidates) > 0 {
		best := candidates[0]
		bestDiff := abs64(best.MTime - p.job.info.MTime)
		for _, c := range candidates[1:] {
			d := abs64(c.MTime - p.job.info.MTime)
			if d < bestDiff {
				best, bestDiff = c, d
			}
		}
		if err := r.Catalog.UpdatePath(best.ID, p.job.relPath, p.job.info.MTime, now); err != nil {
			return fmt.Errorf("update path %s: %w", p.job.relPath, err)
		}
		// If best.ID was marked missing earlier in this same pass (a
		// same-scan rename), it's no longer missing now that it's been
		// reattached to the new path — don't double-count it as Missing.
		delete(newlyMissing, best.ID)
		result.Moved++
		return nil
	}

	v := &catalog.Video{
		Path: p.job.relPath, FileSize: p.job.info.Size, MTime: p.job.info.MTime, PartialHash: p.hash,
		Status:          catalog.StatusActive,
		DurationSeconds: nullFloat(p.pr.DurationSeconds),
		Width:           nullInt(int64(p.pr.Width)),
		Height:          nullInt(int64(p.pr.Height)),
		VideoCodec:      nullString(p.pr.VideoCodec),
		AudioCodec:      nullString(p.pr.AudioCodec),
		Container:       nullString(p.pr.Container),
		Bitrate:         nullInt(p.pr.Bitrate),
		FPS:             nullFloat(p.pr.FPS),
		FirstSeenAt:     now,
		LastSeenAt:      now,
		LastScannedAt:   now,
	}
	if _, err := r.Catalog.InsertVideo(v); err != nil {
		return fmt.Errorf("insert video %s: %w", p.job.relPath, err)
	}
	result.Added++
	return nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func nullFloat(v float64) sql.NullFloat64 {
	if v == 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}

func nullInt(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
