package cli

import (
	"log/slog"

	"github.com/justinwoo280/sentinel/internal/agent"
	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/spf13/cobra"
)

func newAgentCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run as an edge Agent (keepalive + outbound control connection)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadAgent(configPath)
			if err != nil {
				return err
			}
			log := slog.Default()
			a, err := agent.New(cfg, configPath, log)
			if err != nil {
				return err
			}
			log.Info("agent starting",
				"node", cfg.Node.Name, "region", cfg.Region.Code,
				"ip", a.PublicIP())
			return a.Start(cmd.Context())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c",
		"/etc/sentinel/agent.yaml", "path to agent config file")
	return cmd
}
