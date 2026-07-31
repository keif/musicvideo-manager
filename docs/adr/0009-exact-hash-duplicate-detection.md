# 9. Exact-hash duplicate detection: no new schema, no caching, report-only

Status: Accepted

## Context

ADR 0004 established `partial_hash` (SHA-256 of the first/last 1MiB) as
the scanner's cheap move/rename identity check, explicitly deferring full
SHA-256 hashing to "a future `duplicates` sub-project, where accuracy
matters more than scan speed," and noting that a `(file_size,
partial_hash)` collision between two genuinely different files is
possible and that "the `duplicates` sub-project's full-hash pass is the
authoritative check when it matters." This is that sub-project's first
slice.

## Decision

`internal/duplicates.FindExactDuplicates` adds no new catalog schema. It
consumes `catalog.ListActive()`'s existing `FileSize`/`PartialHash`/`Path`
fields directly and does all grouping in memory. Full SHA-256 hashing is
bounded to videos that already collide on `(FileSize, PartialHash)` with
at least one other active video -- in a healthy library this is a small
fraction of the total, keeping the expensive full-file read work bounded
without needing any new persistence.

The computed full hash is **not** cached anywhere (no new `videos.full_hash`
column). Every `duplicates` run recomputes full hashes for that run's
candidate set from scratch. This was a deliberate simplicity choice over
persisting and invalidating a cache: caching would require deciding when a
cached hash goes stale (on `mtime`/`file_size` change, requiring a change
to the scanner's reconciliation logic) and this pass's candidate sets are
expected to be small relative to the whole library, so the repeated-read
cost was judged not worth that complexity for a first slice.

This feature is entirely read-only: it renames, moves, or deletes nothing.
Identifying duplicates and acting on them are different problems with very
different risk profiles, and this slice only does the former.

## Consequences

- Running `duplicates` repeatedly on an unchanged library re-reads every
  candidate file's full bytes each time, rather than reusing a prior run's
  result. If a library's candidate set ever grows large enough for this to
  matter in practice, revisit adding a persisted, invalidation-aware full
  hash -- this ADR's "no caching" call is a first-slice simplicity
  decision, not a permanent architectural constraint.
- Because nothing is cached, `duplicates` and `scan` are fully decoupled:
  running `duplicates` never needs to touch or understand scan-time
  reconciliation logic, and adding this feature required zero changes to
  any previously-shipped code.
- Future M5 slices (Chromaprint audio fingerprinting, frame/perceptual
  video fingerprinting) are near-duplicate detection, a different problem
  from this slice's exact-duplicate detection -- they will likely need
  their own persisted fingerprint storage (unlike this slice), since
  fingerprint computation is more expensive than a full-file hash and
  near-duplicate comparison can't reuse the cheap `partial_hash` pre-filter
  this slice relies on.
