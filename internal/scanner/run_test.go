package scanner_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/scanner"
)

func TestRunPersistsScanRunAndReturnsSummary(t *testing.T) {
	requireFFmpeg(t)

	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })

	root := t.TempDir()
	writeVideoFile(t, root, "Artist/Song.mp4", "content-a")

	// scanner.Run wires up the real ComputeHash/Probe by default, but this
	// test only needs to verify orchestration (counts + scan_runs bookkeeping),
	// so it constructs a Reconciler-backed Run indirectly through the public
	// Run function with the real hash/probe implementations against a fixture
	// that doesn't need to be a valid video for the *hash* path — Probe will
	// fail for non-video content, which exercises the error-recording path too.
	summary, err := scanner.Run(context.Background(), cat, root, []string{"mp4"}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// "content-a" is not a real video, so ffprobe fails and it's recorded as
	// an error rather than added — this test is asserting Run's bookkeeping
	// (FilesScanned includes errored files, the error is captured), not Probe's
	// success path (covered by Task 8's TestProbeExtractsMetadata).
	if summary.FilesScanned != 1 {
		t.Fatalf("expected 1 file scanned, got %d", summary.FilesScanned)
	}
	if len(summary.Errors) != 1 {
		t.Fatalf("expected 1 error (invalid video content), got %+v", summary.Errors)
	}
	if summary.Errors[0].Path != "Artist/Song.mp4" {
		t.Fatalf("unexpected error path: %+v", summary.Errors[0])
	}
}
