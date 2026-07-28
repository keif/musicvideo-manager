# 3. Metadata provider plugin interface

Status: Accepted

## Context

The roadmap calls for multiple metadata sources (MusicBrainz, Discogs,
TheAudioDB, Last.fm) and, as a stretch goal, a plugin SDK for third-party
providers. None of these are implemented in the Foundation/Scanner phase,
but the interface they'll implement against is cheap to define now and
avoids an awkward later move of the type from an internal package to a
public one.

## Decision

`pkg/provider` defines `MetadataProvider` (`Name() string`,
`Match(ctx, MatchQuery) ([]MatchResult, error)`) and its supporting types.
It lives in `pkg/` (not `internal/`) because it's meant to be imported by
code outside this module eventually. No implementations exist yet.

## Consequences

- Future metadata-matching sub-projects implement this interface rather
  than inventing their own per-provider shape.
- Because `pkg/` is public, changes to this interface after providers exist
  are breaking changes for anyone who's implemented against it — worth
  getting the shape right before the first real provider lands, but not
  worth over-designing now with only a placeholder consumer.
