package catalog

import "database/sql"

// Artist is a single row in the artists table.
type Artist struct {
	ID        int64
	Name      string
	MBID      sql.NullString
	CreatedAt int64
}

const artistColumns = `id, name, mbid, created_at`

func scanArtist(row rowScanner) (*Artist, error) {
	a := &Artist{}
	if err := row.Scan(&a.ID, &a.Name, &a.MBID, &a.CreatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

// FindArtistByMBID returns the artist with the given MusicBrainz ID, if any.
func (c *Catalog) FindArtistByMBID(mbid string) (*Artist, bool, error) {
	row := c.db.QueryRow(`SELECT `+artistColumns+` FROM artists WHERE mbid = ?`, mbid)
	a, err := scanArtist(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return a, true, nil
}

// FindArtistByName returns the first artist with the given exact name, if any.
func (c *Catalog) FindArtistByName(name string) (*Artist, bool, error) {
	row := c.db.QueryRow(`SELECT `+artistColumns+` FROM artists WHERE name = ?`, name)
	a, err := scanArtist(row)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return a, true, nil
}

// UpsertArtist returns the ID of the artist matching mbid (if non-empty) or
// name, inserting a new row if neither matches an existing one. Existing
// rows are never overwritten by this call.
func (c *Catalog) UpsertArtist(name, mbid string, at int64) (int64, error) {
	if mbid != "" {
		if existing, ok, err := c.FindArtistByMBID(mbid); err != nil {
			return 0, err
		} else if ok {
			return existing.ID, nil
		}
	}
	if existing, ok, err := c.FindArtistByName(name); err != nil {
		return 0, err
	} else if ok {
		return existing.ID, nil
	}

	var mbidValue sql.NullString
	if mbid != "" {
		mbidValue = sql.NullString{String: mbid, Valid: true}
	}
	res, err := c.db.Exec(`INSERT INTO artists (name, mbid, created_at) VALUES (?, ?, ?)`, name, mbidValue, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
