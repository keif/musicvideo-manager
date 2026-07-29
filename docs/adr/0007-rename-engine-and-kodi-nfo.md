# 7. Rename engine safety model and Kodi NFO format over Plex's movie.nfo workaround

Status: Accepted

## Context

The rename engine is the project's first filesystem-mutating feature — every
prior command (`scan`, `match`) only ever wrote to the catalog. Two things
needed a decision before writing any code: how much safety machinery a
file-moving feature actually needs, and which NFO schema to target first,
given the original project brief said "support Plex first" but Plex has no
purpose-built music-video NFO format (its only path is a legacy Movies
agent reading `movie.nfo`, treating each video as a standalone "movie").

A real-data finding also shaped the rename template: MusicBrainz's returned
`album` field for a confirmed match is whatever release happens to be first
in its API response, not necessarily the canonical studio album (e.g. Alice
Cooper's "Poison" resolved to a 2009 live-compilation release title).

## Decision

**Rename safety**: every applied rename is recorded (`rename_runs` +
`rename_history`), never deleted — only marked `'undone'` after a
successful `rename --undo`, which reverses the most recent applied run in
full. Collisions (two videos computing the same target path) are detected
before any file is touched and excluded from application entirely rather
than auto-resolved or silently overwritten. The default rename template
(`{artist}/{title}`) avoids the noisy `{album}` field.

**NFO format**: target Kodi's native `<musicvideo>`/`<artist>` XML schema
first, not Plex's `movie.nfo` workaround. Kodi's schema is purpose-built for
this content type, and both Jellyfin and Emby's music-video libraries also
read Kodi-format NFO — one implementation reaches three media servers.
Plex's `movie.nfo` support becomes a distinct follow-on, since it needs a
different schema (`<movie>` root, fields like `plot`/`genre` that don't map
cleanly from what the catalog stores) and a different conceptual framing
(each video as a "movie," not a "music video").

## Consequences

- A user on Plex today gets no NFO benefit from this milestone — only
  Kodi/Jellyfin/Emby read what's generated. This is a deliberate sequencing
  choice, not an oversight; Plex support is real future work, not deferred
  indefinitely.
- Undo only ever reverses the single most recent applied run, not an
  arbitrary past one. Simple and matches the realistic "oops, bad template"
  recovery scenario; a run-history browser is possible future work if a
  more granular need emerges.
- Rename collisions are a known, current risk (duplicate files like
  `In Da Club-video.mp4`/`In Da Club-video.ts` in the real library can both
  match to the same song) with no automatic resolution yet — proper
  resolution is M5 (Duplicate Detection)'s job, once the tooling to tell
  which of two files is the one to keep actually exists.
- NFO generation runs independently of rename and is always safe to
  re-run — a future rematch improving a title doesn't require re-renaming
  to get updated NFOs, just `nfo` again.
