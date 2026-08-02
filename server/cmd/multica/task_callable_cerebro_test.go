package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

func TestTaskCallableForCommandUsesExactRegisteredIdentity(t *testing.T) {
	tests := map[string]struct {
		commandPath string
		want        string
	}{
		"Issue":    {commandPath: "issue create", want: "create_issue"},
		"Assign":   {commandPath: "issue assign", want: "update_issue"},
		"Artifact": {commandPath: "artifact folder delete", want: "delete_artifact_folder"},
		"Wakeup":   {commandPath: "wakeup cancel", want: "cancel_wakeup"},
		"Handoff":  {commandPath: "issue session handoff", want: "handoff_session"},
		"Workflow": {commandPath: "workflow toggle", want: "toggle_workflow"},
		"Command":  {commandPath: "command update", want: "update_command"},
		"Eval":     {commandPath: "workflow eval bind", want: "bind_eval"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cmd := rootCmd
			for _, token := range strings.Fields(tt.commandPath) {
				child, _, err := cmd.Find([]string{token})
				if err != nil || child == cmd {
					t.Fatalf("find %q below %q: %v", token, cmd.Name(), err)
				}
				cmd = child
			}
			if got := taskCallableForCommand(cmd); got != tt.want {
				t.Fatalf("taskCallableForCommand(%q) = %q, want %q", tt.commandPath, got, tt.want)
			}
		})
	}
}

func TestEveryTaskCallableCommandIsRegisteredAndCatalogBound(t *testing.T) {
	for commandPath, callable := range taskCallableByCommand {
		cmd, _, err := rootCmd.Find(strings.Fields(commandPath))
		if err != nil || cmd == rootCmd || cmd.CommandPath() == rootCmd.CommandPath() {
			t.Errorf("registered callable command %q does not resolve in the live CLI tree: %v", commandPath, err)
			continue
		}
		binding, ok := platformcatalog.ByToolBinding(callable)
		if !ok {
			t.Errorf("CLI command %q callable %q has no platformcatalog ToolBindings owner", commandPath, callable)
			continue
		}
		if binding.Key == "" {
			t.Errorf("CLI command %q callable %q resolved to an empty capability", commandPath, callable)
		}
	}
}
