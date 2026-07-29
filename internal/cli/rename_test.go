package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/cli"
)

func TestRenameCommandRequiresPriorScan(t *testing.T) {
	root := t.TempDir()

	rootCmd := cli.NewRootCmd("test")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"rename", root, "--dry-run"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error when renaming a library that hasn't been scanned yet")
	}
}

func TestRenameCommandDryRunEmptyLibrary(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	var out bytes.Buffer
	renameCmd := cli.NewRootCmd("test")
	renameCmd.SetOut(&out)
	renameCmd.SetArgs([]string{"rename", root, "--dry-run", "--json"})

	if err := renameCmd.Execute(); err != nil {
		t.Fatalf("rename Execute: %v", err)
	}

	var plan struct {
		Moves     []any `json:"moves"`
		Unchanged int   `json:"unchanged"`
		Conflicts []any `json:"conflicts"`
	}
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("expected valid JSON output, got error %v; output: %s", err, out.String())
	}
	if len(plan.Moves) != 0 || plan.Unchanged != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("expected an all-zero plan for an empty library, got %+v", plan)
	}
}

func TestRenameCommandDryRunDoesNotTouchFilesystem(t *testing.T) {
	root := t.TempDir()
	videoPath := filepath.Join(root, "Some Artist", "Some Song.mp4")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	renameCmd := cli.NewRootCmd("test")
	renameCmd.SetOut(&bytes.Buffer{})
	renameCmd.SetArgs([]string{"rename", root, "--dry-run"})
	if err := renameCmd.Execute(); err != nil {
		t.Fatalf("rename Execute: %v", err)
	}

	if _, err := os.Stat(videoPath); err != nil {
		t.Fatalf("expected original file untouched by dry-run: %v", err)
	}
}

func TestRenameCommandUndoWithNothingToUndoErrors(t *testing.T) {
	root := t.TempDir()

	setupCmd := cli.NewRootCmd("test")
	setupCmd.SetOut(&bytes.Buffer{})
	setupCmd.SetArgs([]string{"scan", root})
	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("setup scan: %v", err)
	}

	undoCmd := cli.NewRootCmd("test")
	undoCmd.SetOut(&bytes.Buffer{})
	undoCmd.SetArgs([]string{"rename", root, "--undo"})

	if err := undoCmd.Execute(); err == nil {
		t.Fatal("expected an error undoing when nothing has been renamed")
	}
}
