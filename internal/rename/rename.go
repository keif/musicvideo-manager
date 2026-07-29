package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// Renamer computes and applies renames for confirmed matches.
type Renamer struct {
	Catalog  *catalog.Catalog
	Template string
	DryRun   bool
}

// PlannedMove is one video's computed rename.
type PlannedMove struct {
	VideoID int64  `json:"video_id"`
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// Conflict is a target path claimed by more than one video.
type Conflict struct {
	NewPath  string   `json:"new_path"`
	OldPaths []string `json:"old_paths"`
}

// Plan is the full outcome of computing renames for every confirmed match,
// before any filesystem write.
type Plan struct {
	Moves     []PlannedMove `json:"moves"`
	Unchanged int           `json:"unchanged"`
	Conflicts []Conflict    `json:"conflicts"`
}

// Result is the outcome of actually applying a Plan.
type Result struct {
	FilesRenamed int           `json:"files_renamed"`
	Conflicts    []Conflict    `json:"conflicts"`
	Blocked      []PlannedMove `json:"blocked"`
}

// occupant is one confirmed match's claim on a target path, tracked
// alongside whether it actually needs to move to get there.
type occupant struct {
	videoID int64
	oldPath string
	newPath string
	moving  bool
}

// Plan computes every confirmed match's target path and detects
// collisions, without touching the filesystem.
//
// Collision detection groups every confirmed match by its computed target
// path -- including videos that are already sitting at their correct,
// final path (moving == false). This matters: if video A already lives at
// "Artist/Title.mp4" (so it's individually "unchanged") and video B's
// computed target also lands on "Artist/Title.mp4", applying the plan
// naively would os.Rename video B on top of video A's file and silently
// destroy it. Grouping by target path first, then checking whether more
// than one video occupies each group -- regardless of whether every
// occupant needs to move -- catches that case, not just the case where two
// videos both need to move to the same place.
func (r *Renamer) Plan() (Plan, error) {
	matches, err := r.Catalog.ListConfirmedMatches()
	if err != nil {
		return Plan{}, fmt.Errorf("list confirmed matches: %w", err)
	}

	byTarget := map[string][]occupant{}
	plan := Plan{Moves: []PlannedMove{}, Conflicts: []Conflict{}}

	for _, m := range matches {
		relStem, err := RenderTemplate(r.Template, TemplateFields{Artist: m.Artist, Title: m.Title, Album: m.Album, Year: m.Year})
		if err != nil {
			return Plan{}, fmt.Errorf("render template for video %d: %w", m.VideoID, err)
		}
		ext := filepath.Ext(m.VideoPath)
		newPath := relStem + ext

		byTarget[newPath] = append(byTarget[newPath], occupant{
			videoID: m.VideoID,
			oldPath: m.VideoPath,
			newPath: newPath,
			moving:  newPath != m.VideoPath,
		})
	}

	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	for _, target := range targets {
		group := byTarget[target]

		if len(group) == 1 {
			o := group[0]
			if o.moving {
				plan.Moves = append(plan.Moves, PlannedMove{VideoID: o.videoID, OldPath: o.oldPath, NewPath: o.newPath})
			} else {
				plan.Unchanged++
			}
			continue
		}

		// More than one video claims this target path. Any occupant that
		// isn't moving is already resident there and is left in place
		// (it still counts as unchanged); any occupant that would need to
		// move is excluded from Moves, since moving it risks clobbering
		// whichever file -- moved or resident -- ends up at that path
		// first. The whole group is reported as one conflict so the
		// caller can see every video involved.
		oldPaths := make([]string, 0, len(group))
		for _, o := range group {
			oldPaths = append(oldPaths, o.oldPath)
			if !o.moving {
				plan.Unchanged++
			}
		}
		sort.Strings(oldPaths)
		plan.Conflicts = append(plan.Conflicts, Conflict{NewPath: target, OldPaths: oldPaths})
	}

	return plan, nil
}

// Apply performs the moves in plan against root, updating the catalog and
// recording rename history. Apply refuses to touch the filesystem when
// r.DryRun is true -- defense in depth alongside the caller's own
// dry-run branching.
func (r *Renamer) Apply(root string, plan Plan) (Result, error) {
	if r.DryRun {
		return Result{Conflicts: plan.Conflicts, Blocked: []PlannedMove{}}, nil
	}
	if len(plan.Moves) == 0 {
		return Result{Conflicts: plan.Conflicts, Blocked: []PlannedMove{}}, nil
	}

	now := time.Now().Unix()
	runID, err := r.Catalog.StartRenameRun(now)
	if err != nil {
		return Result{}, fmt.Errorf("start rename run: %w", err)
	}

	renamed := 0
	blocked := []PlannedMove{}
	for _, mv := range plan.Moves {
		oldFull := filepath.Join(root, filepath.FromSlash(mv.OldPath))
		newFull := filepath.Join(root, filepath.FromSlash(mv.NewPath))

		// Plan()'s collision detection only knows about other confirmed
		// matches -- it has no visibility into unmatched/needs-review
		// videos or any other untracked file that might already occupy
		// this exact target path in the real library. os.Rename onto an
		// existing file silently and atomically replaces it on Unix, so
		// guard against that here with an existence check immediately
		// before the rename, and skip this move (without aborting the
		// rest of the batch) if the target is already occupied.
		//
		// On a case-insensitive filesystem (macOS APFS/HFS+, Windows NTFS
		// by default -- this project's actual target platforms),
		// os.Lstat(newFull) resolves case-insensitively, so a legitimate
		// case-only or Unicode-normalization-only rename (e.g.
		// "artist/song.mp4" -> "Artist/Song.mp4") makes newFull appear to
		// already exist -- but what's actually sitting there is the source
		// file itself, just spelled with different case. Use os.SameFile
		// to distinguish that from a real conflict (a different file
		// already occupying the target): only treat this as blocked when
		// the existing entry is NOT the same underlying file as the
		// source.
		if newInfo, err := os.Lstat(newFull); err == nil {
			oldInfo, err := os.Lstat(oldFull)
			if err != nil {
				return Result{}, fmt.Errorf("check source path %s: %w", mv.OldPath, err)
			}
			if !os.SameFile(oldInfo, newInfo) {
				blocked = append(blocked, mv)
				continue
			}
			// Same underlying file: this is a case-only or
			// Unicode-normalization-only rename on a case-insensitive
			// filesystem, not a real conflict. Fall through and perform
			// the rename as if the target didn't exist.
		} else if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("check existing target %s: %w", mv.NewPath, err)
		}

		if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
			return Result{}, fmt.Errorf("create directory for %s: %w", mv.NewPath, err)
		}
		if err := os.Rename(oldFull, newFull); err != nil {
			return Result{}, fmt.Errorf("rename %s to %s: %w", mv.OldPath, mv.NewPath, err)
		}
		if err := r.Catalog.UpdateVideoPath(mv.VideoID, mv.NewPath); err != nil {
			return Result{}, fmt.Errorf("update catalog path for video %d: %w", mv.VideoID, err)
		}
		if err := r.Catalog.RecordRenameHistory(runID, mv.VideoID, mv.OldPath, mv.NewPath); err != nil {
			return Result{}, fmt.Errorf("record rename history for video %d: %w", mv.VideoID, err)
		}
		renamed++
	}

	if err := r.Catalog.FinishRenameRun(runID, renamed, time.Now().Unix()); err != nil {
		return Result{}, fmt.Errorf("finish rename run: %w", err)
	}

	return Result{FilesRenamed: renamed, Conflicts: plan.Conflicts, Blocked: blocked}, nil
}
