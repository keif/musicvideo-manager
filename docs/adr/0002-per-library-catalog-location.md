# 2. Per-library catalog and config colocated in the library root

Status: Accepted

## Context

The catalog ([[0001]]) needs to live somewhere. A central per-machine
app-data directory (e.g. `~/.local/share/musicvideo-manager/`) is the more
common convention, but this tool's libraries are large media collections
that get moved between machines, external drives, and servers far more
often than typical application state does.

## Decision

Each library's catalog and config live at
`<library-root>/.musicvideo-manager/{catalog.sqlite,config.yaml}`, not in a
central app-data directory. A library is fully self-contained: moving or
copying the library root moves the catalog and config with it.

## Consequences

- No cross-library registry or "which machine has which library" tracking
  is needed for the MVP — multiple libraries are just multiple independent
  `.musicvideo-manager/` directories.
- Provider API keys/credentials must NOT live in this directory, since it's
  expected to be backed up, copied, and potentially shared — they come from
  environment variables or a separate machine-local credentials file.
- Tools that back up or sync the media library automatically back up the
  catalog too, with no extra configuration.
