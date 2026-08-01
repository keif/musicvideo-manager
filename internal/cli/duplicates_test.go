package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestDuplicatesCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"duplicates", root})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when finding duplicates for a library that hasn't been scanned yet")
	}
}

func TestDuplicatesCommandJSONOutputEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	dupCmd := cli.NewRootCmd("test")
	dupCmd.SetOut(&out)
	dupCmd.SetArgs([]string{"duplicates", root, "--json"})

	if err := dupCmd.Execute(); err != nil {
		t.Fatalf("duplicates Execute: %v", err)
	}

	var result struct {
		VideosScanned   int             `json:"videos_scanned"`
		DuplicateGroups []map[string]any `json:"duplicate_groups"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if result.VideosScanned != 0 || len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected an all-zero result for an empty library, got %+v", result)
	}
}
