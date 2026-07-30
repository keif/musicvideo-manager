package thumbnail_test

import (
	"context"
	"database/sql"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/thumbnail"
)

func openTestCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return cat
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func insertVideo(t *testing.T, cat *catalog.Catalog, relPath string, duration sql.NullFloat64) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: 5, MTime: 1, PartialHash: relPath,
		Status: catalog.StatusActive, DurationSeconds: duration,
		FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed, skipping")
	}
}

// generateFixtureVideo writes a short synthetic video (color bars + a sine
// wave audio tone) to path using ffmpeg, for tests that need a real,
// decodable file.
func generateFixtureVideo(t *testing.T, path string, durationSeconds int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%d:size=320x240:rate=25", durationSeconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=duration=%d", durationSeconds),
		"-c:v", "libx264", "-c:a", "aac", "-shortest",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture generation failed: %v\n%s", err, out)
	}
}

func TestWriteAllExtractsThumbnail(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	generateFixtureVideo(t, filepath.Join(root, "Beck", "Loser.mp4"), 4)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{Float64: 4, Valid: true})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideosProcessed != 1 || result.ThumbnailsWritten != 1 || result.ImagesSkipped != 0 || result.NoDuration != 0 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	targetPath := filepath.Join(root, "Beck", "Loser.jpg")
	f, err := os.Open(targetPath)
	if err != nil {
		t.Fatalf("expected thumbnail file to exist: %v", err)
	}
	defer f.Close()

	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatalf("expected a valid JPEG, got decode error: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("expected positive JPEG dimensions, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestWriteAllSkipsExistingThumbnail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Beck", "Loser.mp4"), []byte("video"))
	writeFile(t, filepath.Join(root, "Beck", "Loser.jpg"), []byte("already here"))

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{Float64: 4, Valid: true})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.ImagesSkipped != 1 || result.ThumbnailsWritten != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	content, err := os.ReadFile(filepath.Join(root, "Beck", "Loser.jpg"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "already here" {
		t.Fatalf("expected existing thumbnail to be left untouched, got %q", content)
	}
}

func TestWriteAllSkipsVideoWithNoDuration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Beck", "Loser.mp4"), []byte("video"))

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.NoDuration != 1 || result.ThumbnailsWritten != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "Beck", "Loser.jpg")); err == nil {
		t.Fatal("expected no thumbnail to be written for a video with no duration")
	}
}

func TestWriteAllRecordsExtractionFailureAndContinues(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Corrupt", "Broken.mp4"), []byte("this is not a video file"))
	generateFixtureVideo(t, filepath.Join(root, "Beck", "Loser.mp4"), 4)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Corrupt/Broken.mp4", sql.NullFloat64{Float64: 4, Valid: true})
	insertVideo(t, cat, "Beck/Loser.mp4", sql.NullFloat64{Float64: 4, Valid: true})

	result, err := thumbnail.WriteAll(context.Background(), cat, root, 50)
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if result.VideosProcessed != 2 || result.ThumbnailsWritten != 1 || len(result.Errors) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Errors[0].VideoPath != "Corrupt/Broken.mp4" || result.Errors[0].Message == "" {
		t.Fatalf("unexpected error entry: %+v", result.Errors[0])
	}
	if _, err := os.Stat(filepath.Join(root, "Beck", "Loser.jpg")); err != nil {
		t.Fatalf("expected the good video's thumbnail to still be written: %v", err)
	}
}
