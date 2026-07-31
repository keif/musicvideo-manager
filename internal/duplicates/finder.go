package duplicates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// DuplicateGroup is a set of active videos confirmed byte-identical via
// full SHA-256 hashing.
type DuplicateGroup struct {
	FileSize int64    `json:"file_size"`
	Hash     string   `json:"hash"`
	Paths    []string `json:"paths"` // absolute (root-joined), sorted
}

// DuplicateErrorEntry is a single video whose full-hash read failed. Not
// persisted -- recorded in the run's Result only.
type DuplicateErrorEntry struct {
	VideoPath string `json:"video_path"`
	Message   string `json:"message"`
}

// Result summarizes a FindExactDuplicates run.
type Result struct {
	VideosScanned    int                   `json:"videos_scanned"`
	CandidatesHashed int                   `json:"candidates_hashed"`
	DuplicateGroups  []DuplicateGroup      `json:"duplicate_groups"`
	ReclaimableBytes int64                 `json:"reclaimable_bytes"`
	Errors           []DuplicateErrorEntry `json:"errors"`
}

type sizeHashKey struct {
	size int64
	hash string
}

// FindExactDuplicates finds groups of active videos that are byte-for-byte
// identical. Only videos sharing (FileSize, PartialHash) with at least one
// other active video are fully read -- a (FileSize, PartialHash) collision
// is a candidate, never itself reported; the full SHA-256 comparison is
// the authoritative check. Report-only: no filesystem changes.
func FindExactDuplicates(cat *catalog.Catalog, root string) (Result, error) {
	videos, err := cat.ListActive()
	if err != nil {
		return Result{}, fmt.Errorf("list active videos: %w", err)
	}

	result := Result{VideosScanned: len(videos), DuplicateGroups: []DuplicateGroup{}, Errors: []DuplicateErrorEntry{}}

	candidates := map[sizeHashKey][]*catalog.Video{}
	for _, v := range videos {
		key := sizeHashKey{size: v.FileSize, hash: v.PartialHash}
		candidates[key] = append(candidates[key], v)
	}

	fullGroups := map[string][]*catalog.Video{}

	for _, group := range candidates {
		if len(group) < 2 {
			continue
		}
		for _, v := range group {
			fullPath := filepath.Join(root, filepath.FromSlash(v.Path))
			hash, err := hashFile(fullPath)
			if err != nil {
				result.Errors = append(result.Errors, DuplicateErrorEntry{VideoPath: v.Path, Message: err.Error()})
				continue
			}
			result.CandidatesHashed++
			fullGroups[hash] = append(fullGroups[hash], v)
		}
	}

	for hash, entries := range fullGroups {
		if len(entries) < 2 {
			continue
		}
		paths := make([]string, 0, len(entries))
		for _, v := range entries {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(v.Path)))
		}
		sort.Strings(paths)

		size := entries[0].FileSize
		result.DuplicateGroups = append(result.DuplicateGroups, DuplicateGroup{
			FileSize: size,
			Hash:     hash,
			Paths:    paths,
		})
		result.ReclaimableBytes += int64(len(entries)-1) * size
	}

	sort.Slice(result.DuplicateGroups, func(i, j int) bool {
		return result.DuplicateGroups[i].Paths[0] < result.DuplicateGroups[j].Paths[0]
	})
	sort.Slice(result.Errors, func(i, j int) bool {
		return result.Errors[i].VideoPath < result.Errors[j].VideoPath
	})

	return result, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
