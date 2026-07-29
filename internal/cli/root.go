package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the musicvideo-manager root command with all
// subcommands attached.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "musicvideo-manager",
		Short: "Organize, enrich, and manage a local music video collection",
	}
	root.AddCommand(newScanCmd())
	root.AddCommand(newMatchCmd())
	root.AddCommand(newRenameCmd())
	root.AddCommand(newNFOCmd())
	root.AddCommand(newVersionCmd(version))
	return root
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}
