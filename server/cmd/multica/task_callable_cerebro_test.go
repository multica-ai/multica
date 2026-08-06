package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	"github.com/spf13/cobra"
)

func TestTaskCallableForCommandUsesExactRegisteredIdentity(t *testing.T) {
	tests := map[string]struct {
		commandPath string
		want        string
	}{
		"Issue":        {commandPath: "issue create", want: "create_issue"},
		"Assign":       {commandPath: "issue assign", want: "update_issue"},
		"PullRequests": {commandPath: "issue pull-requests", want: "get_issue"},
		"RunMessages":  {commandPath: "issue run-messages", want: "get_issue"},
		"MetadataList": {commandPath: "issue metadata list", want: "get_issue"},
		"MetadataSet":  {commandPath: "issue metadata set", want: "update_issue"},
		"Subscriber":   {commandPath: "issue subscriber add", want: "subscribe_issue"},
		"Rerun":        {commandPath: "issue rerun", want: "rerun_issue"},
		"Artifact":     {commandPath: "artifact folder delete", want: "delete_artifact_folder"},
		"Wakeup":       {commandPath: "wakeup cancel", want: "cancel_wakeup"},
		"Autopilot":    {commandPath: "autopilot update", want: "update_autopilot"},
		"Handoff":      {commandPath: "issue session handoff", want: "handoff_session"},
		"Workflow":     {commandPath: "workflow toggle", want: "toggle_workflow"},
		"Command":      {commandPath: "command update", want: "update_command"},
		"Eval":         {commandPath: "workflow eval bind", want: "bind_eval"},
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

func TestEveryTaskGatedCLICommandHasCallableMapping(t *testing.T) {
	for _, commandPath := range []string{"issue", "artifact", "document", "wakeup", "command", "workflow"} {
		cmd, _, err := rootCmd.Find(strings.Fields(commandPath))
		if err != nil || cmd == rootCmd {
			t.Fatalf("find task-gated CLI root %q: %v", commandPath, err)
		}
		assertTaskCallableTree(t, cmd, map[string]bool{"workflow hook": true})
	}

	for _, commandPath := range []string{"note read", "note search", "attachment upload"} {
		cmd, _, err := rootCmd.Find(strings.Fields(commandPath))
		if err != nil || cmd == rootCmd {
			t.Fatalf("find task-gated CLI command %q: %v", commandPath, err)
		}
		if got := taskCallableForCommand(cmd); got == "" {
			t.Errorf("task-gated CLI command %q has no callable mapping", commandPath)
		}
	}
}

func assertTaskCallableTree(t *testing.T, root *cobra.Command, excluded map[string]bool) {
	t.Helper()
	for _, cmd := range root.Commands() {
		commandPath := strings.TrimPrefix(cmd.CommandPath(), rootCmd.Name()+" ")
		if excluded[commandPath] {
			continue
		}
		if cmd.Runnable() && taskCallableForCommand(cmd) == "" {
			t.Errorf("task-gated CLI command %q has no callable mapping", commandPath)
		}
		assertTaskCallableTree(t, cmd, excluded)
	}
}
