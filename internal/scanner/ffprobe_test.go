package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/scanner"
)

func TestProbeExtractsMetadata(t *testing.T) {
	requireFFmpeg(t)

	path := filepath.Join(t.TempDir(), "fixture.mp4")
	generateFixtureVideo(t, path, 2)

	result, err := scanner.Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if result.DurationSeconds < 1.5 || result.DurationSeconds > 2.5 {
		t.Errorf("expected duration ~2s, got %f", result.DurationSeconds)
	}
	if result.Width != 320 || result.Height != 240 {
		t.Errorf("expected 320x240, got %dx%d", result.Width, result.Height)
	}
	if result.VideoCodec != "h264" {
		t.Errorf("expected video codec h264, got %q", result.VideoCodec)
	}
	if result.AudioCodec != "aac" {
		t.Errorf("expected audio codec aac, got %q", result.AudioCodec)
	}
	if result.FPS < 24 || result.FPS > 26 {
		t.Errorf("expected fps ~25, got %f", result.FPS)
	}
	if result.Container == "" {
		t.Error("expected non-empty container")
	}
}

func TestProbeCorruptFileReturnsError(t *testing.T) {
	requireFFmpeg(t)

	path := filepath.Join(t.TempDir(), "corrupt.mp4")
	if err := os.WriteFile(path, []byte("this is not a video file"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := scanner.Probe(context.Background(), path); err == nil {
		t.Fatal("expected error probing a corrupt file, got nil")
	}
}
