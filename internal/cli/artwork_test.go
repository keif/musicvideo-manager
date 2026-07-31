package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestArtworkCommandRequiresAPIKey(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "")

	root := t.TempDir()
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"artwork", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when FANARTTV_API_KEY is not set")
	}
}

func TestArtworkCommandRequiresPriorScan(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "test-key")

	root := t.TempDir()
	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"artwork", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when the library hasn't been scanned yet")
	}
}

func TestArtworkCommandJSONOutputEmptyLibrary(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "test-key")

	root := t.TempDir()
	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	artworkCmd := cli.NewRootCmd("test")
	artworkCmd.SetOut(&out)
	artworkCmd.SetArgs([]string{"artwork", root, "--json"})

	// An empty library has no confirmed matches, so WriteAll never makes a
	// real network call -- this exercises the command's plumbing (env var
	// check, config/catalog wiring, JSON encoding) without needing a mock
	// fanart.tv server.
	if err := artworkCmd.Execute(); err != nil {
		t.Fatalf("artwork Execute: %v", err)
	}

	var result struct {
		ArtistsProcessed int `json:"artists_processed"`
		ImagesWritten    int `json:"images_written"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.ArtistsProcessed != 0 || result.ImagesWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
