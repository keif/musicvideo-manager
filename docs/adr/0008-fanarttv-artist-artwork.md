# 8. fanart.tv artist artwork: cache reuse, skip-if-exists, and API key handling

Status: Accepted

## Context

This is the project's first artwork-downloading feature. Two prior
milestones already established patterns this one reuses directly: the
Metadata Matching milestone's `provider_cache` table (any provider's JSON
response, keyed by provider name + cache key) and its MusicBrainz client
shape (cache-check -> rate-limit -> fetch -> cache-store), and the Rename
Engine milestone's precedent of never storing provider credentials in the
per-library `config.yaml` (ADR 0002).

fanart.tv's music-artist API is keyed by MusicBrainz artist ID, which
every artist in the catalog already carries (100% coverage as of this
milestone, since `UpsertArtist` from Metadata Matching prefers MBID). This
made fanart.tv a natural first artwork source, needing no new
identification step.

Unlike MusicBrainz, fanart.tv does not publish a strict numeric rate limit
(it's soft, per-key, and rarely enforced in practice) and requires a
registered personal API key rather than anonymous access.

## Decision

The fanart.tv client (`internal/providers/fanarttv`) reuses the existing
`provider_cache` table and `GetCachedResponse`/`SetCachedResponse` catalog
methods rather than introducing a new cache table -- a fanart.tv JSON
response is architecturally the same shape of thing MusicBrainz's is (a
cacheable provider response keyed by provider name + key), just for a
different provider. Both real responses and "artist has no fanart.tv
entry" (HTTP 404) are cached, so a negative result isn't re-queried either.

The client's `MinInterval` field defaults to a conservative, non-zero
value as polite default behavior, not because a specific published fanart.tv
limit is being complied with -- the exact number is left as an
easily-adjustable default rather than asserted as fact.

The API key is read from the `FANARTTV_API_KEY` environment variable only,
never from `config.yaml`, extending ADR 0002's reasoning: a library's
config travels with the library (colocated in `.musicvideo-manager/`,
possibly backed up or shared), and a provider credential should not travel
with it.

Downloaded image bytes are cached implicitly by their presence on disk --
the writer checks `os.Stat` before downloading and skips if the target
file already exists, never overwriting. This differs from the NFO writer's
always-overwrite behavior: regenerating a small XML file is cheap, but
re-downloading a multi-megabyte image on every run is real, avoidable cost
and bandwidth.

## Consequences

- Running `artwork` before the real fanart.tv rate limit (if one is ever
  encountered) is understood requires checking a live `Retry-After`
  response, which this pass does not implement -- a 429 response is
  currently treated as a generic non-200 error rather than automatically
  retried. Acceptable for a first pass; worth revisiting if real usage
  ever hits it.
- Because artwork files are only ever filled in when missing, there is no
  way in this pass to force a refresh of artwork that later turns out to
  be wrong or outdated short of manually deleting the file first. A future
  `--force` flag is straightforward to add if that need arises.
- Artwork is fetched once per distinct artist *folder*, not per artist
  *name* -- consistent with how `nfo`'s `artist.nfo` is deduplicated, and
  correct because Kodi/Jellyfin/Emby read these files from the folder a
  video lives in, not from a name string.
