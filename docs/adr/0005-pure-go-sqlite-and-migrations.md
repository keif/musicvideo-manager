# 5. Pure-Go SQLite driver and a hand-rolled embedded migrator

Status: Accepted

## Context

SQLite access from Go typically goes through `mattn/go-sqlite3`, which
wraps the C SQLite library via cgo. cgo complicates cross-compilation
(needs a C toolchain per target) and CI (needs matching toolchains per
platform in the build matrix) for no benefit here, since this project
targets macOS and Linux binaries with no need for SQLite extensions cgo
would otherwise unlock.

Schema migrations were originally planned around `golang-migrate`, but its
maintained SQLite driver depends on `mattn/go-sqlite3`, which would
reintroduce the exact cgo dependency this decision avoids.

## Decision

Use `modernc.org/sqlite`, a pure-Go SQLite implementation, for all database
access. Pair it with a small hand-rolled migrator (`internal/catalog`):
numbered `.sql` files under `internal/catalog/migrations/`, embedded via
`go:embed`, applied in order inside a transaction, tracked in a
`schema_migrations` table the migrator creates itself.

## Consequences

- No cgo anywhere in the build — `go build`/`go test`/cross-compilation
  need only a Go toolchain, matching [[Global Constraints|the macOS+Linux
  platform target]].
- The migrator only supports forward ("up") migrations — acceptable since
  the catalog is local, additive, and per-library; there's no production
  rollback scenario to design for at this scale.
- Future schema changes (adding `artists`, `songs`, `matches`, `artwork`,
  `collections`, `playlists` tables for later sub-projects) are new
  numbered files in the same migrations directory — no change to the
  migrator itself.
