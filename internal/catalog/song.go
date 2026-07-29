package catalog

import "database/sql"

// Song is a single row in the songs table.
type Song struct {
	ID        int64
	ArtistID  int64
	Title     string
	Album     sql.NullString
	Year      sql.NullInt64
	MBID      sql.NullString
	CreatedAt int64
}

const songColumns = `id, artist_id, title, album, year, mbid, created_at`

func scanSong(row rowScanner) (*Song, error) {
	s := &Song{}
	if err := row.Scan(&s.ID, &s.ArtistID, &s.Title, &s.Album, &s.Year, &s.MBID, &s.CreatedAt); err != nil {
		return nil, err
	}
	return s, nil
}

// FindSongByMBID returns the song with the given MusicBrainz recording ID, if any.
func (c *Catalog) FindSongByMBID(mbid string) (*Song, bool, error) {
	row := c.db.QueryRow(`SELECT `+songColumns+` FROM songs WHERE mbid = ?`, mbid)
	s, err := scanSong(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}

// FindSongByArtistAndTitle returns the first song matching artistID and
// an exact title, if any.
func (c *Catalog) FindSongByArtistAndTitle(artistID int64, title string) (*Song, bool, error) {
	row := c.db.QueryRow(`SELECT `+songColumns+` FROM songs WHERE artist_id = ? AND title = ?`, artistID, title)
	s, err := scanSong(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}

// UpsertSong returns the ID of the song matching mbid (if non-empty) or
// (artistID, title), inserting a new row if neither matches an existing
// one. Existing rows are never overwritten by this call.
func (c *Catalog) UpsertSong(artistID int64, title, album string, year int, mbid string, at int64) (int64, error) {
	if mbid != "" {
		if existing, ok, err := c.FindSongByMBID(mbid); err != nil {
			return 0, err
		} else if ok {
			return existing.ID, nil
		}
	}
	if existing, ok, err := c.FindSongByArtistAndTitle(artistID, title); err != nil {
		return 0, err
	} else if ok {
		return existing.ID, nil
	}

	var mbidValue, albumValue sql.NullString
	var yearValue sql.NullInt64
	if mbid != "" {
		mbidValue = sql.NullString{String: mbid, Valid: true}
	}
	if album != "" {
		albumValue = sql.NullString{String: album, Valid: true}
	}
	if year != 0 {
		yearValue = sql.NullInt64{Int64: int64(year), Valid: true}
	}

	res, err := c.db.Exec(`INSERT INTO songs (artist_id, title, album, year, mbid, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		artistID, title, albumValue, yearValue, mbidValue, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
