-- internal/catalog/migrations/0002_matching.sql
CREATE TABLE artists (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    mbid       TEXT UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE songs (
    id         INTEGER PRIMARY KEY,
    artist_id  INTEGER NOT NULL REFERENCES artists(id),
    title      TEXT NOT NULL,
    album      TEXT,
    year       INTEGER,
    mbid       TEXT UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE matches (
    id             INTEGER PRIMARY KEY,
    video_id       INTEGER NOT NULL UNIQUE REFERENCES videos(id),
    song_id        INTEGER REFERENCES songs(id),
    provider       TEXT NOT NULL,
    confidence     REAL NOT NULL,
    status         TEXT NOT NULL,
    parsed_artist  TEXT,
    parsed_title   TEXT,
    matched_at     INTEGER NOT NULL
);
CREATE INDEX idx_matches_status ON matches(status);

CREATE TABLE provider_cache (
    id            INTEGER PRIMARY KEY,
    provider      TEXT NOT NULL,
    cache_key     TEXT NOT NULL,
    response_json TEXT NOT NULL,
    fetched_at    INTEGER NOT NULL,
    UNIQUE(provider, cache_key)
);
