package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/keif/musicvideo-manager/internal/catalog"
	"github.com/keif/musicvideo-manager/internal/config"
	"github.com/keif/musicvideo-manager/internal/rename"
)

func newRenameCmd() *cobra.Command {
	var jsonOutput bool
	var dryRun bool
	var undo bool

	cmd := &cobra.Command{
		Use:   "rename [path]",
		Short: "Rename catalogued videos into a templated layout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun && undo {
				return fmt.Errorf("--dry-run and --undo cannot be used together")
			}

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

			if undo {
				result, err := rename.Undo(cat, absRoot)
				if err != nil {
					return fmt.Errorf("undo: %w", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Restored %d files, %d skipped\n", result.FilesRestored, result.FilesSkipped)
				return nil
			}

			renamer := &rename.Renamer{Catalog: cat, Template: cfg.Rename.Template, DryRun: dryRun}
			plan, err := renamer.Plan()
			if err != nil {
				return fmt.Errorf("plan renames: %w", err)
			}

			if dryRun {
				if jsonOutput {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(plan)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Plan: %d to rename, %d unchanged, %d conflicts\n", len(plan.Moves), plan.Unchanged, len(plan.Conflicts))
				for _, mv := range plan.Moves {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s -> %s\n", mv.OldPath, mv.NewPath)
				}
				for _, c := range plan.Conflicts {
					fmt.Fprintf(cmd.OutOrStdout(), "  conflict: %v -> %s\n", c.OldPaths, c.NewPath)
				}
				return nil
			}

			result, err := renamer.Apply(absRoot, plan)
			if err != nil {
				return fmt.Errorf("apply renames: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed %d files, %d conflicts skipped, %d blocked\n", result.FilesRenamed, len(result.Conflicts), len(result.Blocked))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview renames without moving files")
	cmd.Flags().BoolVar(&undo, "undo", false, "reverse the most recent applied rename run")
	return cmd
}
