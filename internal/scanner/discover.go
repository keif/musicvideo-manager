package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// FileInfo is a discovered file's identity as seen on disk, before any
// hashing or probing has happened.
type FileInfo struct {
	Path  string // relative to root, forward-slash separated
	Size  int64
	MTime int64
}

const stateDirName = ".musicvideo-manager"

// Discover walks root recursively and returns every file whose extension
// (case-insensitive) is in extensions, keyed by root-relative path. The
// state directory (.musicvideo-manager) is always skipped, regardless of
// its contents.
func Discover(root string, extensions []string) (map[string]FileInfo, error) {
	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet["."+strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}

	skipDir := filepath.Join(root, stateDirName)
	result := make(map[string]FileInfo)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == skipDir {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !extSet[ext] {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		result[rel] = FileInfo{Path: rel, Size: info.Size(), MTime: info.ModTime().Unix()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
