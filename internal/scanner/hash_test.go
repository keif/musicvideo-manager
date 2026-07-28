package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keif/musicvideo-manager/internal/scanner"
)

func writeFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestComputeHashEmptyFile(t *testing.T) {
	path := writeFile(t, t.TempDir(), "empty.mp4", nil)
	hash, err := scanner.ComputeHash(path)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash for empty file")
	}
}

func TestComputeHashSmallFileDeterministicAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	pathA := writeFile(t, dir, "a.mp4", []byte("hello world"))
	pathB := writeFile(t, dir, "b.mp4", []byte("hello world"))
	pathC := writeFile(t, dir, "c.mp4", []byte("goodbye world"))

	hashA, err := scanner.ComputeHash(pathA)
	if err != nil {
		t.Fatalf("ComputeHash a: %v", err)
	}
	hashB, err := scanner.ComputeHash(pathB)
	if err != nil {
		t.Fatalf("ComputeHash b: %v", err)
	}
	hashC, err := scanner.ComputeHash(pathC)
	if err != nil {
		t.Fatalf("ComputeHash c: %v", err)
	}

	if hashA != hashB {
		t.Fatalf("expected identical content to hash identically: %q != %q", hashA, hashB)
	}
	if hashA == hashC {
		t.Fatal("expected different content to hash differently")
	}
}

func TestComputeHashLargeFileIgnoresMiddleBytes(t *testing.T) {
	dir := t.TempDir()
	const chunk = 1 << 20 // 1MiB, matches scanner.hashChunkSize

	build := func(first, last byte) []byte {
		buf := make([]byte, 2*chunk+100)
		for i := 0; i < chunk; i++ {
			buf[i] = first
		}
		for i := len(buf) - chunk; i < len(buf); i++ {
			buf[i] = last
		}
		return buf
	}

	contentA := build(0xAA, 0xBB)
	contentA[chunk+50] = 0x01 // differs only in the untouched middle region
	contentB := build(0xAA, 0xBB)
	contentB[chunk+50] = 0x02

	pathA := writeFile(t, dir, "large-a.mp4", contentA)
	pathB := writeFile(t, dir, "large-b.mp4", contentB)

	hashA, err := scanner.ComputeHash(pathA)
	if err != nil {
		t.Fatalf("ComputeHash a: %v", err)
	}
	hashB, err := scanner.ComputeHash(pathB)
	if err != nil {
		t.Fatalf("ComputeHash b: %v", err)
	}
	if hashA != hashB {
		t.Fatal("expected files differing only in the middle region to hash identically")
	}

	contentC := build(0xAA, 0xCC) // differs in the last chunk
	pathC := writeFile(t, dir, "large-c.mp4", contentC)
	hashC, err := scanner.ComputeHash(pathC)
	if err != nil {
		t.Fatalf("ComputeHash c: %v", err)
	}
	if hashA == hashC {
		t.Fatal("expected files differing in the last chunk to hash differently")
	}
}

func TestComputeHashNonexistentFile(t *testing.T) {
	_, err := scanner.ComputeHash(filepath.Join(t.TempDir(), "does-not-exist.mp4"))
	if err == nil {
		t.Fatal("expected error hashing a nonexistent file")
	}
}
