package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
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

func TestScanCommandTextOutput(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Artist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	generateFixtureVideo(t, filepath.Join(root, "Artist", "Song.mp4"), 1)

	var out bytes.Buffer
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"scan", root})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, ".musicvideo-manager", "catalog.sqlite")); err != nil {
		t.Fatalf("expected catalog.sqlite to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".musicvideo-manager", "config.yaml")); err != nil {
		t.Fatalf("expected config.yaml to be created: %v", err)
	}
	if out.String() == "" {
		t.Fatal("expected non-empty scan summary output")
	}
}

func TestScanCommandJSONOutput(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	generateFixtureVideo(t, filepath.Join(root, "Song.mp4"), 1)

	var out bytes.Buffer
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"scan", root, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var summary struct {
		FilesAdded int `json:"files_added"`
	}
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if summary.FilesAdded != 1 {
		t.Fatalf("expected 1 file added, got %d", summary.FilesAdded)
	}
}

func TestScanCommandSecondRunReportsUnchanged(t *testing.T) {
	requireFFmpeg(t)

	root := t.TempDir()
	generateFixtureVideo(t, filepath.Join(root, "Song.mp4"), 1)

	first := cli.NewRootCmd("test")
	first.SetOut(&bytes.Buffer{})
	first.SetArgs([]string{"scan", root, "--json"})
	if err := first.Execute(); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	var out bytes.Buffer
	second := cli.NewRootCmd("test")
	second.SetOut(&out)
	second.SetArgs([]string{"scan", root, "--json"})
	if err := second.Execute(); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	var summary struct {
		FilesAdded int `json:"files_added"`
	}
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("expected valid JSON output: %v", err)
	}
	if summary.FilesAdded != 0 {
		t.Fatalf("expected 0 files added on second scan, got %d", summary.FilesAdded)
	}
}

func TestScanCommandNonexistentRoot(t *testing.T) {
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"scan", "/does/not/exist"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error scanning a nonexistent root")
	}
}

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	rootCmd := cli.NewRootCmd("v1.2.3")
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.String() != "v1.2.3\n" {
		t.Fatalf("expected %q, got %q", "v1.2.3\n", out.String())
	}
}
