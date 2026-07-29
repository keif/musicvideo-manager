package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/nfo"
)

func newNFOCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "nfo [path]",
		Short: "Write Kodi-format NFO files for confirmed matches",
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

			cat, err := catalog.Open(filepath.Join(stateDir, "catalog.sqlite"))
			if err != nil {
				return fmt.Errorf("open catalog: %w", err)
			}
			defer cat.Close()

			result, err := nfo.WriteAll(cat, absRoot)
			if err != nil {
				return fmt.Errorf("write nfo: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d video NFOs, %d artist NFOs\n", result.VideoNFOsWritten, result.ArtistNFOsWritten)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
