package scanner_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/scanner"
)

func fakeHash(path string) (string, error) {
	// deterministic per-content fake: hash = content bytes as string
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fakeProbe(ctx context.Context, path string) (scanner.ProbeResult, error) {
	return scanner.ProbeResult{DurationSeconds: 120, Width: 1920, Height: 1080, VideoCodec: "h264"}, nil
}

func newReconciler(t *testing.T) (*scanner.Reconciler, *catalog.Catalog) {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return &scanner.Reconciler{Catalog: cat, Hash: fakeHash, Probe: fakeProbe, Workers: 2}, cat
}

func writeVideoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestReconcileFirstScanAddsAllFiles(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")
	writeVideoFile(t, root, "Artist/Other.mp4", "content-b")

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.Added != 2 || result.Updated != 0 || result.Moved != 0 || result.Missing != 0 || result.Unchanged != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileSecondScanIsUnchanged(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")

	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Unchanged != 1 || result.Added != 0 || result.Updated != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileDetectsEditedFile(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")
	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	// Sleep isn't reliable for mtime granularity in CI; force a different
	// mtime explicitly instead of relying on wall-clock delay.
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a-edited")
	newTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "Artist/Song.mp4"), newTime, newTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Updated != 1 || result.Added != 0 || result.Unchanged != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileDetectsRename(t *testing.T) {
	rec, cat := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Old.mp4", "content-a")
	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	if err := os.Rename(filepath.Join(root, "Artist/Old.mp4"), filepath.Join(root, "Artist/New.mp4")); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Moved != 1 || result.Added != 0 || result.Missing != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	active, err := cat.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Path != "Artist/New.mp4" {
		t.Fatalf("expected single active row at new path, got %+v", active)
	}
}

func TestReconcileDetectsDeletion(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")
	if _, err := rec.Reconcile(context.Background(), root, []string{"mp4"}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "Artist/Song.mp4")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	result, err := rec.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if result.Missing != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReconcileContinuesPastHashError(t *testing.T) {
	rec, _ := newReconciler(t)
	root := t.TempDir()
	writeVideoFile(t, root, "Good.mp4", "content-a")
	writeVideoFile(t, root, "Bad.mp4", "content-b")

	failing := *rec
	failing.Hash = func(path string) (string, error) {
		if filepath.Base(path) == "Bad.mp4" {
			return "", fmt.Errorf("simulated hash failure")
		}
		return fakeHash(path)
	}

	result, err := failing.Reconcile(context.Background(), root, []string{"mp4"})
	if err != nil {
		t.Fatalf("Reconcile should not fail the whole run on one bad file: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected 1 successful add, got %+v", result)
	}
	if len(result.Errors) != 1 || result.Errors[0].Path != "Bad.mp4" {
		t.Fatalf("expected 1 recorded error for Bad.mp4, got %+v", result.Errors)
	}
}
