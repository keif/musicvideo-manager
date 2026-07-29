# 6. MusicBrainz provider: rate limiting, response caching, and artist/recording ID separation

Status: Accepted

## Context

MusicBrainz is the first real implementation of the `pkg/provider.MetadataProvider`
interface (previously just an interface with a fake test double). Its API
usage policy requires at most one request per second (unauthenticated) and
a descriptive `User-Agent` header identifying the calling application —
violating either can get an IP rate-limited or blocked. Separately, the
project's own stated goal is to "never repeatedly hit APIs": every query
should be cacheable so re-running `match` costs nothing once a video has
been looked up.

While implementing the provider, `MatchResult.SourceID` (built in the
prior milestone as a single generic provider ID field, documented as
"MBID, Discogs ID, etc.") turned out to be ambiguous: a MusicBrainz
recording search result carries both a recording MBID and an artist MBID,
and the catalog schema needs to store both (`songs.mbid` and
`artists.mbid`) to avoid creating duplicate artist rows across different
songs by the same artist.

## Decision

`MusicBrainzProvider` enforces the rate limit itself via an internal
mutex-guarded "time since last request" check before every outbound call,
overridable in tests via a `MinInterval` field so the test suite runs in
milliseconds rather than real seconds. Every response — including empty
results — is cached in the catalog's `provider_cache` table, keyed by a
hash of the normalized (artist, title) query, before parsing; a cache hit
skips both the network call and the rate limiter entirely.

`pkg/provider.MatchResult` gained a second ID field, `ArtistSourceID`,
alongside the existing `SourceID` (which now explicitly represents the
matched song/recording). The Matcher upserts artists by `ArtistSourceID`
first (falling back to name if empty) and songs by `SourceID` (falling
back to artist+title if empty), so the same artist across multiple songs
resolves to one `artists` row rather than one per song.

## Consequences

- Future providers (Discogs, TheAudioDB, Last.fm) inherit the same
  `ArtistSourceID`/`SourceID` split for free — no further interface
  changes needed for that dimension.
- `provider_cache` has no TTL or invalidation in this phase: a cached
  MusicBrainz response is kept forever, even if the underlying MusicBrainz
  data later changes. This is a deliberate MVP deferral — clearing or
  refreshing the cache requires manual SQL against `catalog.sqlite` for
  now, since no CLI command for it exists yet. Worth a `doctor`-style
  command in a later milestone.
- The rate limiter is per-provider-instance, not global or persisted
  across process runs. Two concurrent `match` invocations against the same
  library would each maintain their own independent rate limit and could
  jointly exceed MusicBrainz's 1 req/sec policy. Not a concern today since
  nothing in this project runs `match` concurrently against the same
  library, but worth remembering if that ever changes.
