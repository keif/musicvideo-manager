package catalog_test

import (
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

func TestScanRunLifecycle(t *testing.T) {
	cat := openTestCatalog(t)

	id, err := cat.StartScanRun(1000)
	if err != nil {
		t.Fatalf("StartScanRun: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero scan run id")
	}

	if err := cat.RecordScanError(id, "bad/file.mp4", "ffprobe: invalid data", 1005); err != nil {
		t.Fatalf("RecordScanError: %v", err)
	}

	counts := catalog.ScanRunCounts{Scanned: 3, Added: 1, Updated: 1, Moved: 0, Missing: 0, Errors: 1}
	if err := cat.FinishScanRun(id, counts, 1010); err != nil {
		t.Fatalf("FinishScanRun: %v", err)
	}
}
