package artwork

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/fanarttv"
)

// ArtworkErrorEntry is a single artist whose artwork fetch or a specific
// image download failed. Not persisted -- recorded in the run's Result only.
type ArtworkErrorEntry struct {
	Artist  string `json:"artist"`
	Message string `json:"message"`
}

// Result summarizes a WriteAll run.
type Result struct {
	ArtistsProcessed int                 `json:"artists_processed"`
	ImagesWritten    int                 `json:"images_written"`
	ImagesSkipped    int                 `json:"images_skipped"`
	NoArtwork        int                 `json:"no_artwork"`
	Errors           []ArtworkErrorEntry `json:"errors"`
}

// WriteAll fetches and writes fanart.tv artwork for every distinct artist
// folder among confirmed matches, rooted at root. Existing image files are
// never overwritten -- this only ever fills gaps.
func WriteAll(ctx context.Context, cat *catalog.Catalog, client *fanarttv.Client, root string) (Result, error) {
	videos, err := cat.ListConfirmedArtistVideos()
	if err != nil {
		return Result{}, fmt.Errorf("list confirmed artist videos: %w", err)
	}

	result := Result{Errors: []ArtworkErrorEntry{}}
	processedDirs := map[string]bool{}

	for _, v := range videos {
		artistDir := filepath.Dir(filepath.Join(root, filepath.FromSlash(v.VideoPath)))
		if processedDirs[artistDir] {
			continue
		}
		processedDirs[artistDir] = true

		if v.ArtistMBID == "" {
			continue
		}

		result.ArtistsProcessed++

		art, err := client.ArtworkForArtist(ctx, v.ArtistMBID)
		if err != nil {
			result.Errors = append(result.Errors, ArtworkErrorEntry{Artist: v.Artist, Message: err.Error()})
			continue
		}

		if art.ThumbURL == "" && art.BackgroundURL == "" && art.LogoURL == "" && art.BannerURL == "" {
			result.NoArtwork++
			continue
		}

		targets := []struct {
			url      string
			filename string
		}{
			{art.ThumbURL, "folder.jpg"},
			{art.BackgroundURL, "fanart.jpg"},
			{art.LogoURL, "clearlogo.png"},
			{art.BannerURL, "banner.jpg"},
		}

		for _, t := range targets {
			if t.url == "" {
				continue
			}
			targetPath := filepath.Join(artistDir, t.filename)
			if _, err := os.Stat(targetPath); err == nil {
				result.ImagesSkipped++
				continue
			}

			if err := downloadFile(ctx, client.HTTPClient, t.url, targetPath); err != nil {
				result.Errors = append(result.Errors, ArtworkErrorEntry{Artist: v.Artist, Message: fmt.Sprintf("download %s: %v", t.filename, err)})
				continue
			}
			result.ImagesWritten++
		}
	}

	return result, nil
}

func downloadFile(ctx context.Context, httpClient *http.Client, url, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(targetPath)
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}
