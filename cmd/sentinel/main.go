// Command sentinel is the single binary that runs both the Agent (edge
// node) and the Master (control plane) roles, selected by subcommand.
//
// See DESIGN.md for the full architecture.
package main

import (
	"fmt"
	"os"

	"github.com/justinwoo280/sentinel/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
