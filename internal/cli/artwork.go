package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/artwork"
	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/providers/fanarttv"
)

func newArtworkCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "artwork [path]",
		Short: "Download artist artwork from fanart.tv for confirmed matches",
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

			envPath, err := userDotEnvPath()
			if err != nil {
				return err
			}
			if err := loadDotEnv(envPath); err != nil {
				return fmt.Errorf("load .env: %w", err)
			}

			apiKey := os.Getenv("FANARTTV_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("FANARTTV_API_KEY environment variable is not set")
			}

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			client := fanarttv.New(cat, apiKey)
			result, err := artwork.WriteAll(cmd.Context(), cat, client, absRoot)
			if err != nil {
				return fmt.Errorf("write artwork: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Processed %d artists: %d images written, %d skipped, %d with no artwork, %d errors\n",
				result.ArtistsProcessed, result.ImagesWritten, result.ImagesSkipped, result.NoArtwork, len(result.Errors))
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.Artist, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
