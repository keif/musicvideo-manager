# 4. Partial-hash-by-default identity and move detection

Status: Accepted

## Context

The scanner needs a cheap way to recognize "this is the same file I saw
before, just moved or renamed" without re-reading every byte of every file
on every scan — collections here run into the hundreds to thousands of
files, some multiple gigabytes.

## Decision

Scan-time identity uses `partial_hash = SHA-256(first 1MiB || last 1MiB)`
(or the whole file if it's under 2MiB), combined with the stored
`file_size`. This is used for move/rename detection during `scan`. Full
SHA-256 and Chromaprint audio fingerprinting are deferred to a future
`duplicates` sub-project, where accuracy matters more than scan speed.

## Consequences

- A file whose first/last 1MiB is unchanged but whose middle differs (rare
  for real edits, but possible for deliberately crafted files) will not be
  detected as changed until its size or mtime also changes.
- Two different files that happen to collide on `(file_size, partial_hash)`
  can be misidentified as a move; [[0001's]] scanner reconciliation breaks
  ties by closest `mtime` and treats the rest as new inserts — a heuristic,
  not a correctness guarantee. The `duplicates` sub-project's full-hash
  pass is the authoritative check when it matters.
