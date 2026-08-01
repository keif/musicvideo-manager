# Exact-Hash Duplicate Detection — Design Spec

Status: Approved
Date: 2026-07-31
Scope: M5 (Duplicate Detection), first slice. Finding byte-identical
video files (e.g. the same video accidentally downloaded twice) via full
SHA-256 hashing. Chromaprint audio fingerprinting and frame/perceptual
video fingerprinting (both near-duplicate detection across different
encodes/sources of the same content) are explicitly deferred to future
slices of this milestone — different techniques, different dependencies,
different design surface.

## Context

Builds on Foundation/Scanner (the catalog, `scan`, and the
`(file_size, partial_hash)` identity already computed for every active
video) and directly fulfills what ADR 0004 anticipated: `partial_hash`
(`SHA-256` of the first/last 1MiB, used for cheap move/rename detection
during `scan`) was explicitly chosen over full-file hashing for scan-time
performance, with the ADR stating "Full SHA-256 ... [is] deferred to a
future `duplicates` sub-project, where accuracy matters more than scan
speed" and calling out that a `(file_size, partial_hash)` collision
between two genuinely different files is possible (same size, same
first/last 1MiB, different middle bytes) and that "the `duplicates`
sub-project's full-hash pass is the authoritative check when it matters."
This spec is exactly that authoritative check.

## Goals

- Find groups of active videos that are byte-identical (true duplicates),
  using full SHA-256 hashing.
- Bound the expensive full-hash I/O to genuine candidates only: videos
  that already collide on the cheap, already-computed
  `(file_size, partial_hash)` pair — not the whole library.
- Report duplicate groups (paths, size, reclaimable space) via a new
  `duplicates` CLI command, independent of `scan`/`match`/`rename`/`nfo`/
  `artwork`/`thumbnail` — runnable anytime once the library has been
  scanned.
- Report only — no filesystem changes. Identifying duplicates and acting
  on them (deleting/consolidating) are different problems with very
  different risk profiles; this slice only does the former.

## Non-goals (deferred to future work)

- Chromaprint audio fingerprinting (near-duplicate detection via audio
  content — same song, different video source/re-encode) — a separate
  future slice of M5, needs a new external binary (`fpcalc`) and
  similarity-threshold matching logic.
- Frame/perceptual video fingerprinting (near-duplicate detection via
  visual content) — another separate future slice, needs its own hashing
  approach and threshold logic.
- Any deletion, consolidation, "pick a keeper," quarantine, or other
  filesystem action on duplicate groups — this pass only reports.
- Any new catalog schema (new tables/columns) — this slice needs none; see
  Architecture.
- Any config-file settings — nothing about exact-hash duplicate detection
  is user-configurable in this pass (unlike `rename.template` or
  `thumbnail.percent`).

## New package

```
internal/duplicates/
└── finder.go       # FindExactDuplicates: group by (size, partial_hash), confirm via full SHA-256
```

No catalog changes. `FindExactDuplicates` consumes the existing
`(*catalog.Catalog) ListActive() ([]*catalog.Video, error)` — already
returns `FileSize`, `PartialHash`, and `Path` for every active video —
and does all grouping in memory.

## Algorithm

```go
func FindExactDuplicates(cat *catalog.Catalog, root string) (Result, error)
```

1. Call `cat.ListActive()` to get every active video.
2. Group videos in memory by `(FileSize, PartialHash)`. Any group with
   more than one member is a *candidate set* — files already identical in
   size and in their first/last 1MiB, per the scanner's existing
   `partial_hash` definition (ADR 0004). A video in a group of size 1 is
   never touched again — no full-hash I/O for it at all, keeping this
   bounded to genuinely suspicious files in a healthy library.
3. For every video in a candidate set, stream the full file through
   `crypto/sha256` (`io.Copy` into the hasher — never `os.ReadFile`, since
   video files can be multiple gigabytes and must not be loaded whole into
   memory). A read failure (permissions, file vanished mid-run, etc.) is
   recorded in the run's error list and that video is excluded from
   grouping — not a hard command failure, the same error-tolerance
   convention as every other writer/finder in this codebase.
4. Sub-group each candidate set by its members' full SHA-256 hash. A
   sub-group with more than one member is a confirmed, authoritative
   duplicate group. A candidate set that turns out to differ in the middle
   bytes (ADR 0004's documented false-positive case for `partial_hash`
   alone) is correctly *not* reported — this is the whole reason the
   full-hash confirmation step exists.
5. Compute `ReclaimableBytes` per group as `(count - 1) * FileSize`
   (keeping exactly one copy of each duplicate group is assumed to be the
   floor of what's reclaimable), summed across all groups.

```go
type DuplicateGroup struct {
    FileSize int64    `json:"file_size"`
    Hash     string   `json:"hash"`
    Paths    []string `json:"paths"` // absolute (root-joined), directly actionable
}

type DuplicateErrorEntry struct {
    VideoPath string `json:"video_path"`
    Message   string `json:"message"`
}

type Result struct {
    VideosScanned    int                   `json:"videos_scanned"`
    CandidatesHashed int                   `json:"candidates_hashed"`
    DuplicateGroups  []DuplicateGroup      `json:"duplicate_groups"`
    ReclaimableBytes int64                 `json:"reclaimable_bytes"`
    Errors           []DuplicateErrorEntry `json:"errors"`
}
```

`VideosScanned` is the total active video count considered.
`CandidatesHashed` is how many of those actually had their full bytes
read (i.e. were in a candidate set of size > 1) — the number this design
is explicitly trying to keep small relative to `VideosScanned` in a
healthy library.

No `context.Context` parameter: this package does no network I/O and
shells out to no external process (unlike `internal/artwork`/
`internal/thumbnail`), so there is nothing to usefully cancel mid-flight —
matching the precedent already set by `nfo.WriteAll(cat, root)`, the
other ctx-less writer in this codebase.

## CLI surface

```
musicvideo-manager duplicates [path] [--json]
```

Same "not yet scanned" guard and catalog-open sequence as `nfo`. No config
load needed (no user-configurable settings in this pass). Report-only:
nothing is renamed, moved, or deleted.

## Testing

Pure Go — no `ffmpeg`, no network, no `httptest` server. The simplest
testing story of any milestone so far. Real temp files with controlled
byte content, real `catalog.InsertVideo` calls with explicitly-set
`FileSize`/`PartialHash` (no dependency on `internal/scanner`'s actual
hash computation — this package only ever consumes catalog rows, tests
set them directly, consistent with how `internal/artwork`/
`internal/thumbnail` tests don't depend on `internal/match`/
`internal/scanner` internals either). Coverage:

- Two byte-identical files are correctly grouped as a duplicate, with the
  right hash and reclaimable-bytes value.
- Two files sharing `(file_size, partial_hash)` but differing in their
  middle bytes are correctly NOT reported (the ADR 0004 false-positive
  case — the reason the full-hash confirmation step exists).
- A video with no `(file_size, partial_hash)` collision is never read at
  all — verified by asserting it's excluded from `CandidatesHashed`.
- A three-or-more-way duplicate group is grouped correctly.
- A file-read error on one candidate is recorded in `Errors` and doesn't
  block grouping of the rest of the run.
- `ReclaimableBytes` arithmetic is correct across multiple groups of
  different sizes.

## Roadmap (context, not detailed here)

- **Fanart.tv artist artwork** — already implemented.
- **Local video thumbnails** — already implemented.
- **This spec** — exact-hash duplicate detection (full SHA-256).
- **Future** — Chromaprint audio fingerprinting; frame/perceptual video
  fingerprinting (both M5); collections + playlists (M6); web UI (M7).
