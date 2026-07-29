package catalog

import "database/sql"

// VideoStatus is the lifecycle state of a catalogued video.
type VideoStatus string

const (
	StatusActive  VideoStatus = "active"
	StatusMissing VideoStatus = "missing"
)

// Video is a single row in the videos table.
type Video struct {
	ID              int64
	Path            string
	FileSize        int64
	MTime           int64
	PartialHash     string
	Status          VideoStatus
	DurationSeconds sql.NullFloat64
	Width           sql.NullInt64
	Height          sql.NullInt64
	VideoCodec      sql.NullString
	AudioCodec      sql.NullString
	Container       sql.NullString
	Bitrate         sql.NullInt64
	FPS             sql.NullFloat64
	FirstSeenAt     int64
	LastSeenAt      int64
	LastScannedAt   int64
}

const videoColumns = `id, path, file_size, mtime, partial_hash, status,
	duration_seconds, width, height, video_codec, audio_codec, container, bitrate, fps,
	first_seen_at, last_seen_at, last_scanned_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVideo(row rowScanner) (*Video, error) {
	v := &Video{}
	err := row.Scan(&v.ID, &v.Path, &v.FileSize, &v.MTime, &v.PartialHash, &v.Status,
		&v.DurationSeconds, &v.Width, &v.Height, &v.VideoCodec, &v.AudioCodec, &v.Container, &v.Bitrate, &v.FPS,
		&v.FirstSeenAt, &v.LastSeenAt, &v.LastScannedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// ListActive returns every video row with status = 'active'.
func (c *Catalog) ListActive() ([]*Video, error) {
	rows, err := c.db.Query(`SELECT ` + videoColumns + ` FROM videos WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// FindMissingByHash returns videos with status = 'missing' whose stored
// file_size and partial_hash match, used for move/rename detection.
func (c *Catalog) FindMissingByHash(fileSize int64, hash string) ([]*Video, error) {
	rows, err := c.db.Query(`SELECT `+videoColumns+` FROM videos WHERE status = 'missing' AND file_size = ? AND partial_hash = ?`, fileSize, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InsertVideo inserts a new video row and returns its assigned ID.
func (c *Catalog) InsertVideo(v *Video) (int64, error) {
	res, err := c.db.Exec(`INSERT INTO videos (path, file_size, mtime, partial_hash, status,
		duration_seconds, width, height, video_codec, audio_codec, container, bitrate, fps,
		first_seen_at, last_seen_at, last_scanned_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Path, v.FileSize, v.MTime, v.PartialHash, v.Status,
		v.DurationSeconds, v.Width, v.Height, v.VideoCodec, v.AudioCodec, v.Container, v.Bitrate, v.FPS,
		v.FirstSeenAt, v.LastSeenAt, v.LastScannedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateProbe overwrites size/hash/status/metadata fields and timestamps
// for an existing row, keyed by v.ID. Used for the "changed" reconciliation case.
func (c *Catalog) UpdateProbe(v *Video) error {
	_, err := c.db.Exec(`UPDATE videos SET file_size=?, mtime=?, partial_hash=?, status=?,
		duration_seconds=?, width=?, height=?, video_codec=?, audio_codec=?, container=?, bitrate=?, fps=?,
		last_seen_at=?, last_scanned_at=? WHERE id=?`,
		v.FileSize, v.MTime, v.PartialHash, v.Status,
		v.DurationSeconds, v.Width, v.Height, v.VideoCodec, v.AudioCodec, v.Container, v.Bitrate, v.FPS,
		v.LastSeenAt, v.LastScannedAt, v.ID)
	return err
}

// UpdatePath moves an existing row to a new path and reactivates it.
// Used for the "moved/renamed" and "resurrected" reconciliation cases,
// where probe metadata is reused rather than recomputed.
func (c *Catalog) UpdatePath(id int64, newPath string, mtime int64, at int64) error {
	_, err := c.db.Exec(`UPDATE videos SET path=?, mtime=?, status='active', last_seen_at=?, last_scanned_at=? WHERE id=?`,
		newPath, mtime, at, at, id)
	return err
}

// TouchSeen marks a row as seen (and active) without changing its stored
// metadata. Used for the "unchanged" reconciliation case.
func (c *Catalog) TouchSeen(id int64, at int64) error {
	_, err := c.db.Exec(`UPDATE videos SET status = 'active', last_seen_at = ?, last_scanned_at = ? WHERE id = ?`, at, at, id)
	return err
}

// MarkMissing marks a row missing. Rows are never deleted by scanning.
func (c *Catalog) MarkMissing(id int64) error {
	_, err := c.db.Exec(`UPDATE videos SET status = 'missing' WHERE id = ?`, id)
	return err
}

// UpdateVideoPath updates a video's path only, used by the rename engine.
// Unlike UpdatePath (scan reconciliation), this does not touch status or
// last_seen_at/last_scanned_at -- a rename isn't "seeing" the file for the
// first time, just relocating it.
func (c *Catalog) UpdateVideoPath(id int64, newPath string) error {
	_, err := c.db.Exec(`UPDATE videos SET path = ? WHERE id = ?`, newPath, id)
	return err
}
