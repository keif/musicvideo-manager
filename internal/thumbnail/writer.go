package thumbnail

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// ThumbnailErrorEntry is a single video whose thumbnail extraction failed.
// Not persisted -- recorded in the run's Result only.
type ThumbnailErrorEntry struct {
	VideoPath string `json:"video_path"`
	Message   string `json:"message"`
}

// Result summarizes a WriteAll run.
type Result struct {
	VideosProcessed   int                   `json:"videos_processed"`
	ThumbnailsWritten int                   `json:"thumbnails_written"`
	ImagesSkipped     int                   `json:"images_skipped"`
	NoDuration        int                   `json:"no_duration"`
	Errors            []ThumbnailErrorEntry `json:"errors"`
}

// WriteAll generates a local thumbnail image for every active video in the
// catalog, seeking to percent% into each video's cataloged duration. An
// existing thumbnail file is never overwritten -- this only ever fills gaps.
func WriteAll(ctx context.Context, cat *catalog.Catalog, root string, percent int) (Result, error) {
	videos, err := cat.ListActive()
	if err != nil {
		return Result{}, fmt.Errorf("list active videos: %w", err)
	}

	result := Result{Errors: []ThumbnailErrorEntry{}}

	for _, v := range videos {
		result.VideosProcessed++

		fullPath := filepath.Join(root, filepath.FromSlash(v.Path))
		targetPath := strings.TrimSuffix(fullPath, filepath.Ext(fullPath)) + ".jpg"

		if _, err := os.Stat(targetPath); err == nil {
			result.ImagesSkipped++
			continue
		}

		if !v.DurationSeconds.Valid || v.DurationSeconds.Float64 <= 0 {
			result.NoDuration++
			continue
		}

		seekSeconds := v.DurationSeconds.Float64 * float64(percent) / 100

		if err := extractFrame(ctx, fullPath, seekSeconds, targetPath); err != nil {
			os.Remove(targetPath) // best-effort cleanup of any partial output
			result.Errors = append(result.Errors, ThumbnailErrorEntry{VideoPath: v.Path, Message: err.Error()})
			continue
		}
		result.ThumbnailsWritten++
	}

	return result, nil
}

func extractFrame(ctx context.Context, videoPath string, seekSeconds float64, targetPath string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.3f", seekSeconds),
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "2",
		targetPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg extract frame: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
