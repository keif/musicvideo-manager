-- internal/catalog/migrations/0001_init.sql
CREATE TABLE videos (
    id                INTEGER PRIMARY KEY,
    path              TEXT NOT NULL UNIQUE,
    file_size         INTEGER NOT NULL,
    mtime             INTEGER NOT NULL,
    partial_hash      TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active',
    duration_seconds  REAL,
    width             INTEGER,
    height            INTEGER,
    video_codec       TEXT,
    audio_codec       TEXT,
    container         TEXT,
    bitrate           INTEGER,
    fps               REAL,
    first_seen_at     INTEGER NOT NULL,
    last_seen_at      INTEGER NOT NULL,
    last_scanned_at   INTEGER NOT NULL
);
CREATE INDEX idx_videos_status ON videos(status);
CREATE INDEX idx_videos_hash ON videos(partial_hash);

CREATE TABLE scan_runs (
    id             INTEGER PRIMARY KEY,
    started_at     INTEGER NOT NULL,
    finished_at    INTEGER,
    files_scanned  INTEGER NOT NULL DEFAULT 0,
    files_added    INTEGER NOT NULL DEFAULT 0,
    files_updated  INTEGER NOT NULL DEFAULT 0,
    files_moved    INTEGER NOT NULL DEFAULT 0,
    files_missing  INTEGER NOT NULL DEFAULT 0,
    errors_count   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE scan_errors (
    id           INTEGER PRIMARY KEY,
    scan_run_id  INTEGER NOT NULL REFERENCES scan_runs(id),
    path         TEXT NOT NULL,
    message      TEXT NOT NULL,
    occurred_at  INTEGER NOT NULL
);
