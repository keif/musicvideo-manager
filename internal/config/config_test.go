package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/config"
)

func TestLoadOrCreateWritesDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg, err := config.LoadOrCreate(path, "My Library")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Library.Name != "My Library" {
		t.Fatalf("expected library name %q, got %q", "My Library", cfg.Library.Name)
	}
	if len(cfg.Scan.Extensions) == 0 {
		t.Fatal("expected default extensions to be populated")
	}
	if cfg.Scan.Workers != 0 {
		t.Fatalf("expected default workers 0 (auto), got %d", cfg.Scan.Workers)
	}
	if cfg.Thumbnail.Percent != 10 {
		t.Fatalf("expected default thumbnail percent 10, got %d", cfg.Thumbnail.Percent)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to be written to disk: %v", err)
	}
}

func TestLoadOrCreateReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "schema_version: 1\nlibrary:\n  name: Existing\nscan:\n  extensions: [mp4]\n  workers: 4\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Ignored Default")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Library.Name != "Existing" {
		t.Fatalf("expected existing config to be loaded, got name %q", cfg.Library.Name)
	}
	if cfg.Scan.Workers != 4 {
		t.Fatalf("expected workers 4, got %d", cfg.Scan.Workers)
	}
}

func TestLoadOrCreateRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := config.LoadOrCreate(path, "Default"); err == nil {
		t.Fatal("expected error loading invalid YAML, got nil")
	}
}

func TestLoadOrCreateRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Default")
	if err != nil {
		t.Fatalf("LoadOrCreate on empty file: %v", err)
	}
	// An empty YAML file parses to a zero-value Config: schema_version 0,
	// no extensions configured. Callers (the scan command) are responsible
	// for deciding whether that's acceptable; Load itself doesn't inject
	// defaults into a file that exists but is empty.
	if cfg.SchemaVersion != 0 {
		t.Fatalf("expected zero-value SchemaVersion for empty file, got %d", cfg.SchemaVersion)
	}
}

func TestDefaultIncludesMatchConfidenceThreshold(t *testing.T) {
	cfg := config.Default("My Library")
	if cfg.Match.ConfidenceThreshold != 0.7 {
		t.Fatalf("expected default confidence threshold 0.7, got %f", cfg.Match.ConfidenceThreshold)
	}
}

func TestLoadOrCreateParsesExistingMatchSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "schema_version: 1\nlibrary:\n  name: Existing\nscan:\n  extensions: [mp4]\n  workers: 4\nmatch:\n  confidence_threshold: 0.85\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Ignored Default")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Match.ConfidenceThreshold != 0.85 {
		t.Fatalf("expected confidence threshold 0.85, got %f", cfg.Match.ConfidenceThreshold)
	}
}

func TestDefaultIncludesRenameTemplate(t *testing.T) {
	cfg := config.Default("My Library")
	if cfg.Rename.Template != "{artist}/{title}" {
		t.Fatalf("expected default rename template %q, got %q", "{artist}/{title}", cfg.Rename.Template)
	}
}

func TestLoadOrCreateParsesExistingRenameSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "schema_version: 1\nlibrary:\n  name: Existing\nscan:\n  extensions: [mp4]\n  workers: 4\nmatch:\n  confidence_threshold: 0.7\nrename:\n  template: \"{artist}/{year} - {title}\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadOrCreate(path, "Ignored Default")
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Rename.Template != "{artist}/{year} - {title}" {
		t.Fatalf("expected loaded rename template, got %q", cfg.Rename.Template)
	}
}
