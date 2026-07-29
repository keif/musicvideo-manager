# Artist Artwork (fanart.tv) — Design Spec

Status: Approved
Date: 2026-07-29
Scope: M4 (Artwork), first slice. Downloading and caching artist-level
artwork (thumb, fanart background, HD logo, banner) from fanart.tv, keyed
by the MusicBrainz artist IDs already in the catalog. Local video-level
thumbnail generation (an ffmpeg frame grab, unrelated mechanism, no
external API) and any other artwork source are explicitly deferred to
future work.

## Context

Builds on all three completed milestones: Foundation/Scanner (the catalog
and `scan`), Metadata Matching (confirmed matches, artist/song records with
MusicBrainz IDs, the `provider_cache` table, and the MusicBrainz provider
pattern this spec reuses), and Rename Engine & Kodi NFO Export (the
`internal/nfo`-style independent-command pattern this spec follows, plus
the established convention of writing Kodi-recognizable sidecar files next
to library content).

All 167 distinct artists currently in the catalog already have a
MusicBrainz artist ID (100% coverage, from the Metadata Matching
milestone's `UpsertArtist`), which is exactly the key fanart.tv's music API
requires — no new identification step is needed before this feature can
run.

## Goals

- Fetch artist-level artwork (thumb, background/fanart, HD logo, banner)
  from fanart.tv for every distinct artist among confirmed matches.
- Cache the fanart.tv API's JSON response (image URLs) so the same artist
  is never re-queried once cached — reusing the existing `provider_cache`
  table and mechanism from the Metadata Matching milestone, not a new
  table.
- Avoid re-downloading image bytes that are already present on disk.
- Write downloaded images as Kodi-recognizable sidecar files in each
  artist's folder.
- Read the fanart.tv API key from an environment variable, never from
  `config.yaml` — consistent with the existing ADR 0002 decision that
  provider credentials never live in the per-library config.
- `artwork` CLI command, independent of `rename`/`nfo` — runnable anytime
  without re-scanning or re-matching.

## Non-goals (deferred to future work)

- Local video-level thumbnail generation (ffmpeg frame grab) — a separate
  sub-project; no external API, different mechanism entirely.
- Album-level artwork — the catalog's `songs.album` is a free-text field
  from MusicBrainz's first-returned release (already documented as noisy
  in the Rename Engine spec), not a normalized album entity with its own
  ID; album art is out of scope until that changes.
- Any other artwork source (Discogs, TheAudioDB, Last.fm images) — fanart.tv
  only, mirroring the "one provider first" pattern from Metadata Matching.
- Editing, replacing, or force-refreshing artwork that already exists on
  disk — this pass only ever fills gaps, never overwrites.
- Any UI/preview for reviewing downloaded artwork — CLI only, per the
  project's current phase.

## New packages

```
internal/providers/fanarttv/
└── fanarttv.go       # Client for fanart.tv's music-artist API

internal/artwork/
└── writer.go          # WriteAll: fetch + download + write artwork files
```

## API key handling

The fanart.tv API key is read from the `FANARTTV_API_KEY` environment
variable. If unset when the `artwork` command runs, it fails immediately
with a clear error — no silent skip, no attempt to run without
authentication. The key is never read from or written to `config.yaml`.

## fanart.tv client

```go
type Client struct {
    Catalog     *catalog.Catalog
    APIKey      string
    HTTPClient  *http.Client
    BaseURL     string        // overridable in tests
    MinInterval time.Duration // polite spacing between real requests; overridable in tests
}

func (c *Client) ArtworkForArtist(ctx context.Context, artistMBID string) (Artwork, error)
```

Same cache-check → rate-limit-wait → HTTP call → cache-store → parse shape
as the existing MusicBrainz provider (`internal/providers/musicbrainz`).
The JSON response is cached via the catalog's existing
`GetCachedResponse`/`SetCachedResponse` methods with `provider="fanarttv"`
and `cache_key=artistMBID` — no new cache table.

`MinInterval` defaults to a conservative, polite spacing between requests.
Unlike MusicBrainz's well-documented 1 req/sec policy, this spec does not
assert a specific published fanart.tv rate limit as fact — the exact
current number should be confirmed against fanart.tv's real documentation
during implementation, and the default adjusted if needed. The important
architectural property (an overridable field, defaulting to something
conservative, never zero/unlimited) is what this spec commits to.

An artist with no fanart.tv entry is a normal outcome: `ArtworkForArtist`
returns an empty `Artwork{}` with `nil` error, not a failure — mirroring
how the MusicBrainz provider returns zero candidates rather than erroring
when nothing matches.

`Artwork` holds up to one URL per type:

```go
type Artwork struct {
    ThumbURL      string
    BackgroundURL string
    LogoURL       string
    BannerURL     string
}
```

## Writer orchestration

```go
func WriteAll(cat *catalog.Catalog, client *fanarttv.Client, root string) (Result, error)
```

1. Query distinct artists among confirmed matches via a new catalog
   method, `ListConfirmedArtists() ([]ConfirmedArtist, error)`, returning
   `ConfirmedArtist{Name, MBID, FolderPath string}` — one row per distinct
   `(artist name, MBID, folder)` combination, mirroring how `nfo`'s
   `artist.nfo` deduplicates per folder rather than per video.
2. Skip (not an error) any artist with an empty MBID.
3. Call `client.ArtworkForArtist(ctx, artist.MBID)`. On error, record it in
   the run's error list and continue to the next artist — the established
   error-tolerance pattern from every prior command (`scan`, `match`,
   the rename engine's `Apply`).
4. For each of the four artwork types present in the response
   (`ThumbURL`/`BackgroundURL`/`LogoURL`/`BannerURL`, any may be empty),
   compute the target path (`folder.jpg`, `fanart.jpg`, `clearlogo.png`,
   `banner.jpg` in the artist's folder) and download+write it **only if
   that file does not already exist**. An existing file is skipped, not
   overwritten — this pass only fills gaps.

`Result` reports counts: artists processed, images written per type,
images skipped (already present), artists with no fanart.tv entry,
errors.

## CLI surface

```
musicvideo-manager artwork [path] [--json]
```

Same "not yet scanned" guard, config/catalog-open sequence, and JSON/text
output shape as `nfo`/`rename`. No new config section is needed beyond
what already exists (no user-configurable artwork settings in this pass —
the four types and their filenames are fixed).

## Testing

- fanart.tv client: `httptest.Server`-mocked (no live network in CI),
  covering cache hit/miss, the rate-limit spacing (injectable interval,
  same pattern as the MusicBrainz provider's tests), an artist with no
  entry (empty `Artwork{}`, no error), and a non-200 response.
- Writer: temp-directory fixtures with a fake `Client`-shaped dependency
  (or the real `Client` pointed at a test server) — covering writing all
  four types, skipping a type that's already present on disk, skipping an
  artist with no MBID, and continuing the batch after one artist's fetch
  errors.
- Real verification: once you have a `FANARTTV_API_KEY`, this milestone's
  closing task runs `artwork` for real against the actual library (safe to
  do for real, unlike the rename engine — this only ever adds new files,
  never moves or deletes anything) and reports what was actually
  downloaded, with a manual spot-check that a few images look correct.

## Roadmap (context, not detailed here)

- **This spec** — fanart.tv artist artwork (thumb/fanart/logo/banner).
- **Future** — local video thumbnail generation (ffmpeg frame grab);
  additional artwork sources; duplicate detection (M5); collections +
  playlists (M6); web UI (M7).
