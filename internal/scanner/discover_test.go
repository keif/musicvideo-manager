package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/scanner"
)

func TestDiscoverFiltersByExtensionAndWalksSubdirs(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	mustWrite("Artist/Song.mp4")
	mustWrite("Artist/Nested/Other.MP4")     // case-insensitive extension match
	mustWrite("Artist/notes.txt")             // wrong extension, excluded
	mustWrite(".musicvideo-manager/catalog.sqlite") // state dir, always excluded, even matching content wouldn't matter

	found, err := scanner.Discover(root, []string{"mp4", "mkv"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(found), found)
	}
	if _, ok := found["Artist/Song.mp4"]; !ok {
		t.Error("expected Artist/Song.mp4 to be discovered")
	}
	if _, ok := found["Artist/Nested/Other.MP4"]; !ok {
		t.Error("expected Artist/Nested/Other.MP4 to be discovered")
	}
}

func TestDiscoverSkipsStateDirEvenWithMatchingExtension(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".musicvideo-manager")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sneaky.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	found, err := scanner.Discover(root, []string{"mp4"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 files, got %d: %+v", len(found), found)
	}
}

func TestDiscoverEmptyDirectory(t *testing.T) {
	found, err := scanner.Discover(t.TempDir(), []string{"mp4"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 files, got %d", len(found))
	}
}

func TestDiscoverNonexistentRoot(t *testing.T) {
	_, err := scanner.Discover(filepath.Join(t.TempDir(), "does-not-exist"), []string{"mp4"})
	if err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}
