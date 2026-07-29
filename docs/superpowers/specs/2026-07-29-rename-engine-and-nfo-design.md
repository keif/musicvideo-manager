# Rename Engine & Kodi NFO Export — Design Spec

Status: Approved
Date: 2026-07-29
Scope: M3 (Rename Engine + NFO). Configurable rename templates with dry-run,
apply, and undo; Kodi-format `musicvideo.nfo`/`artist.nfo` generation.
Plex's `movie.nfo` workaround, Jellyfin/Emby-specific quirks beyond what
Kodi-format NFO already gets read by, and any rename-collision auto-resolution
smarter than "flag and skip" are explicitly deferred to future work.

## Context

Builds on the completed Foundation/Scanner (M0/M1) and Metadata Matching (M2)
milestones. The catalog now holds 624 catalogued videos against the real
library at `/Volumes/Yggdrasil/Music Videos`, of which 293 have a confirmed
MusicBrainz match (artist + title, often album/year) and 331 are unmatched.
This spec adds the first filesystem-mutating feature: renaming video files
into a configurable, templated layout, and generating Kodi-format NFO sidecar
files so Kodi/Jellyfin/Emby can read correct metadata without a network
scrape.

A real-data finding informs the design: for confirmed matches, the `album`
field returned by MusicBrainz's recording search is whatever release happens
to be first in the API's release list, not necessarily the canonical studio
album — e.g. Alice Cooper's "Poison" resolved to "Theatre of Death: Live at
Hammersmith 2009," a live compilation. Any rename template depending on
`{album}` inherits this noise, so the default template avoids it.

## Goals

- Configurable rename templates (`{artist}`, `{title}`, `{album}`, `{year}`)
  with a safe default that doesn't depend on the noisy album field.
- Dry-run mode: preview every planned move with no filesystem writes.
- Apply mode: perform the moves, recording enough history to undo them.
- Undo: reverse the most recently applied rename run in full.
- Kodi-format NFO generation: `<basename>.nfo` per video (`<musicvideo>`
  root) and `artist.nfo` per artist folder (`<artist>` root), independent of
  whether rename has run, so NFOs can be refreshed later without re-renaming.
- Collision safety: two videos computing the same target path are flagged
  and left untouched rather than one silently overwriting the other.

## Non-goals (deferred to future work)

- Plex's `movie.nfo` workaround (treating each video as a standalone
  "movie") — a separate follow-on once this Kodi-format path is proven.
- Any command to resolve a rename collision automatically (e.g.
  auto-suffixing `(2)`) — collisions are a real, current risk given known
  duplicate files in the library (e.g. `In Da Club-video.mp4` and
  `In Da Club-video.ts`), and proper resolution belongs to M5 (Duplicate
  Detection). This milestone only detects and reports them.
- Renaming or generating NFOs for `needs_review`/`unmatched` videos — only
  `confirmed` matches carry enough data to build a template from.
- Per-run or per-file undo browsing (only "undo the last applied run" as a
  whole).

## New packages

```
internal/rename/
├── template.go   # RenderTemplate(template string, fields TemplateFields) (relPath string, err error)
├── rename.go     # Renamer{Catalog, Template, DryRun}.Plan(ctx) / .Apply(ctx)
└── undo.go       # Undo(cat *catalog.Catalog, root string) (UndoResult, error)

internal/nfo/
├── musicvideo.go # Kodi <musicvideo> XML generation (per-video NFO)
└── artist.go     # Kodi <artist> XML generation (per-artist NFO)
```

## Catalog schema additions (migration `0003`)

```sql
CREATE TABLE rename_runs (
    id            INTEGER PRIMARY KEY,
    started_at    INTEGER NOT NULL,
    finished_at   INTEGER,
    files_renamed INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'applied'  -- 'applied' | 'undone'
);

CREATE TABLE rename_history (
    id             INTEGER PRIMARY KEY,
    rename_run_id  INTEGER NOT NULL REFERENCES rename_runs(id),
    video_id       INTEGER NOT NULL REFERENCES videos(id),
    old_path       TEXT NOT NULL,
    new_path       TEXT NOT NULL
);
```

Only real applies get a `rename_runs` row — dry runs never touch the
catalog, since there's nothing to undo for a preview. `rename --undo` finds
the most recent `status = 'applied'` row, reverses every associated
`rename_history` entry (moving `new_path` back to `old_path`), and flips
that run's status to `'undone'` so it's never targeted by undo again and
is never deleted, matching the project's existing "never destroy history"
philosophy (`videos.status`, `matches` never deleted).

After a rename applies, `videos.path` must be updated to the new location
so the catalog stays consistent with the filesystem. This uses a new,
dedicated catalog method (`UpdateVideoPath(id int64, newPath string) error`
— a plain path swap, nothing else), not the scanner's existing
`UpdatePath` (which reactivates a row from `missing` and touches
`last_seen_at`/`last_scanned_at`, semantics that belong to scan
reconciliation, not a rename that isn't "seeing" the file for the first
time).

## Rename template

`RenderTemplate(template string, fields TemplateFields) (relPath string, err error)`
where `TemplateFields{Artist, Title, Album string, Year int}` comes from the
video's confirmed match (joined through `songs`/`artists`).

- Placeholders: `{artist}`, `{title}`, `{album}`, `{year}`. `/` in the
  template creates subdirectories; everything else is a literal path
  segment. The source file's extension is preserved automatically —
  templates describe only the path stem.
- Default template: `{artist}/{title}` — deliberately excludes `{album}`/
  `{year}` given the album-noise finding above. Config:
  ```yaml
  rename:
    template: "{artist}/{title}"
  ```
- Each placeholder's substituted *value* has filesystem-illegal characters
  stripped (`/ \ : * ? " < > |`) before insertion, so a stray character in
  an artist/title/album name can never inject an unintended path segment.
- After substitution, empty path segments collapse (a template using
  `{album}` for a video with no album data produces `Artist/Title`, not
  `Artist//Title`).

## Collision handling

`Renamer.Plan` computes every target path for every eligible video up
front, before any filesystem write, and groups by target path. Any target
path claimed by more than one video is a collision: all videos in that
group are excluded from the plan's "to apply" set and reported separately
as conflicts (with all colliding source paths and the shared target). This
is expected to actually happen with the current library's known duplicate
files. `Apply()` only ever executes the non-conflicting subset of a plan.

## Renamer orchestration

```go
type Renamer struct {
    Catalog  *catalog.Catalog
    Template string
    DryRun   bool
}

func (r *Renamer) Plan(ctx context.Context, root string) (Plan, error)
func (r *Renamer) Apply(ctx context.Context, root string, plan Plan) (Result, error)
```

1. Query confirmed matches joined with videos/songs/artists via a new
   catalog method, `ListConfirmedMatches() ([]ConfirmedMatch, error)`,
   returning `ConfirmedMatch{VideoID int64, VideoPath string, Artist,
   Title, Album string, Year int}`.
2. For each, render the template into a target relative path; skip (not an
   error) if target equals the video's current path.
3. Detect collisions across the full candidate set (see above).
4. `Plan` returns the full breakdown (to-move, skipped-already-correct,
   conflicts) without touching the filesystem — this is what `--dry-run`
   prints.
5. `Apply` (only when `DryRun` is false) creates target parent directories
   as needed, performs the moves via `os.Rename`, updates `videos.path` in
   the catalog for each moved video, and records the run + history.

## NFO generation

Independent of `Renamer` — reads whatever path a video currently has.

- **Per-video NFO**: for a video at `<dir>/<base>.<ext>`, write
  `<dir>/<base>.nfo`. Root element `<musicvideo>` with `<title>`,
  `<artist>`, `<album>`, `<year>`, `<filenameandpath>`. Built via Go's
  `encoding/xml` marshaling (never string concatenation), so special
  characters in real titles/artists (apostrophes, ampersands, the
  stylized hyphen in "The All‐American Rejects") are escaped correctly.
- **Per-artist NFO**: one `artist.nfo` per distinct artist among confirmed
  matches, written to that artist's folder (the parent directory of its
  videos). Root element `<artist>` with `<name>`. Regenerated fresh on
  every run (overwritten, not skip-if-exists) so it reflects current match
  data.
- Only videos with `matches.status = 'confirmed'` get NFOs.

## CLI surface

```
musicvideo-manager rename [path] [--dry-run] [--json]
musicvideo-manager rename --undo [path]
musicvideo-manager nfo [path] [--json]
```

`rename` without `--dry-run` performs real moves; `--dry-run` only prints
the plan. `nfo` always writes (there's no meaningful dry-run for a sidecar
file that doesn't move anything).

## Testing

- `template.go`: table-driven tests for substitution, illegal-character
  sanitization, empty-segment collapse, and collision grouping — pure
  functions, no filesystem I/O.
- `rename.go`/`undo.go`: tested against temporary directories with fixture
  files (never the real library) — covering apply, dry-run (no filesystem
  changes), idempotent rerun, collision exclusion, and undo reversing a
  real applied run including the `videos.path` catalog update.
- `nfo` package: XML generation correctness (escaping, valid Kodi schema
  shape), tested via `encoding/xml` round-trip or direct string assertion
  on marshaled output — no filesystem I/O required for the generation logic
  itself, though the CLI-level writer is tested against temp directories.

## Safety: real-library verification is dry-run only

Unlike `scan`/`match` (catalog-only, additive), `rename --apply` mutates
real files on disk. This milestone's real-library verification step runs
`rename` in `--dry-run` mode only against the actual library, to confirm
the planned renames for all 293 confirmed videos look correct — no real
file is moved as part of building or verifying this milestone. Actually
applying the rename to the real library is a distinct, explicit action
outside this spec's automated verification, to be taken only after
reviewing the dry-run output.

## Roadmap (context, not detailed here)

- **This spec** — rename engine (template/dry-run/apply/undo), Kodi-format
  NFO generation.
- **Future** — Plex `movie.nfo` workaround; artwork download/caching (M4);
  duplicate detection with proper collision resolution (M5); collections +
  playlists (M6); web UI (M7).
