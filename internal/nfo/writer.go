package nfo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keif/musicvideo-manager/internal/catalog"
)

// Result summarizes a WriteAll run.
type Result struct {
	VideoNFOsWritten  int `json:"video_nfos_written"`
	ArtistNFOsWritten int `json:"artist_nfos_written"`
}

// WriteAll generates a per-video NFO for every confirmed match and a
// per-artist NFO for every distinct video folder among them, rooted at root.
func WriteAll(cat *catalog.Catalog, root string) (Result, error) {
	matches, err := cat.ListConfirmedMatches()
	if err != nil {
		return Result{}, fmt.Errorf("list confirmed matches: %w", err)
	}

	result := Result{}
	writtenArtistDirs := map[string]bool{}

	for _, m := range matches {
		videoFull := filepath.Join(root, filepath.FromSlash(m.VideoPath))
		ext := filepath.Ext(videoFull)
		nfoPath := strings.TrimSuffix(videoFull, ext) + ".nfo"

		body, err := MarshalMusicVideo(MusicVideoNFO{
			Title: m.Title, Artist: m.Artist, Album: m.Album, Year: m.Year,
			FilenameAndPath: m.VideoPath,
		})
		if err != nil {
			return Result{}, fmt.Errorf("marshal video nfo for %s: %w", m.VideoPath, err)
		}
		if err := os.WriteFile(nfoPath, body, 0o644); err != nil {
			return Result{}, fmt.Errorf("write video nfo for %s: %w", m.VideoPath, err)
		}
		result.VideoNFOsWritten++

		artistDir := filepath.Dir(videoFull)
		if !writtenArtistDirs[artistDir] {
			artistBody, err := MarshalArtist(ArtistNFO{Name: m.Artist})
			if err != nil {
				return Result{}, fmt.Errorf("marshal artist nfo for %s: %w", m.Artist, err)
			}
			if err := os.WriteFile(filepath.Join(artistDir, "artist.nfo"), artistBody, 0o644); err != nil {
				return Result{}, fmt.Errorf("write artist nfo for %s: %w", m.Artist, err)
			}
			writtenArtistDirs[artistDir] = true
			result.ArtistNFOsWritten++
		}
	}

	return result, nil
}
