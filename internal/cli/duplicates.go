package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/duplicates"
)

func newDuplicatesCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "duplicates [path]",
		Short: "Find byte-identical duplicate videos via full SHA-256 hashing",
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

			result, err := duplicates.FindExactDuplicates(cat, absRoot)
			if err != nil {
				return fmt.Errorf("find duplicates: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Scanned %d videos, hashed %d candidates: %d duplicate groups found, %d bytes reclaimable, %d errors\n",
				result.VideosScanned, result.CandidatesHashed, len(result.DuplicateGroups), result.ReclaimableBytes, len(result.Errors))
			for _, g := range result.DuplicateGroups {
				fmt.Fprintf(cmd.OutOrStdout(), "  duplicate (%d bytes, %s):\n", g.FileSize, g.Hash)
				for _, p := range g.Paths {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", p)
				}
			}
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.VideoPath, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
