package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/justinwoo280/sentinel/internal/install"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newInstallCommand() *cobra.Command {
	var configPath string
	var role string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Interactive installer (choose agent or master role)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := strings.ToLower(strings.TrimSpace(role))
			if r == "" {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf(
						"install: no --role given and stdin is not a terminal to prompt for one.\n" +
							"Pass --role agent or --role master explicitly, or run this in an " +
							"interactive session (see scripts/install.sh for the recommended " +
							"one-line install command)")
				}
				r = promptRole()
			}
			switch r {
			case "agent":
				if configPath == "" {
					configPath = "/etc/sentinel/agent.yaml"
				}
				return install.Run(configPath)
			case "master":
				mp := configPath
				if mp == "" {
					mp = "/etc/sentinel/master.yaml"
				}
				// Master install = init keypair/config + install service.
				return masterInit(mp, true)
			default:
				return fmt.Errorf("unknown role %q (want agent or master)", r)
			}
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"path to config file (defaults per role)")
	cmd.Flags().StringVar(&role, "role", "",
		"install role: agent or master (prompted if empty)")
	return cmd
}

// promptRole asks the user to choose an install role.
func promptRole() string {
	fmt.Println("Select install role:")
	fmt.Println("  1. Agent  (edge node — keepalive + outbound control connection)")
	fmt.Println("  2. Master (control plane — Telegram bot + EWP server)")
	fmt.Print("\nEnter choice [1-2]: ")
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "1", "agent":
			return "agent"
		case "2", "master":
			return "master"
		}
	}
	return ""
}
