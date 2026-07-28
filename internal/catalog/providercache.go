package catalog

import "database/sql"

// GetCachedResponse returns a previously cached provider response, if one exists.
func (c *Catalog) GetCachedResponse(providerName, cacheKey string) (string, bool, error) {
	var responseJSON string
	err := c.db.QueryRow(`SELECT response_json FROM provider_cache WHERE provider = ? AND cache_key = ?`,
		providerName, cacheKey).Scan(&responseJSON)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return responseJSON, true, nil
}

// SetCachedResponse stores (or overwrites) a provider response.
// provider_cache entries never expire in this phase — see ADR 0006.
func (c *Catalog) SetCachedResponse(providerName, cacheKey, responseJSON string, fetchedAt int64) error {
	_, err := c.db.Exec(`INSERT INTO provider_cache (provider, cache_key, response_json, fetched_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, cache_key) DO UPDATE SET
			response_json = excluded.response_json,
			fetched_at = excluded.fetched_at`,
		providerName, cacheKey, responseJSON, fetchedAt)
	return err
}
