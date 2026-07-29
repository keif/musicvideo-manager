package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/config"
	"github.com/keif/musicvideo-manager/internal/match"
	"github.com/keif/musicvideo-manager/internal/providers/musicbrainz"
)

const musicBrainzUserAgent = "musicvideo-manager/dev (https://github.com/keif/musicvideo-manager)"

func newMatchCmd() *cobra.Command {
	var jsonOutput bool
	var listReview bool

	cmd := &cobra.Command{
		Use:   "match [path]",
		Short: "Identify catalogued videos against MusicBrainz",
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

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			if listReview {
				return printReviewQueue(cmd, cat, jsonOutput)
			}

			matcher := &match.Matcher{
				Catalog:             cat,
				Provider:            musicbrainz.New(cat, musicBrainzUserAgent),
				ConfidenceThreshold: cfg.Match.ConfidenceThreshold,
			}

			result, err := matcher.Run(cmd.Context())
			if err != nil {
				return fmt.Errorf("match: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Matched: %d confirmed, %d needs review, %d unmatched, %d errors\n",
				result.Confirmed, result.NeedsReview, result.Unmatched, len(result.Errors))
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.Path, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&listReview, "list-review", false, "list videos needing manual review instead of matching")
	return cmd
}

func printReviewQueue(cmd *cobra.Command, cat *catalog.Catalog, jsonOutput bool) error {
	items, err := cat.ListNeedsReview()
	if err != nil {
		return fmt.Errorf("list needs review: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	for _, item := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n  parsed: %s - %s (confidence %.2f)\n",
			item.VideoPath, item.ParsedArtist, item.ParsedTitle, item.Confidence)
	}
	return nil
}
