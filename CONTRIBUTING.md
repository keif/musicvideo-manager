# Contributing

## Development setup

- Go 1.25+
- ffmpeg/ffprobe on your `PATH` (required both at runtime and by the test suite)
- Optional: `golangci-lint` for running `make lint` locally (CI runs it regardless)

```bash
git clone <repo-url>
cd musicvideo-manager
go build ./...
make test
```

## Running tests

```bash
make test          # go test ./... -race -cover
make lint           # golangci-lint run ./...
```

Tests that need a real video file generate one on the fly with `ffmpeg`
(see `requireFFmpeg`/`generateFixtureVideo` helpers in `internal/scanner`
and `internal/cli`) and skip themselves with a clear message if `ffmpeg`/
`ffprobe` aren't on your `PATH`. CI always has them installed.

## Code style

- `gofmt` (run automatically by most editors; CI's lint job also enforces it via `golangci-lint`)
- No comments explaining *what* code does — name things clearly instead.
  Comments are for non-obvious *why* (a workaround, an invariant, a
  constraint that would surprise a reader).
- Keep files focused: one clear responsibility per file. If a file is
  growing unwieldy, that's a signal to split it.

## Commits

Follow [Conventional Commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`). Keep commits
small and focused — one logical change per commit.

## Architecture changes

Non-trivial architecture decisions get an ADR under `docs/adr/`, following
the numbered format already there (`Status` / `Context` / `Decision` /
`Consequences`). New feature areas (metadata matching, rename engine,
artwork, etc.) get a design spec under `docs/superpowers/specs/` before
implementation — see the existing spec for the shape to follow.

## Pull requests

- Every PR should leave `make test` and `make lint` passing.
- New behavior needs tests covering the happy path and the relevant edge
  cases (NULL/empty/boundary values, malformed input) — see existing tests
  in `internal/catalog`, `internal/scanner`, and `internal/config` for the
  expected depth.
