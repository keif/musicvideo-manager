package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestArtworkCommandLoadsAPIKeyFromDotEnvFile(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "")
	os.Unsetenv("FANARTTV_API_KEY")

	// The .env file lives under $HOME/.config/musicvideo-manager/, not
	// inside the library being scanned -- redirect HOME so this test
	// never touches the real user's config.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	configDir := filepath.Join(fakeHome, ".config", "musicvideo-manager")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("FANARTTV_API_KEY=from-dotenv-file\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

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

	// An empty library never makes a real fanart.tv call, so this only
	// exercises that the .env file satisfied the API-key check rather than
	// the command failing with "FANARTTV_API_KEY environment variable is
	// not set".
	if err := artworkCmd.Execute(); err != nil {
		t.Fatalf("artwork Execute: %v", err)
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
