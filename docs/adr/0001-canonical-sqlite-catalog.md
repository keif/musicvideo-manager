# 1. Canonical SQLite catalog as source of truth

Status: Accepted

## Context

Most local media managers treat the filesystem (and sidecar NFO files) as
the source of truth, regenerating them on every change. That makes history,
provenance, and multi-target export (Plex today, Jellyfin/Kodi/Emby later)
hard: there's nowhere to record "we matched this against MusicBrainz with
0.62 confidence and a human accepted it" other than baking it into a
filename or an NFO field meant for a specific media server's schema.

## Decision

musicvideo-manager maintains its own SQLite catalog of every video, its
extracted metadata, match history, and artwork. Filenames, NFO files, and
any future per-media-server export are *generated from* the catalog, never
the other way around. The catalog is authoritative.

## Consequences

- Any media-server-specific export (Plex NFO, Jellyfin, Kodi, a static
  site) becomes a pure read + template step against the catalog, not a
  parser of scattered on-disk conventions.
- The catalog must capture enough state (paths, hashes, timestamps, scan
  history) to reconstruct "what do we know and how confident are we"
  without re-scanning or re-querying providers.
- Losing the catalog file loses matching/curation history even though the
  underlying video files are untouched — mitigated by [[0002]]'s decision
  to colocate the catalog with the library so normal backup of the drive
  covers it.
