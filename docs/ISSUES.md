# Planned GitHub Issues — M0 Foundation & M1 Scanner

Copy each section below into a GitHub issue (`gh issue create --title "..." --body-file -` or
the web UI) once this repo has a remote. Grouped by milestone; each maps to
one or more tasks in `docs/superpowers/plans/2026-07-20-foundation-and-scanner.md`.

## M0 — Foundation

### Scaffold the Go module and repository layout
Set up `go.mod`, `.gitignore`, `LICENSE` (MIT), and the `cmd/ internal/ pkg/
web/ docs/ examples/ scripts/ testdata/` directory layout described in the
design spec. Acceptance: `go build ./...` and `go vet ./...` succeed.

### Define the metadata provider plugin interface
Add `pkg/provider` with `MetadataProvider`, `MatchQuery`, `MatchResult`. No
implementations — this is the seam future provider sub-projects build
against. Acceptance: a fake in-package test provider satisfies the
interface.

### Implement the catalog schema and embedded migrations
Add `internal/catalog` with the `videos`/`scan_runs`/`scan_errors` schema
(see the design spec) applied via a hand-rolled embedded-SQL migrator
(`modernc.org/sqlite`, no cgo — see ADR 5). Acceptance: `Open` creates the
schema on a fresh database and is idempotent on re-open.

### Implement the catalog repository methods
Add `Video`/`ScanRunCounts` models and the `ListActive`,
`FindMissingByHash`, `InsertVideo`, `UpdateProbe`, `UpdatePath`,
`TouchSeen`, `MarkMissing`, `StartScanRun`, `FinishScanRun`,
`RecordScanError` methods. Acceptance: unit tests cover insert, list,
status transitions, and hash-based lookup, including NULL metadata fields.

### Implement per-library config load/save
Add `internal/config` with `Config`/`Library`/`ScanConfig` and
`LoadOrCreate`, defaulting extensions to
`mp4,mkv,avi,mov,m4v,ts,mpg,webm` and `workers: 0` (auto). Acceptance: unit
tests cover default-creation, loading an existing file, and rejecting
invalid YAML.

### Add CI, Makefile, lint config, and Dependabot
GitHub Actions matrix over macOS/Linux installing ffmpeg, running
`go vet`/`golangci-lint`/`go test -race -cover`; `Makefile` with
`build/test/lint/testdata/docker-build` targets; Dependabot for Go modules
and Actions. Acceptance: CI is green on a PR that touches only docs.

### Add Docker image and compose service
Multi-stage `Dockerfile` (static binary, ffmpeg installed in the final
stage) and a `docker-compose.yml` mounting a library volume. Acceptance:
`docker build` succeeds and `docker run musicvideo-manager:local --help`
prints usage.

### Write ADRs for the foundation decisions
Five ADRs under `docs/adr/`: canonical catalog, per-library catalog
location, provider plugin interface, partial-hash identity, pure-Go SQLite
+ hand-rolled migrator. Acceptance: each follows the Status/Context/
Decision/Consequences format.

### Write README and CONTRIBUTING
Project overview, status, requirements, usage, roadmap table, dev setup,
test/lint commands, commit conventions. Acceptance: a new contributor can
clone, build, and run the test suite using only the README/CONTRIBUTING.

## M1 — Scanner

### Implement fast partial-content hashing
`ComputeHash` — SHA-256 of the whole file under 2MiB, or first+last 1MiB
above that threshold. Acceptance: unit tests cover empty files, small
files, and large files where only the untouched middle region differs.

### Implement the ffprobe metadata wrapper
`Probe` — shells out to `ffprobe`, parses duration/resolution/codecs/
bitrate/fps/container from its JSON output. Acceptance: an integration
test against an ffmpeg-generated fixture asserts the parsed values; a
second test asserts a corrupt file returns an error rather than a
zero-value result.

### Implement library file discovery
`Discover` — recursive walk filtered to the configured extensions
(case-insensitive), always skipping `.musicvideo-manager/`. Acceptance:
unit tests cover nested subdirectories, mixed-case extensions, the state
directory being skipped even when it contains a matching-extension file,
and an empty/nonexistent root.

### Implement scan reconciliation
`Reconciler.Reconcile` — classifies every discovered file as unchanged,
updated, moved, or newly added against the catalog, and marks unseen
active rows missing. Acceptance: unit tests (using fake hash/probe
functions) cover every classification plus a file that fails to hash/probe
without aborting the whole run.

### Implement scan run orchestration
`scanner.Run` — wraps `Reconciler` with `scan_runs`/`scan_errors`
bookkeeping and returns a `RunSummary`. Acceptance: an integration test
confirms `FilesScanned` accounts for every discovered file including
errors.

### Implement the `scan` and `version` CLI commands
Cobra wiring: `musicvideo-manager scan [path] [--json]` and
`musicvideo-manager version`. Acceptance: an end-to-end test (real ffmpeg
fixture) confirms `.musicvideo-manager/{config.yaml,catalog.sqlite}` are
created, a second run reports zero new files, and `--json` output parses.

---

## Future milestones (placeholders — not detailed until specced)

- M2 Metadata Matching
- M3 Rename Engine + NFO Output
- M4 Artwork
- M5 Duplicate Detection
- M6 Collections + Playlists
- M7 Web UI
