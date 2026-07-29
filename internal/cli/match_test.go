package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestMatchCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"match", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when matching a library that hasn't been scanned yet")
	}
}

func TestMatchCommandJSONOutput(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".musicvideo-manager")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Simulate a completed scan by opening the catalog directly and
	// inserting one active video with an unparseable filename (no " - "
	// delimiter, no parent folder), so match completes without needing
	// real network access to MusicBrainz.
	catPath := filepath.Join(stateDir, "catalog.sqlite")
	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	// Creating the catalog file directly via the scan command against an
	// empty directory is the simplest way to get a real, migrated
	// catalog.sqlite on disk without duplicating catalog.Open here.
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}
	if _, err := os.Stat(catPath); err != nil {
		t.Fatalf("expected catalog.sqlite to exist after scan: %v", err)
	}

	var out bytes.Buffer
	matchCmd := cli.NewRootCmd("test")
	matchCmd.SetOut(&out)
	matchCmd.SetArgs([]string{"match", root, "--json"})

	if err := matchCmd.Execute(); err != nil {
		t.Fatalf("match Execute: %v", err)
	}

	var result struct {
		Confirmed   int `json:"confirmed"`
		NeedsReview int `json:"needs_review"`
		Unmatched   int `json:"unmatched"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	// The scanned directory is empty (no video files), so there's nothing
	// to match -- this test exercises the command's plumbing (config load,
	// catalog open, Matcher wiring, JSON encoding), not matching logic
	// itself, which is covered by internal/match's own tests.
	if result.Confirmed != 0 || result.NeedsReview != 0 || result.Unmatched != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}

func TestMatchCommandListReview(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	listCmd := cli.NewRootCmd("test")
	listCmd.SetOut(&out)
	listCmd.SetArgs([]string{"match", root, "--list-review", "--json"})

	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list-review Execute: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("expected valid JSON array output, got error %v; output: %s", err, out.String())
	}
	if len(items) != 0 {
		t.Fatalf("expected an empty review queue for an empty library, got %+v", items)
	}
}
