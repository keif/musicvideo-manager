-- internal/catalog/migrations/0003_rename.sql
CREATE TABLE rename_runs (
    id            INTEGER PRIMARY KEY,
    started_at    INTEGER NOT NULL,
    finished_at   INTEGER,
    files_renamed INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'applied'
);

CREATE TABLE rename_history (
    id             INTEGER PRIMARY KEY,
    rename_run_id  INTEGER NOT NULL REFERENCES rename_runs(id),
    video_id       INTEGER NOT NULL REFERENCES videos(id),
    old_path       TEXT NOT NULL,
    new_path       TEXT NOT NULL
);
