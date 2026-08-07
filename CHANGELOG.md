# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-08-07

First tagged release. `musicvideo-manager` can scan a local music video
library, identify videos against MusicBrainz, rename them into a
consistent layout, generate thumbnails and Kodi NFO files, fetch artist
artwork, and find exact duplicates.

### Added

- `scan` — walk a library directory, extract technical metadata via
  ffprobe, and populate a SQLite catalog with incremental rescan,
  move/rename detection (via partial-content hashing), and missing-file
  handling.
- `match` — identify catalogued videos against MusicBrainz, combining
  filename parsing and embedded-tag hints with a confidence threshold
  and a provider response cache to respect MusicBrainz rate limits.
- `rename` — render a configurable template into a normalized library
  layout, with `--dry-run` preview, conflict detection, path-traversal
  guards, and `--undo`.
- `nfo` — write Kodi-format NFO files for confirmed matches.
- `thumbnail` — generate local video thumbnails by extracting a frame
  with ffmpeg.
- `artwork` — download artist artwork from fanart.tv for confirmed
  matches, with API key credentials loaded from a user-level `.env`
  file so they don't travel with a shared/copied library.
- `duplicates` — find byte-identical duplicate videos via full SHA-256
  hashing.
- `version` — print the build version.
- CI: matrix test run (Ubuntu + macOS) with race detection and coverage,
  plus golangci-lint.
- Docker image and compose service for running the tool in a container.
- Architecture decision records under `docs/adr` covering the catalog
  model, MusicBrainz rate limiting, the rename engine's safety model,
  and fanart.tv credential handling.

[0.1.0]: https://github.com/keif/musicvideo-manager/releases/tag/v0.1.0
