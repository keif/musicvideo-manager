package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestNFOCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"nfo", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when generating NFOs for a library that hasn't been scanned yet")
	}
}

func TestNFOCommandJSONOutputEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	nfoCmd := cli.NewRootCmd("test")
	nfoCmd.SetOut(&out)
	nfoCmd.SetArgs([]string{"nfo", root, "--json"})

	if err := nfoCmd.Execute(); err != nil {
		t.Fatalf("nfo Execute: %v", err)
	}

	var result struct {
		VideoNFOsWritten  int `json:"video_nfos_written"`
		ArtistNFOsWritten int `json:"artist_nfos_written"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.VideoNFOsWritten != 0 || result.ArtistNFOsWritten != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
