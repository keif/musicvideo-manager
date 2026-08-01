// internal/duplicates/finder_test.go
package duplicates_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/duplicates"
)

func openTestCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	return cat
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func insertVideo(t *testing.T, cat *catalog.Catalog, relPath string, fileSize int64, partialHash string) int64 {
	t.Helper()
	id, err := cat.InsertVideo(&catalog.Video{
		Path: relPath, FileSize: fileSize, MTime: 1, PartialHash: partialHash,
		Status: catalog.StatusActive, FirstSeenAt: 1, LastSeenAt: 1, LastScannedAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertVideo: %v", err)
	}
	return id
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func TestFindExactDuplicatesGroupsIdenticalFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("identical video bytes")

	writeFile(t, filepath.Join(root, "A.mp4"), content)
	writeFile(t, filepath.Join(root, "B.mp4"), content)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "A.mp4", int64(len(content)), "same-partial-hash")
	insertVideo(t, cat, "B.mp4", int64(len(content)), "same-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if result.VideosScanned != 2 || result.CandidatesHashed != 2 {
		t.Fatalf("unexpected counters: %+v", result)
	}
	if len(result.DuplicateGroups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d: %+v", len(result.DuplicateGroups), result.DuplicateGroups)
	}
	group := result.DuplicateGroups[0]
	if group.Hash != sha256Hex(content) {
		t.Fatalf("unexpected hash: %q", group.Hash)
	}
	if group.FileSize != int64(len(content)) {
		t.Fatalf("unexpected file size: %d", group.FileSize)
	}
	wantPaths := []string{filepath.Join(root, "A.mp4"), filepath.Join(root, "B.mp4")}
	if len(group.Paths) != 2 || group.Paths[0] != wantPaths[0] || group.Paths[1] != wantPaths[1] {
		t.Fatalf("unexpected paths: %v", group.Paths)
	}
	if result.ReclaimableBytes != int64(len(content)) {
		t.Fatalf("expected reclaimable bytes %d, got %d", len(content), result.ReclaimableBytes)
	}
}

func TestFindExactDuplicatesRejectsPartialHashCollisionWithDifferentContent(t *testing.T) {
	root := t.TempDir()
	contentA := []byte("AAAA-middle-differs-1-AAAA")
	contentB := []byte("AAAA-middle-differs-2-AAAA")
	if len(contentA) != len(contentB) {
		t.Fatalf("test fixture bug: contents must be the same length")
	}

	writeFile(t, filepath.Join(root, "A.mp4"), contentA)
	writeFile(t, filepath.Join(root, "B.mp4"), contentB)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "A.mp4", int64(len(contentA)), "colliding-partial-hash")
	insertVideo(t, cat, "B.mp4", int64(len(contentB)), "colliding-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if result.CandidatesHashed != 2 {
		t.Fatalf("expected both candidates to be hashed, got %d", result.CandidatesHashed)
	}
	if len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected no duplicate groups (content differs), got %+v", result.DuplicateGroups)
	}
	if result.ReclaimableBytes != 0 {
		t.Fatalf("expected 0 reclaimable bytes, got %d", result.ReclaimableBytes)
	}
}

func TestFindExactDuplicatesSkipsVideoWithNoCollision(t *testing.T) {
	root := t.TempDir()
	content := []byte("unique video bytes")
	writeFile(t, filepath.Join(root, "Solo.mp4"), content)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Solo.mp4", int64(len(content)), "unique-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if result.VideosScanned != 1 {
		t.Fatalf("expected 1 video scanned, got %d", result.VideosScanned)
	}
	if result.CandidatesHashed != 0 {
		t.Fatalf("expected 0 candidates hashed (no collision), got %d", result.CandidatesHashed)
	}
	if len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected no duplicate groups, got %+v", result.DuplicateGroups)
	}
}

func TestFindExactDuplicatesHandlesThreeWayGroup(t *testing.T) {
	root := t.TempDir()
	content := []byte("triplicate video bytes")

	for _, name := range []string{"A.mp4", "B.mp4", "C.mp4"} {
		writeFile(t, filepath.Join(root, name), content)
	}

	cat := openTestCatalog(t)
	for _, name := range []string{"A.mp4", "B.mp4", "C.mp4"} {
		insertVideo(t, cat, name, int64(len(content)), "shared-partial-hash")
	}

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.DuplicateGroups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d: %+v", len(result.DuplicateGroups), result.DuplicateGroups)
	}
	if len(result.DuplicateGroups[0].Paths) != 3 {
		t.Fatalf("expected 3 paths in the group, got %d", len(result.DuplicateGroups[0].Paths))
	}
	wantReclaimable := int64(2) * int64(len(content))
	if result.ReclaimableBytes != wantReclaimable {
		t.Fatalf("expected reclaimable bytes %d, got %d", wantReclaimable, result.ReclaimableBytes)
	}
}

func TestFindExactDuplicatesRecordsReadErrorAndContinues(t *testing.T) {
	root := t.TempDir()
	content := []byte("video bytes")
	writeFile(t, filepath.Join(root, "Real.mp4"), content)
	// "Missing.mp4" is cataloged but has no file on disk -- simulates a
	// file that vanished or became unreadable after the last scan.

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Real.mp4", int64(len(content)), "shared-partial-hash")
	insertVideo(t, cat, "Missing.mp4", int64(len(content)), "shared-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].VideoPath != "Missing.mp4" || result.Errors[0].Message == "" {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if result.CandidatesHashed != 1 {
		t.Fatalf("expected 1 candidate successfully hashed, got %d", result.CandidatesHashed)
	}
	if len(result.DuplicateGroups) != 0 {
		t.Fatalf("expected no duplicate groups (only one survivor), got %+v", result.DuplicateGroups)
	}
}

func TestFindExactDuplicatesSortsErrorsByVideoPath(t *testing.T) {
	root := t.TempDir()
	contentA := []byte("video bytes group a")
	contentB := []byte("video bytes group b")

	// Two independent candidate buckets, each with one file present on
	// disk and one missing. Map iteration order over `candidates` is
	// randomized, so without sorting, result.Errors could come back in
	// either order across runs -- this test only proves something with
	// two independent read failures to order.
	writeFile(t, filepath.Join(root, "ZReal.mp4"), contentA)
	// "ZMissing.mp4" is cataloged but has no file on disk.
	writeFile(t, filepath.Join(root, "AReal.mp4"), contentB)
	// "AMissing.mp4" is cataloged but has no file on disk.

	cat := openTestCatalog(t)
	insertVideo(t, cat, "ZReal.mp4", int64(len(contentA)), "group-a-partial-hash")
	insertVideo(t, cat, "ZMissing.mp4", int64(len(contentA)), "group-a-partial-hash")
	insertVideo(t, cat, "AReal.mp4", int64(len(contentB)), "group-b-partial-hash")
	insertVideo(t, cat, "AMissing.mp4", int64(len(contentB)), "group-b-partial-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %+v", len(result.Errors), result.Errors)
	}
	wantOrder := []string{"AMissing.mp4", "ZMissing.mp4"}
	gotOrder := []string{result.Errors[0].VideoPath, result.Errors[1].VideoPath}
	if gotOrder[0] != wantOrder[0] || gotOrder[1] != wantOrder[1] {
		t.Fatalf("expected errors sorted by VideoPath %v, got %v", wantOrder, gotOrder)
	}
	if result.Errors[0].Message == "" || result.Errors[1].Message == "" {
		t.Fatalf("expected non-empty error messages: %+v", result.Errors)
	}
}

func TestFindExactDuplicatesReclaimableBytesAcrossMultipleGroups(t *testing.T) {
	root := t.TempDir()
	smallContent := []byte("small")
	bigContent := []byte("this is a much bigger duplicate payload")

	writeFile(t, filepath.Join(root, "Small1.mp4"), smallContent)
	writeFile(t, filepath.Join(root, "Small2.mp4"), smallContent)
	writeFile(t, filepath.Join(root, "Big1.mp4"), bigContent)
	writeFile(t, filepath.Join(root, "Big2.mp4"), bigContent)

	cat := openTestCatalog(t)
	insertVideo(t, cat, "Small1.mp4", int64(len(smallContent)), "small-hash")
	insertVideo(t, cat, "Small2.mp4", int64(len(smallContent)), "small-hash")
	insertVideo(t, cat, "Big1.mp4", int64(len(bigContent)), "big-hash")
	insertVideo(t, cat, "Big2.mp4", int64(len(bigContent)), "big-hash")

	result, err := duplicates.FindExactDuplicates(cat, root)
	if err != nil {
		t.Fatalf("FindExactDuplicates: %v", err)
	}
	if len(result.DuplicateGroups) != 2 {
		t.Fatalf("expected 2 duplicate groups, got %d: %+v", len(result.DuplicateGroups), result.DuplicateGroups)
	}
	// Sorted by first path: "Big1.mp4" < "Small1.mp4"
	if result.DuplicateGroups[0].Paths[0] != filepath.Join(root, "Big1.mp4") {
		t.Fatalf("expected groups sorted by first path, got %+v", result.DuplicateGroups)
	}
	wantReclaimable := int64(len(smallContent)) + int64(len(bigContent))
	if result.ReclaimableBytes != wantReclaimable {
		t.Fatalf("expected reclaimable bytes %d, got %d", wantReclaimable, result.ReclaimableBytes)
	}
}
