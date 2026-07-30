# Local Video Thumbnails — Design Spec

Status: Approved
Date: 2026-07-30
Scope: M4 (Artwork), second slice. Generating a local thumbnail image for
each active video in the catalog by extracting a frame with `ffmpeg`. The
first M4 slice (artist artwork from fanart.tv — thumb/fanart/logo/banner)
is already implemented and merged; this slice is a separate, unrelated
mechanism (no external API, no metadata match required) and was explicitly
deferred out of that work.

## Context

Builds on Foundation/Scanner (the catalog, `scan`, and `scanner.Probe`'s
existing `ffprobe`-via-`exec.Command` pattern for extracting video
metadata — the same external-binary-shelling approach this feature reuses
for `ffmpeg`) and the fanart.tv artwork milestone (the `internal/artwork`
package's `WriteAll`/`Result` shape and error-tolerance pattern, which this
feature's `internal/thumbnail` package mirrors).

`ffmpeg`/`ffprobe` are already a stated prerequisite (README: "ffmpeg/ffprobe
on your PATH") and already required for `scan` to populate
`videos.duration_seconds`, which this feature reads directly rather than
re-probing.

Unlike the fanart.tv artwork feature, this slice needs no metadata match,
no external API, no API key, and no response caching — it operates
entirely on video files and catalog data that already exist locally.

## Goals

- Generate a local thumbnail image for every active video in the catalog,
  regardless of match status.
- Extract the frame via `ffmpeg`, seeking to a configurable percentage
  into the video's duration (already cataloged by `scan`, not re-probed).
- Write the thumbnail using Kodi's local-thumbnail naming convention (same
  base filename as the video, `.jpg` extension) so it's auto-detected with
  no NFO reference needed.
- Never overwrite an existing thumbnail file — this pass only fills gaps,
  consistent with the fanart.tv artwork feature's policy.
- `thumbnail` CLI command, independent of `scan`/`match`/`rename`/`nfo`/`artwork`
  — runnable anytime once the library has been scanned.

## Non-goals (deferred to future work)

- Artist-level artwork (poster/fanart/logo/banner) — already covered by
  the fanart.tv artwork milestone; unrelated mechanism.
- Smart/scene-aware frame selection (e.g. ffmpeg's `thumbnail` video
  filter, which samples multiple candidate frames) — a fixed-percentage
  seek was explicitly chosen over this for simplicity and speed.
- Multiple thumbnail candidates or a review/picker UI — one thumbnail per
  video, no review step, consistent with the project's current CLI-only
  phase.
- Force-regenerating or replacing an existing thumbnail — this pass only
  fills gaps, never overwrites.
- Any Go-native (cgo or pure-Go) video decoding — `ffmpeg` is already a
  required external dependency; shelling out to it (as `scanner.Probe`
  already does for `ffprobe`) is far simpler than introducing a decoding
  library, and ADR 0005 already established a project-wide preference for
  avoiding cgo.

## New package

```
internal/thumbnail/
└── writer.go       # WriteAll: compute target path + seek time, extract via ffmpeg, write only if missing
```

## Config

A new `thumbnail` section is added to `config.yaml`, mirroring the shape
of the existing `rename` section:

```yaml
thumbnail:
    percent: 10
```

`Percent` (int, 1-99) is how far into the video's duration to seek before
grabbing a frame. Defaults to `10` for newly discovered libraries via
`config.Default`. As with every other config addition in this project's
history, a library's `config.yaml` that predates this feature will not
have this section auto-backfilled (`config.Load`'s established
non-backfilling behavior) — an existing library must have this section
added manually (or reset to defaults) before running `thumbnail`, exactly
as happened for the `match` and `rename` sections previously.

## Writer orchestration

```go
func WriteAll(ctx context.Context, cat *catalog.Catalog, root string, percent int) (Result, error)
```

1. Query all active videos via the existing `(*Catalog) ListActive() ([]*Video, error)`
   — no match-status gate, no new catalog query needed.
2. For each video, compute the target path: `video.Path` is stored
   relative to the library root (same convention as the artwork writer's
   `ConfirmedArtistVideo.VideoPath`), so join it against `root`, then
   replace the file extension with `.jpg` — same directory as the video.
3. If a file already exists at the target path, skip it (never overwrite).
4. `Video.DurationSeconds` is `sql.NullFloat64` (populated by `scan`'s
   `ffprobe` pass). If it is not valid, or its `Float64` value is `<= 0`,
   skip the video and record the reason — a percentage-based seek cannot
   be safely computed without a real duration.
5. Otherwise, compute the seek timestamp (`DurationSeconds * percent / 100`)
   and run `ffmpeg -y -ss <timestamp> -i <video> -vframes 1 -q:v 2 <target>`
   — a fast, keyframe-based seek performed before `-i` (not a
   frame-accurate decode-from-start seek), since a representative frame is
   sufficient and this is materially faster for long videos.
6. On an `ffmpeg` failure (corrupt file, undecodable codec, etc.), record
   it in the run's error list and continue to the next video — the
   established error-tolerance pattern from every prior command.

`Result` reports counts: videos processed, thumbnails written, skipped
(already present), skipped (no duration), errors.

```go
type Result struct {
    VideosProcessed   int
    ThumbnailsWritten int
    ImagesSkipped     int
    NoDuration        int
    Errors            []ThumbnailErrorEntry
}

type ThumbnailErrorEntry struct {
    VideoPath string
    Message   string
}
```

## CLI surface

```
musicvideo-manager thumbnail [path] [--json]
```

Same "not yet scanned" guard, config/catalog-open sequence, and JSON/text
output shape as `nfo`/`artwork`. Unlike `artwork`, there is no API key to
check — this command can run immediately after `scan` completes. The
`percent` value comes from `config.yaml`'s new `thumbnail.percent` field
(no CLI flag in this pass — consistent with how `rename`'s template isn't
overridable per-invocation either).

## Testing

- Writer: real, short synthetic videos generated via `ffmpeg`'s `lavfi`
  test-source filters (mirroring `internal/scanner/fixture_test.go`'s
  `generateFixtureVideo` helper — duplicated locally in
  `internal/thumbnail`, since Go test helpers aren't shared across
  packages), covering: a real extraction produces a valid, non-empty JPEG
  at the exact expected path; an existing thumbnail is never overwritten;
  a video with zero/missing duration is skipped and counted, not errored;
  a corrupt/undecodable video is caught, recorded in `Result.Errors`, and
  the run continues to the next video.
- A `requireFFmpeg(t)` skip-helper (also mirrored from the scanner
  package) skips tests cleanly, not fails, when `ffmpeg`/`ffprobe` aren't
  installed.
- No network involved anywhere in this feature — no httptest servers
  needed, the simplest testing story of any milestone so far.
- Real verification: once implemented, this milestone's closing task runs
  `thumbnail` for real against the actual library (safe to do for real,
  same as `artwork` and `nfo` — this only ever adds new files, never
  moves or deletes anything) and reports what was actually generated,
  with a manual spot-check that a few thumbnails look like real,
  reasonable frames (not black/blank).

## Roadmap (context, not detailed here)

- **Fanart.tv artist artwork** — already implemented (poster/fanart/logo/banner).
- **This spec** — local video thumbnails (ffmpeg frame grab).
- **Future** — duplicate detection (M5); collections + playlists (M6); web UI (M7).
