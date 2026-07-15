package cli

import (
	"github.com/justinwoo280/sentinel/internal/manage"
	"github.com/spf13/cobra"
)

func newManageCommand() *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Interactive management panel (status, service control, config, uninstall)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return manage.Run(role)
		},
	}
	cmd.Flags().StringVar(&role, "role", "",
		"role to manage: agent or master (auto-detected if empty)")
	return cmd
}
