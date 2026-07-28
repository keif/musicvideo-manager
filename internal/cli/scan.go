package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/config"
	"github.com/keif/musicvideo-manager/internal/scanner"
)

func newScanCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a library directory and update its catalog",
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

			info, err := os.Stat(absRoot)
			if err != nil {
				return fmt.Errorf("library root: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("library root %s is not a directory", absRoot)
			}

			stateDir := filepath.Join(absRoot, ".musicvideo-manager")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				return fmt.Errorf("create state dir: %w", err)
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

			summary, err := scanner.Run(cmd.Context(), cat, absRoot, cfg.Scan.Extensions, cfg.Scan.Workers)
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Scanned %d files: %d added, %d updated, %d moved, %d missing, %d errors\n",
				summary.FilesScanned, summary.FilesAdded, summary.FilesUpdated, summary.FilesMoved, summary.FilesMissing, len(summary.Errors))
			for _, e := range summary.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  error: %s: %s\n", e.Path, e.Message)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output the scan summary as JSON")
	return cmd
}
