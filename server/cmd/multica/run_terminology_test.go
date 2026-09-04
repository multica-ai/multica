package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunTerminologyInCLIHelp(t *testing.T) {
	tests := []struct {
		name    string
		got     string
		want    string
		notWant string
	}{
		{name: "run messages argument", got: issueRunMessagesCmd.Use, want: "<run-id>", notWant: "<task-id>"},
		{name: "cancel argument", got: issueCancelTaskCmd.Use, want: "<run-id>", notWant: "<task-id>"},
		{name: "cancel summary", got: issueCancelTaskCmd.Short, want: "in-progress or queued run", notWant: "running or queued run"},
		{name: "cancel scope", got: flagHelp(t, issueCancelTaskCmd, "issue"), want: "short run ID", notWant: "short task ID"},
		{name: "run messages scope", got: flagHelp(t, issueRunMessagesCmd, "issue"), want: "short run ID", notWant: "short task ID"},
		{name: "comment parent", got: flagHelp(t, issueCommentAddCmd, "parent"), want: "agent run", notWant: "agent task"},
		{name: "project execution mode", got: flagHelp(t, projectResourceAddCmd, "execution-mode"), want: "how runs share", notWant: "how tasks share"},
		{name: "daemon start", got: daemonStartCmd.Long, want: "polls for runs", notWant: "polls for tasks"},
		{name: "daemon disk usage", got: daemonDiskUsageCmd.Long, want: "per-run", notWant: "per-task"},
		{name: "user profile", got: userProfileCmd.Long, want: "start a run", notWant: "pick up a task"},
		{name: "autopilot description", got: flagHelp(t, autopilotCreateCmd, "description"), want: "run prompt", notWant: "task prompt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.got, tt.want) {
				t.Fatalf("help = %q, want it to contain %q", tt.got, tt.want)
			}
			if strings.Contains(tt.got, tt.notWant) {
				t.Fatalf("help = %q, must not contain legacy product term %q", tt.got, tt.notWant)
			}
		})
	}
}

func flagHelp(t *testing.T, cmd *cobra.Command, name string) string {
	t.Helper()
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("flag --%s is not registered on %s", name, cmd.Name())
	}
	return flag.Usage
}
