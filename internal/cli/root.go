// Package cli builds the cobra command tree for the sentinel binary.
package cli

import (
	"github.com/spf13/cobra"
)

// Version is the build version, overridable at link time via
//
//	-ldflags "-X github.com/justinwoo280/sentinel/internal/cli.Version=..."
var Version = "dev"

// NewRootCommand assembles the full command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "sentinel",
		Short:         "sentinel — IP reputation keepalive agent & control plane",
		Long:          "sentinel is a single binary running either an edge Agent or the control-plane Master.\nSee DESIGN.md for architecture.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newAgentCommand(),
		newMasterCommand(),
		newInstallCommand(),
		newManageCommand(),
		newVersionCommand(),
	)
	return root
}
