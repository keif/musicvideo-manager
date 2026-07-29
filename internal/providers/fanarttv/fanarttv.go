package fanarttv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

const (
	defaultBaseURL     = "https://webservice.fanart.tv/v3/music"
	defaultMinInterval = 500 * time.Millisecond
	providerName       = "fanarttv"
)

// Client fetches artist artwork from fanart.tv, keyed by MusicBrainz
// artist ID. Every response is cached via Catalog so a repeated query
// never hits the network twice, including "no artwork found" results.
type Client struct {
	Catalog     *catalog.Catalog
	APIKey      string
	HTTPClient  *http.Client
	BaseURL     string
	MinInterval time.Duration

	mu          sync.Mutex
	lastRequest time.Time
}

// New returns a Client configured for the real fanart.tv API.
func New(cat *catalog.Catalog, apiKey string) *Client {
	return &Client{
		Catalog:     cat,
		APIKey:      apiKey,
		HTTPClient:  http.DefaultClient,
		BaseURL:     defaultBaseURL,
		MinInterval: defaultMinInterval,
	}
}

func (c *Client) Name() string { return providerName }

// Artwork holds up to one URL per artwork type for a single artist.
type Artwork struct {
	ThumbURL      string
	BackgroundURL string
	LogoURL       string
	BannerURL     string
}

// ArtworkForArtist returns the available artwork URLs for the given
// MusicBrainz artist ID. An artist with no fanart.tv entry returns a
// zero-value Artwork and a nil error -- this is a normal outcome, not a
// failure, and is cached the same as a real result so it isn't re-queried.
func (c *Client) ArtworkForArtist(ctx context.Context, artistMBID string) (Artwork, error) {
	if cached, ok, err := c.Catalog.GetCachedResponse(providerName, artistMBID); err != nil {
		return Artwork{}, fmt.Errorf("check cache: %w", err)
	} else if ok {
		return parseArtistResponse([]byte(cached))
	}

	c.waitForRateLimit()

	body, notFound, err := c.fetch(ctx, artistMBID)
	if err != nil {
		return Artwork{}, fmt.Errorf("fanart.tv fetch: %w", err)
	}
	if notFound {
		body = []byte("{}")
	}

	if err := c.Catalog.SetCachedResponse(providerName, artistMBID, string(body), time.Now().Unix()); err != nil {
		return Artwork{}, fmt.Errorf("cache response: %w", err)
	}

	return parseArtistResponse(body)
}

func (c *Client) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lastRequest.IsZero() {
		if elapsed := time.Since(c.lastRequest); elapsed < c.MinInterval {
			time.Sleep(c.MinInterval - elapsed)
		}
	}
	c.lastRequest = time.Now()
}

func (c *Client) fetch(ctx context.Context, artistMBID string) (body []byte, notFound bool, err error) {
	u := c.BaseURL + "/" + url.PathEscape(artistMBID) + "?api_key=" + url.QueryEscape(c.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return body, false, nil
}

type artistResponse struct {
	ArtistThumb      []imageEntry `json:"artistthumb"`
	ArtistBackground []imageEntry `json:"artistbackground"`
	HDMusicLogo      []imageEntry `json:"hdmusiclogo"`
	MusicBanner      []imageEntry `json:"musicbanner"`
}

type imageEntry struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes string `json:"likes"`
}

func parseArtistResponse(body []byte) (Artwork, error) {
	var parsed artistResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Artwork{}, fmt.Errorf("parse fanart.tv response: %w", err)
	}

	art := Artwork{}
	if len(parsed.ArtistThumb) > 0 {
		art.ThumbURL = parsed.ArtistThumb[0].URL
	}
	if len(parsed.ArtistBackground) > 0 {
		art.BackgroundURL = parsed.ArtistBackground[0].URL
	}
	if len(parsed.HDMusicLogo) > 0 {
		art.LogoURL = parsed.HDMusicLogo[0].URL
	}
	if len(parsed.MusicBanner) > 0 {
		art.BannerURL = parsed.MusicBanner[0].URL
	}
	return art, nil
}
