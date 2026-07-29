package catalog

import "database/sql"

// MatchStatus is the outcome of a single match attempt for a video.
type MatchStatus string

const (
	MatchStatusConfirmed   MatchStatus = "confirmed"
	MatchStatusNeedsReview MatchStatus = "needs_review"
	MatchStatusUnmatched   MatchStatus = "unmatched"
)

// Match is a single row in the matches table: the current best
// identification attempt for one video.
type Match struct {
	ID           int64
	VideoID      int64
	SongID       sql.NullInt64
	Provider     string
	Confidence   float64
	Status       MatchStatus
	ParsedArtist sql.NullString
	ParsedTitle  sql.NullString
	MatchedAt    int64
}

// UpsertMatch inserts a new match row, or replaces the existing row for
// m.VideoID (matches.video_id is UNIQUE — there is exactly one current
// match per video).
func (c *Catalog) UpsertMatch(m *Match) error {
	_, err := c.db.Exec(`INSERT INTO matches (video_id, song_id, provider, confidence, status, parsed_artist, parsed_title, matched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(video_id) DO UPDATE SET
			song_id = excluded.song_id,
			provider = excluded.provider,
			confidence = excluded.confidence,
			status = excluded.status,
			parsed_artist = excluded.parsed_artist,
			parsed_title = excluded.parsed_title,
			matched_at = excluded.matched_at`,
		m.VideoID, m.SongID, m.Provider, m.Confidence, m.Status, m.ParsedArtist, m.ParsedTitle, m.MatchedAt)
	return err
}

// ListActiveUnmatched returns every active video with no row in matches yet.
func (c *Catalog) ListActiveUnmatched() ([]*Video, error) {
	rows, err := c.db.Query(`SELECT ` + videoColumns + ` FROM videos
		WHERE status = 'active' AND id NOT IN (SELECT video_id FROM matches)`)
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

// ReviewItem is a single needs_review match, joined with its video's path
// for display. Nullable fields are surfaced as plain strings (empty if
// NULL) since this is a read-only reporting view, not something composed
// with further NULL-sensitive logic.
type ReviewItem struct {
	VideoPath    string  `json:"video_path"`
	ParsedArtist string  `json:"parsed_artist"`
	ParsedTitle  string  `json:"parsed_title"`
	Confidence   float64 `json:"confidence"`
}

// ListNeedsReview returns every match with status = 'needs_review',
// ordered by video path.
func (c *Catalog) ListNeedsReview() ([]ReviewItem, error) {
	rows, err := c.db.Query(`SELECT v.path, m.parsed_artist, m.parsed_title, m.confidence
		FROM matches m
		JOIN videos v ON v.id = m.video_id
		WHERE m.status = 'needs_review'
		ORDER BY v.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReviewItem
	for rows.Next() {
		var item ReviewItem
		var parsedArtist, parsedTitle sql.NullString
		if err := rows.Scan(&item.VideoPath, &parsedArtist, &parsedTitle, &item.Confidence); err != nil {
			return nil, err
		}
		item.ParsedArtist = parsedArtist.String
		item.ParsedTitle = parsedTitle.String
		out = append(out, item)
	}
	return out, rows.Err()
}
