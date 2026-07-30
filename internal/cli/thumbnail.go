package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/config"
	"github.com/keif/musicvideo-manager/internal/thumbnail"
)

func newThumbnailCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "thumbnail [path]",
		Short: "Generate local video thumbnails by extracting a frame with ffmpeg",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			absRoot, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			stateDir := filepath.Join(absRoot, ".musicvideo-manager")
			if _, err := os.Stat(stateDir); err != nil {
				return fmt.Errorf("library not yet scanned, run 'scan' first: %w", err)
			}

			cfg, err := config.LoadOrCreate(filepath.Join(stateDir, "config.yaml"), filepath.Base(absRoot))
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Thumbnail.Percent <= 0 || cfg.Thumbnail.Percent >= 100 {
				return fmt.Errorf("thumbnail.percent must be between 1 and 99 in config.yaml (got %d) -- add a thumbnail: section with percent: 10 (or your preferred value)", cfg.Thumbnail.Percent)
			}

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			result, err := thumbnail.WriteAll(cmd.Context(), cat, absRoot, cfg.Thumbnail.Percent)
			if err != nil {
				return fmt.Errorf("write thumbnails: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Processed %d videos: %d thumbnails written, %d skipped, %d with no duration, %d errors\n",
				result.VideosProcessed, result.ThumbnailsWritten, result.ImagesSkipped, result.NoDuration, len(result.Errors))
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.VideoPath, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
