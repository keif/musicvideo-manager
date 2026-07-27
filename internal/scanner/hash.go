package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const hashChunkSize = 1 << 20 // 1MiB

// ComputeHash returns a fast identity hash for path: files up to 2MiB are
// hashed in full; larger files are hashed as SHA-256(first 1MiB || last
// 1MiB), skipping the middle. This is fast enough to run on every scan and
// is sufficient for move/rename detection and common-case dedup.
func ComputeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()

	h := sha256.New()
	if size <= 2*hashChunkSize {
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
	} else {
		buf := make([]byte, hashChunkSize)
		if _, err := io.ReadFull(f, buf); err != nil {
			return "", err
		}
		h.Write(buf)

		if _, err := f.Seek(-hashChunkSize, io.SeekEnd); err != nil {
			return "", err
		}
		if _, err := io.ReadFull(f, buf); err != nil {
			return "", err
		}
		h.Write(buf)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
