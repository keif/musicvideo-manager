# Metadata Matching (Local + MusicBrainz) — Design Spec

Status: Approved
Date: 2026-07-28
Scope: First slice of M2 (Metadata Matching). Filename parsing, the
provider response cache, confidence scoring, and a single real provider
implementation (MusicBrainz). Embedded-tag reading and the Discogs/
TheAudioDB/Last.fm providers are explicitly out of scope for this spec —
each becomes a small follow-on addition once this orchestration exists,
since they only need a new `pkg/provider.MetadataProvider` implementation,
not new plumbing.

## Context

Builds on the completed Foundation & Scanner milestone (M0/M1): a working
`scan` command populates a per-library SQLite catalog (`videos` table) from
a real 624-file library at `/Volumes/Yggdrasil/Music Videos`, already
organized as `Artist/Artist - Title[-suffix].ext`. This spec adds the first
identification pass: given a catalogued video, guess its artist and title
from the filename, confirm that guess against MusicBrainz, and record the
result with a confidence score.

`pkg/provider.MetadataProvider` (interface, no implementations) already
exists from the prior milestone — this is the first thing to implement
against it.

## Goals

- Parse artist/title guesses from filenames using the real library's
  naming conventions as ground truth.
- Cache every provider response in SQLite so repeated `match` runs never
  re-query the same input twice ("never repeatedly hit APIs" from the
  original project brief).
- Implement a MusicBrainz provider that respects MusicBrainz's usage
  policy (≤1 request/second, a descriptive `User-Agent` header).
- Score and persist every match attempt — confirmed, needs-review, or
  unmatched — so nothing is silently dropped and nothing gets treated as
  authoritative without clearing a confidence threshold.
- A `match` CLI command that runs this over a library, plus a read-only
  `--list-review` view of the low-confidence queue.

## Non-goals (deferred to future specs)

- Embedded tag reading (ffprobe container metadata as a second identity
  signal).
- Discogs, TheAudioDB, Last.fm provider implementations.
- Any command to accept/edit/override a `needs_review` match by hand (a
  future `match --accept` command or the eventual web UI).
- Cache invalidation/TTL for `provider_cache` — cached responses are kept
  forever in this pass (see "Known limitation" below).
- Rename engine, NFO/artwork generation, duplicate detection, collections,
  playlists — unchanged from the prior spec's roadmap.

## New packages

```
internal/match/
├── filename.go   # ParseFilename(filename, folder string) (artist, title string, confidence float64)
└── match.go      # Matcher{Catalog, Provider, ConfidenceThreshold}.Run(ctx, root, extensions) (Result, error)

internal/providers/
└── musicbrainz/
    └── musicbrainz.go   # implements pkg/provider.MetadataProvider; rate-limited, cached
```

`internal/providers/` is new: a home for each concrete provider as its own
subpackage. Adding Discogs/TheAudioDB/Last.fm later means a new subpackage
implementing `pkg/provider.MetadataProvider`, not touching `internal/match`
at all — this is the seam the prior milestone's provider interface was
built for.

## Catalog schema additions (migration `0002`)

```sql
CREATE TABLE artists (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    mbid       TEXT UNIQUE,          -- MusicBrainz artist ID, NULL until resolved
    created_at INTEGER NOT NULL
);

CREATE TABLE songs (
    id         INTEGER PRIMARY KEY,
    artist_id  INTEGER NOT NULL REFERENCES artists(id),
    title      TEXT NOT NULL,
    album      TEXT,
    year       INTEGER,
    mbid       TEXT UNIQUE,          -- MusicBrainz recording ID
    created_at INTEGER NOT NULL
);

CREATE TABLE matches (
    id             INTEGER PRIMARY KEY,
    video_id       INTEGER NOT NULL UNIQUE REFERENCES videos(id),
    song_id        INTEGER REFERENCES songs(id),   -- NULL if unmatched or needs_review
    provider       TEXT NOT NULL,                   -- 'musicbrainz', etc.
    confidence     REAL NOT NULL,
    status         TEXT NOT NULL,                   -- 'confirmed' | 'needs_review' | 'unmatched'
    parsed_artist  TEXT,
    parsed_title   TEXT,
    matched_at     INTEGER NOT NULL
);
CREATE INDEX idx_matches_status ON matches(status);

CREATE TABLE provider_cache (
    id            INTEGER PRIMARY KEY,
    provider      TEXT NOT NULL,
    cache_key     TEXT NOT NULL,      -- sha256 of the normalized query
    response_json TEXT NOT NULL,
    fetched_at    INTEGER NOT NULL,
    UNIQUE(provider, cache_key)
);
```

`matches.video_id` is `UNIQUE`: there is exactly one *current* match per
video. Re-running `match` upserts this row rather than accumulating a
history — the same "reconcile against current state" philosophy the
scanner already uses for `videos`. `song_id` is only set when
`status = 'confirmed'`; a `needs_review` row still records the best
candidate's `parsed_artist`/`parsed_title`/`confidence` for visibility, but
nothing downstream should treat it as authoritative.

This migration is purely additive (new `0002_*.sql` file under
`internal/catalog/migrations/`) — no existing table changes.

## Filename parsing

`ParseFilename(filename, folder string) (artist, title string, confidence float64)`
takes the base filename (extension already stripped by the caller) and the
video's parent folder name (the catalog stores paths relative to the
library root, e.g. `AC DC/AC DC - Back In Black-video.mp4` → folder
`AC DC`), and tries an ordered list of patterns against `filename`, first
match wins:

1. `Artist - Title (Official Video)` / `Artist - Title [videoid]` /
   `Artist - Title-video` — a single ` - ` delimiter splitting artist from
   title, with common suffixes (`Official Video`, `-video`, bracketed
   YouTube IDs) stripped from the title. Highest confidence.
2. Bare filename with no delimiter — `folder` is used as the artist, and
   the whole filename as a rough title guess. Medium confidence.
3. No usable signal at all (empty filename, and an empty or unusable
   folder) — returns an empty artist and title with confidence `0`. The
   caller treats confidence `0` as a short-circuit: no provider query is
   made, the video goes straight to `status = 'unmatched'`.

Real filenames from the library validate patterns 1 and 2 directly (e.g.
`50 Cent - In Da Club-video.mp4`, `AC DC - Back In Black-video.mp4`,
`a-ha - Take On Me-video.mp4`).

## MusicBrainz provider

`internal/providers/musicbrainz.MusicBrainzProvider` implements
`pkg/provider.MetadataProvider`:

```go
func (p *MusicBrainzProvider) Match(ctx context.Context, q provider.MatchQuery) ([]provider.MatchResult, error)
```

1. Computes `cacheKey = sha256(normalize(q.Artist) + "\x00" + normalize(q.Title))`
   and checks `provider_cache` for `(provider='musicbrainz', cache_key)`.
   A cache hit returns the parsed result immediately — no HTTP call, no
   rate limiting.
2. On a cache miss: waits on an internal rate limiter (minimum 1 second
   since this provider's last outbound request — the interval is a struct
   field, overridable in tests) before calling MusicBrainz's
   `/ws/2/recording` search endpoint with `q.Artist`/`q.Title`, sending
   `User-Agent: musicvideo-manager/<version> (https://github.com/keif/musicvideo-manager)`
   per MusicBrainz's API usage policy.
3. Caches the raw JSON response in `provider_cache` regardless of whether
   it contained results — an empty result is still worth remembering so
   the same empty query isn't repeated next run.
4. Parses the top-ranked candidate (MusicBrainz returns a ranked list with
   a `score` field, 0-100) into a `provider.MatchResult`, with
   `Confidence = score / 100.0`. Returns an empty slice if MusicBrainz
   returned zero candidates.

## Matcher orchestration

```go
type Matcher struct {
    Catalog             *catalog.Catalog
    Provider            provider.MetadataProvider
    ConfidenceThreshold float64
}

func (m *Matcher) Run(ctx context.Context, root string) (Result, error)
```

1. Query the catalog for active videos with no row in `matches` yet (a new
   `catalog.ListActiveUnmatched()` method — a join, not an in-Go filter).
2. For each: extract the base filename and parent folder from `video.Path`,
   call `ParseFilename`. If parse confidence is `0`, write a
   `matches` row with `status = 'unmatched'`, `song_id = NULL`, and move on
   — no provider call.
3. Otherwise build a `provider.MatchQuery{Filename, Artist, Title}` and
   call `m.Provider.Match(ctx, q)`. A provider error is recorded in the
   run's `Result.Errors` and the video is left with **no** `matches` row
   at all (not even `unmatched`) — so it's naturally retried on the next
   `match` run, the same way a scan error doesn't corrupt catalog state.
4. If the provider returns no candidates, write `status = 'unmatched'`.
   Otherwise take the top candidate: if `Confidence >= ConfidenceThreshold`,
   upsert `artists`/`songs` rows (matched by MusicBrainz ID if present,
   else by name) and write `matches` with `status = 'confirmed'` and
   `song_id` set. If below threshold, write `status = 'needs_review'` with
   `song_id = NULL`, keeping `parsed_artist`/`parsed_title`/`confidence`
   for visibility.

No worker pool: MusicBrainz's 1 req/sec limit means concurrency would only
serialize on the rate limiter. Videos are processed sequentially. For the
624-file library, a first full run is roughly 10 minutes of wall clock
(bounded by cache misses hitting the rate limiter); subsequent runs only
pay for genuinely new, still-unmatched videos.

## Config

`config.yaml` gains a `match` section, following the same shape as the
existing `scan` section:

```yaml
match:
  confidence_threshold: 0.7
```

## CLI surface (this phase)

```
musicvideo-manager match [path] [--json]           # match every unmatched active video
musicvideo-manager match --list-review [path]       # read-only: print the needs_review queue
```

`--list-review` prints video path, parsed artist/title guess, best
candidate, and confidence as a table (or JSON with `--json`). No editing —
accepting or overriding a review item is future work.

## Testing

- `ParseFilename`: table-driven tests against the three pattern tiers,
  using real filenames from the library plus edge cases (no delimiter,
  empty string, unicode artist names, folder-derived fallback).
- MusicBrainz provider: tested against an `httptest.Server` mock (no live
  network in CI) — cache hit skips the HTTP call entirely, the rate
  limiter enforces spacing (using an injectable interval so tests run in
  milliseconds), the `User-Agent` header is sent correctly, and
  MusicBrainz's JSON response shape parses correctly including a
  zero-results response.
- Matcher orchestration: a fake `MetadataProvider` (canned responses)
  drives tests for confirmed/needs_review/unmatched/error outcomes,
  verifies the resulting `artists`/`songs`/`matches` rows, and confirms an
  unparseable filename never calls the provider (assert call count is 0).
- Edge cases: empty library (zero unmatched videos, `Run` returns
  immediately), a video whose filename parses but MusicBrainz returns zero
  candidates, a provider error mid-run not aborting the rest of the batch.

## Known limitation

`provider_cache` has no TTL or invalidation in this pass — a cached
MusicBrainz response is kept forever. If MusicBrainz's data for a
recording changes, this won't be picked up without a manual cache-clear
(no command for that exists yet). This is a deliberate MVP deferral, not
an oversight — worth an ADR note alongside this feature's implementation.

## Roadmap (context, not detailed here)

- **This spec** — filename parsing, provider cache, confidence scoring,
  MusicBrainz provider, `match`/`match --list-review` CLI.
- **Future** — embedded tag reading as a second local signal; Discogs,
  TheAudioDB, Last.fm providers (each a new `internal/providers/*`
  subpackage); a review-resolution command; rename engine + NFO output;
  artwork; duplicate detection; collections + playlists; web UI.
