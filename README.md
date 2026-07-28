# musicvideo-manager

An open-source, media-server-agnostic tool for organizing, enriching, and
managing a local music video collection. Point it at a directory of music
videos and it builds a searchable, self-contained catalog of what you have
— then exports metadata for Plex today, with Jellyfin/Emby/Kodi and static
site export planned.

The catalog (not the filesystem) is the source of truth: every video, its
extracted technical metadata, match history, and artwork lives in a local
SQLite database that travels with the library. Filenames and NFO files are
generated *from* the catalog, not the other way around. See
[docs/adr](docs/adr) for the reasoning behind that and other core decisions.

## Status

Early foundation stage. Currently implemented:

- `scan` — walks a library directory, extracts technical metadata via
  ffprobe, and populates the catalog with correct incremental rescan,
  move/rename detection, and missing-file handling.

Not yet implemented (see the roadmap below): metadata matching/identification,
rename engine, NFO/artwork generation, duplicate detection, collections,
playlists, web UI.

## Requirements

- Go 1.25+
- ffmpeg/ffprobe on your `PATH`

## Install

```bash
go build -o musicvideo-manager ./cmd/musicvideo-manager
```

## Usage

```bash
./musicvideo-manager scan /path/to/your/music-videos
./musicvideo-manager scan /path/to/your/music-videos --json
./musicvideo-manager version
```

The first `scan` of a directory creates
`<library-root>/.musicvideo-manager/{config.yaml,catalog.sqlite}`. Re-running
`scan` is incremental: unchanged files are skipped, edited files are
re-probed, moved/renamed files are detected by content hash, and deleted
files are marked missing (never deleted from the catalog).

## Roadmap

| Milestone | Scope |
|---|---|
| M0 Foundation | repo scaffold, catalog schema, provider interface, CLI skeleton, CI/Docker |
| M1 Scanner | `scan` command, ffprobe extraction, incremental rescan |
| M2 Metadata Matching | filename parsing, embedded tags, MusicBrainz/Discogs/TheAudioDB/Last.fm |
| M3 Rename Engine + NFO | configurable rename templates, Plex/Kodi/Jellyfin/Emby NFO export |
| M4 Artwork | poster/fanart/logo/banner/thumbnail download and caching |
| M5 Duplicate Detection | full hash, Chromaprint audio fingerprinting, frame fingerprinting |
| M6 Collections + Playlists | decade/genre/mood collections, M3U/JSON/smart playlists |
| M7 Web UI | React search, metadata editor, artwork preview, batch editing |

Each milestone gets its own design spec under `docs/superpowers/specs/`
before implementation.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — see [LICENSE](LICENSE).
