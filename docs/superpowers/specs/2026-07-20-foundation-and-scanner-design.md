# Foundation & Scanner — Design Spec

Status: Approved
Date: 2026-07-20
Scope: M0 (Foundation) + M1 (Scanner) only. Metadata matching, rename engine,
NFO/artwork generation, duplicate detection, collections, playlists, and the
web UI are each out of scope for this spec and will get their own
brainstorm → spec → plan cycle once this foundation is built and tested.

## Context

This is the first of several sub-projects that together form
**musicvideo-manager**: an open-source, media-server-agnostic tool for
organizing, enriching, and managing local music video collections (initial
target: Plex, architected to support Jellyfin/Emby/Kodi/static sites later).

The core differentiator from existing media managers: the application
maintains its own canonical SQLite catalog of every video, metadata source,
artwork, fingerprint, and history — the filesystem and NFO files are
*exports* of that catalog, not the source of truth.

Reference collection used to inform design decisions: 618+ existing files
under a real library root, already organized by artist folder, with
filenames like `Artist - Title Official Video [youtube-id].mp4`.

## Goals (this spec)

- Stand up the repository, module, and tooling for the whole project.
- Define the canonical catalog schema (v1) and how it evolves over time.
- Define the metadata provider plugin interface (no implementations yet).
- Implement a working `scan` command: walk a library, extract metadata via
  ffprobe, and populate the catalog — with correct incremental rescan,
  move/rename detection, and missing-file handling.

## Non-goals (deferred to future specs)

- Artist/song/title identification or any metadata provider implementation
  (MusicBrainz, Discogs, TheAudioDB, Last.fm).
- Rename engine, NFO generation, artwork downloading.
- Duplicate detection beyond what move/rename detection incidentally
  provides (no Chromaprint audio fingerprinting, no frame fingerprinting).
- Collections, playlists, web UI.
- Stub CLI commands for unimplemented features — commands are added only
  when their sub-project lands, to avoid dead code sitting in the tree.

## Repository layout

```
musicvideo-manager/
├── cmd/musicvideo-manager/     # main.go — thin entrypoint, wires cli package
├── internal/
│   ├── catalog/                # SQLite schema, migrations, query layer
│   ├── scanner/                # ffprobe walk, hashing, reconciliation
│   ├── config/                 # per-library config.yaml load/save
│   └── cli/                    # Cobra command wiring
├── pkg/
│   └── provider/                # MetadataProvider interface + shared types
├── web/                         # empty placeholder for phase 2 React UI
├── docs/
│   ├── adr/
│   └── superpowers/specs/
├── examples/
├── scripts/
├── testdata/
├── .github/workflows/
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── README.md
├── CONTRIBUTING.md
└── LICENSE                      # MIT
```

`internal/` holds implementation details private to this module.
`pkg/provider` is public because it's the seam future metadata-provider
sub-projects (and eventually a plugin SDK, a stretch goal) implement
against — defining the interface now costs little and avoids an awkward
later move from `internal` to `pkg`.

## Technology choices

- **Go + Cobra** for the CLI, per project requirements.
- **`modernc.org/sqlite`** — pure-Go SQLite driver, no cgo. Simplifies
  cross-compiling and CI for the macOS + Linux target matrix (no C
  toolchain dependency in the release pipeline).
- **`golang-migrate`** with `go:embed`'d SQL migration files, so the schema
  can grow additively as future sub-projects add tables (artists, songs,
  matches, artwork, collections, playlists) without touching this spec.
- **Module path:** `github.com/keif/musicvideo-manager`.
- **Target platforms:** macOS and Linux (Windows deferred; Go cross-compiles
  easily so it's a low-cost future addition).

## Catalog & config location

Each library is self-contained. The catalog and config live in a hidden
directory at the library root, not in a central per-machine app-data
directory:

```
<library-root>/.musicvideo-manager/
├── config.yaml
├── catalog.sqlite
└── cache/            # reserved for future API response cache
```

Rationale: the catalog travels with the drive. If the library moves to a
new machine or server, the catalog and config move with it — no separate
backup/sync step required. Multiple libraries are simply multiple
independent `.musicvideo-manager/` directories; there is no cross-library
global state in this phase.

## Config file

`.musicvideo-manager/config.yaml`, created on first `scan` if absent:

```yaml
schema_version: 1
library:
  name: "My Music Videos"
scan:
  extensions: [mp4, mkv, avi, mov, m4v, ts, mpg, webm]
  workers: 0   # 0 = auto (runtime.NumCPU())
```

Rename templates, provider credential references, and collection rules
will be added to this file by their respective future specs. Provider API
keys/secrets themselves are never stored here — they come from environment
variables or a separate global `~/.config/musicvideo-manager/credentials.yaml`
that is never part of a library and never synced with it.

## Catalog schema (v1)

```sql
CREATE TABLE videos (
    id                INTEGER PRIMARY KEY,
    path              TEXT NOT NULL UNIQUE,   -- relative to library root
    file_size         INTEGER NOT NULL,
    mtime             INTEGER NOT NULL,        -- unix seconds, filesystem mtime
    partial_hash      TEXT NOT NULL,           -- see "Hashing strategy" below
    status            TEXT NOT NULL DEFAULT 'active',  -- 'active' | 'missing'
    duration_seconds  REAL,
    width             INTEGER,
    height            INTEGER,
    video_codec       TEXT,
    audio_codec       TEXT,
    container         TEXT,
    bitrate           INTEGER,
    fps               REAL,
    first_seen_at     INTEGER NOT NULL,
    last_seen_at      INTEGER NOT NULL,
    last_scanned_at   INTEGER NOT NULL
);
CREATE INDEX idx_videos_status ON videos(status);
CREATE INDEX idx_videos_hash ON videos(partial_hash);

CREATE TABLE scan_runs (
    id             INTEGER PRIMARY KEY,
    started_at     INTEGER NOT NULL,
    finished_at    INTEGER,
    files_scanned  INTEGER NOT NULL DEFAULT 0,
    files_added    INTEGER NOT NULL DEFAULT 0,
    files_updated  INTEGER NOT NULL DEFAULT 0,
    files_moved    INTEGER NOT NULL DEFAULT 0,
    files_missing  INTEGER NOT NULL DEFAULT 0,
    errors_count   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE scan_errors (
    id           INTEGER PRIMARY KEY,
    scan_run_id  INTEGER NOT NULL REFERENCES scan_runs(id),
    path         TEXT NOT NULL,
    message      TEXT NOT NULL,
    occurred_at  INTEGER NOT NULL
);
```

Metadata fields (`duration_seconds`, `width`, `height`, `video_codec`,
`audio_codec`, `container`, `bitrate`, `fps`) are nullable: if ffprobe can't
determine a value for a corrupt or unusual file, the row is still stored
with those fields null rather than failing the scan.

`artists`, `songs`, `albums`, `matches`, `artwork`, `collections`,
`playlists`, and a provider response cache table are explicitly **not**
part of this schema — they are added by future migrations when their
owning sub-project is designed.

## Provider plugin interface

`pkg/provider/provider.go` — interface and shared types only, no
implementations in this phase:

```go
package provider

import "context"

type MatchQuery struct {
    Filename string
    Artist   string // best-effort guess from filename parsing; may be empty
    Title    string
}

type MatchResult struct {
    Artist     string
    Title      string
    Album      string
    Year       int
    Confidence float64 // 0.0-1.0
    SourceID   string  // provider-specific ID (MBID, Discogs ID, etc.)
}

type MetadataProvider interface {
    Name() string
    Match(ctx context.Context, q MatchQuery) ([]MatchResult, error)
}
```

Future MusicBrainz/Discogs/TheAudioDB/Last.fm providers implement this
interface; none are implemented here.

## Hashing strategy

Full SHA-256 or Chromaprint audio fingerprinting is deferred to a future
`duplicates` sub-project. For scan-time identity and move/rename detection,
a **fast partial hash** is used: `partial_hash = SHA-256(first 1MiB bytes
|| last 1MiB bytes)`, computed alongside the separately-stored `file_size`.
Files smaller than 2MiB hash their entire contents once (no overlap). This
is cheap even on large libraries and is sufficient to detect renames/moves
and catch the common duplicate case without reading entire files on every
scan.

If more than one missing catalog row shares the same `(file_size,
partial_hash)` as a single new file — a true hash collision or genuinely
identical files — the move is resolved to the missing row with the closest
`mtime`; any remaining candidates are inserted as new rows instead of
matched as moves. This is a heuristic, not a guarantee; exact-duplicate
libraries get precise resolution later from the `duplicates` sub-project's
full-hash pass.

## Scan algorithm

1. Resolve the library root. Create `.musicvideo-manager/` if absent
   (default `config.yaml`, empty `catalog.sqlite` with migrations applied).
   Apply any pending migrations if the catalog already exists.
2. `filepath.WalkDir` recursively from the root, skipping
   `.musicvideo-manager/` itself, filtering to the configured extensions
   (case-insensitive): mp4, mkv, avi, mov, m4v, ts, mpg, webm.
3. **Cheap pass:** `os.Stat` every candidate file, building an in-memory
   map of `path → (size, mtime)`. No hashing or ffprobe yet, so this pass
   stays fast regardless of library size.
4. **Reconcile** against the catalog:
   - *Unchanged* — path exists in the catalog, stored size and mtime match
     → update `last_seen_at` only.
   - *Changed* — path exists, size or mtime differs → recompute the
     partial hash, re-run ffprobe, update the row.
   - *New path* — compute the partial hash and run ffprobe. If
     `(file_size, partial_hash)` matches a catalog row not yet seen this
     run, treat it as a **move/rename**: update that row's `path` and
     `mtime` rather than inserting a new row. Otherwise insert a new row.
   - *Missing* — any catalog row not seen this run and not matched as a
     move → set `status = 'missing'`. Rows are never deleted by `scan`;
     deletion is a manual/future `doctor` concern.
5. ffprobe invocations run through a bounded worker pool (default
   `runtime.NumCPU()`, overridable via `scan.workers` in config) since
   ffprobe is the slow step; partial-hash computation is cheap enough to
   run inline.
6. A file that fails to probe (corrupt/unreadable) is logged to
   `scan_errors` with its path and error message and skipped — one bad
   file never aborts the whole scan.
7. The run is recorded in `scan_runs` (added/updated/moved/missing/error
   counts, start/finish timestamps). The CLI prints a human-readable
   summary at the end, or JSON with `--json`.

## CLI surface (this phase)

```
musicvideo-manager scan [path]     # defaults to cwd if path omitted
musicvideo-manager version
```

No other subcommands are added yet. `match`, `rename`, `artwork`, `doctor`,
`duplicates`, and `playlists` are documented in the README roadmap but not
stubbed as commands, to avoid dead code between now and when each is
actually implemented.

## Testing

- **Unit tests** for the reconciliation logic (unchanged / changed / new /
  moved / missing), using tiny synthetic fixture videos generated via
  ffmpeg (a `make testdata` target generates a few seconds of color bars +
  silent audio — small, license-free, deterministic).
- **Edge cases:** zero-byte file, file with no audio track, a file ffprobe
  cannot parse at all (corrupt), filenames containing spaces / brackets /
  unicode / SQL metacharacters (`'`, `;`) — verified safe because all
  catalog queries are parameterized — an empty library directory, and a
  library root that doesn't exist yet.
- **Integration test:** run `scan` twice against a fixture directory (first
  run = all new, second run = all unchanged), then against a mutated copy
  (one file renamed, one edited, one deleted) to verify move and missing
  detection end-to-end.

## CI, Docker, tooling

- **GitHub Actions:** matrix over `macos-latest` and `ubuntu-latest`;
  install ffmpeg; run `go vet`, `golangci-lint`, `go test ./... -race
  -cover`. Dependabot enabled for Go modules and Actions.
- **Docker:** multi-stage `Dockerfile` producing a static binary, plus
  `docker-compose.yml` mounting a library directory as a volume, for
  running scans on a home server without a local Go toolchain.
- **Makefile** targets: `build`, `test`, `lint`, `testdata`, `docker-build`.

## ADRs to write alongside implementation

1. Canonical SQLite catalog as source of truth (not filesystem/NFO-driven).
2. Per-library catalog & config colocated in the library root.
3. Metadata provider plugin interface.
4. Partial-hash-by-default identity/dedup strategy.
5. Pure-Go SQLite driver + golang-migrate for schema evolution.

## Roadmap (context, not detailed here)

- **M0 Foundation** (this spec) — scaffold, schema v1, provider interface
  stub, CLI skeleton, CI/Docker/Makefile.
- **M1 Scanner** (this spec) — working `scan` end-to-end with tests.
- **M2 Metadata Matching** — future spec: filename parsing, embedded tags,
  MusicBrainz/Discogs/TheAudioDB/Last.fm providers, confidence scoring.
- **M3 Rename Engine + NFO Output** — future spec.
- **M4 Artwork** — future spec.
- **M5 Duplicate Detection** — future spec (full hash, Chromaprint, frame
  fingerprinting).
- **M6 Collections + Playlists** — future spec.
- **M7 Web UI** — future spec (phase 2).

GitHub issues will be generated for M0/M1 work items only; M2+ appear as
milestone placeholders without detailed issues until they're specced.
