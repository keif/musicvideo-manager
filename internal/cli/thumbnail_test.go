package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestThumbnailCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"thumbnail", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when generating thumbnails for a library that hasn't been scanned yet")
	}
}

func TestThumbnailCommandRejectsInvalidPercentConfig(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	// Simulate a config.yaml that predates the thumbnail section (the
	// established non-backfilling behavior of config.Load) -- percent
	// resolves to the Go zero value, 0, which must be rejected rather
	// than silently seeking to the very first frame of every video.
	configPath := filepath.Join(root, ".musicvideo-manager", "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 1\nlibrary:\n  name: Test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	thumbCmd := cli.NewRootCmd("test")
	thumbCmd.SetOut(&bytes.Buffer{})
	thumbCmd.SetArgs([]string{"thumbnail", root})

	if err := thumbCmd.Execute(); err == nil {
		t.Fatal("expected an error when thumbnail.percent is missing/zero in config.yaml")
	}
}

func TestThumbnailCommandJSONOutputEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	thumbCmd := cli.NewRootCmd("test")
	thumbCmd.SetOut(&out)
	thumbCmd.SetArgs([]string{"thumbnail", root, "--json"})

	if err := thumbCmd.Execute(); err != nil {
		t.Fatalf("thumbnail Execute: %v", err)
	}

	var result struct {
		VideosProcessed   int `json:"videos_processed"`
		ThumbnailsWritten int `json:"thumbnails_written"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.VideosProcessed != 0 || result.ThumbnailsWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
