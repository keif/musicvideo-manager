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

Currently implemented:

- `scan` — walks a library directory, extracts technical metadata via
  ffprobe, and populates the catalog with correct incremental rescan,
  move/rename detection, and missing-file handling.
- `match` — identifies catalogued videos against MusicBrainz.
- `rename` — renames catalogued videos into a templated layout, with
  `--dry-run` preview and `--undo`.
- `thumbnail` — generates local video thumbnails by extracting a frame
  with ffmpeg.
- `artwork` — downloads artist artwork from fanart.tv for confirmed matches.
- `nfo` — writes Kodi-format NFO files for confirmed matches.
- `duplicates` — finds byte-identical duplicate videos via full SHA-256
  hashing.

Not yet implemented (see the roadmap below): collections, playlists, web UI.

## Requirements

- Go 1.25+
- ffmpeg/ffprobe on your `PATH`

## Install

```bash
go build -o musicvideo-manager ./cmd/musicvideo-manager
```

## Configuration

Some commands call third-party metadata/artwork providers and need API
credentials. Credentials are read from a **user-level** `.env` file — not
from the library directory — since a library's per-library state (catalog,
backups, copies) travels with the library, but a credential shouldn't:

```bash
cp .env.example ~/.config/musicvideo-manager/.env
```

Then fill in:

| Variable | Used by | Required for | Get a key |
|---|---|---|---|
| `FANARTTV_API_KEY` | `artwork` | Downloading artist artwork from fanart.tv | https://fanart.tv/get-an-api-key/ |

Every other command (`scan`, `match`, `rename`, `thumbnail`, `nfo`,
`duplicates`) works with no configuration — `match` queries the public
MusicBrainz API, which doesn't require a key.

## Usage

```bash
./musicvideo-manager scan /path/to/your/music-videos
./musicvideo-manager scan /path/to/your/music-videos --json
./musicvideo-manager match /path/to/your/music-videos
./musicvideo-manager rename /path/to/your/music-videos --dry-run
./musicvideo-manager rename /path/to/your/music-videos
./musicvideo-manager thumbnail /path/to/your/music-videos
./musicvideo-manager artwork /path/to/your/music-videos
./musicvideo-manager nfo /path/to/your/music-videos
./musicvideo-manager duplicates /path/to/your/music-videos
./musicvideo-manager version
```

The first `scan` of a directory creates
`<library-root>/.musicvideo-manager/{config.yaml,catalog.sqlite}`. Re-running
`scan` is incremental: unchanged files are skipped, edited files are
re-probed, moved/renamed files are detected by content hash, and deleted
files are marked missing (never deleted from the catalog).

## Roadmap

| Milestone | Scope | Status |
|---|---|---|
| M0 Foundation | repo scaffold, catalog schema, provider interface, CLI skeleton, CI/Docker | done |
| M1 Scanner | `scan` command, ffprobe extraction, incremental rescan | done |
| M2 Metadata Matching | filename parsing, embedded tags, MusicBrainz matching | done |
| M3 Rename Engine + NFO | configurable rename templates, Kodi NFO export | done |
| M4 Artwork | fanart.tv artist artwork, local ffmpeg thumbnails | done |
| M5 Duplicate Detection | full SHA-256 hash duplicate detection | done |
| M6 Collections + Playlists | decade/genre/mood collections, M3U/JSON/smart playlists | not started |
| M7 Web UI | React search, metadata editor, artwork preview, batch editing | not started |

Each milestone gets its own design spec under `docs/superpowers/specs/`
before implementation.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — see [LICENSE](LICENSE).
