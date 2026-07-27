package scanner_test

import (
	"fmt"
	"os/exec"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed, skipping")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed, skipping")
	}
}

// generateFixtureVideo writes a short synthetic video (color bars + a sine
// wave audio tone) to path using ffmpeg, for tests that need a real,
// ffprobe-parseable file.
func generateFixtureVideo(t *testing.T, path string, durationSeconds int) {
	t.Helper()
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
