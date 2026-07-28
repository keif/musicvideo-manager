package catalog_test

import (
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func TestOpenCreatesSchemaAndIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "catalog.sqlite")

	cat, err := catalog.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := cat.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-opening an existing catalog must not fail or re-apply migrations.
	cat2, err := catalog.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer cat2.Close()
}

func TestOpenRejectsUnwritableDirectory(t *testing.T) {
	_, err := catalog.Open(filepath.Join(t.TempDir(), "missing-parent", "catalog.sqlite"))
	if err == nil {
		t.Fatal("expected error opening catalog in a nonexistent directory, got nil")
	}
}
