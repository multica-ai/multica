package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A group command's Long once went unrendered because subHelpTemplate printed
// only Short, and rules written there never reached anyone. Every Long set on
// a listed command must show up in its --help. Unavailable commands (cobra's
// own help and hidden completion commands) keep cobra's templates.
func TestHelpRendersLongOnEveryCommand(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, c := range cmd.Commands() {
			if !c.IsAvailableCommand() {
				continue
			}
			if c.Long != "" {
				if help := renderHelp(t, c); !strings.Contains(help, c.Long) {
					t.Errorf("%s --help does not render its Long; rendered:\n%s", c.CommandPath(), help)
				}
			}
			walk(c)
		}
	}
	walk(rootCmd)
}
