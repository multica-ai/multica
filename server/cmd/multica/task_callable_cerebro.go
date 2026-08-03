package main

import (
	"strings"

	"github.com/spf13/cobra"
)

var taskCallableByCommand = map[string]string{
	"issue list": "list_issues", "issue get": "get_issue", "issue search": "search_issues",
	"issue pull-requests": "get_issue", "issue runs": "get_issue", "issue run-messages": "get_issue",
	"issue create": "create_issue", "issue update": "update_issue", "issue status": "update_issue", "issue assign": "update_issue",
	"issue ask": "add_comment", "issue context": "get_issue",
	"issue block": "update_issue", "issue unblock": "update_issue", "issue blocks": "get_issue", "issue related": "update_issue",
	"issue label list": "get_issue", "issue label add": "update_issue", "issue label remove": "update_issue",
	"issue metadata list": "get_issue", "issue metadata get": "get_issue",
	"issue metadata set": "update_issue", "issue metadata delete": "update_issue",
	"issue property list": "get_issue", "issue property set": "update_issue", "issue property unset": "update_issue",
	"issue subscriber list": "get_issue", "issue subscriber add": "subscribe_issue", "issue subscriber remove": "subscribe_issue",
	"issue rerun": "rerun_issue", "issue cancel-task": "rerun_issue",
	"issue comment add": "add_comment", "issue comment list": "list_comments",
	"issue comment delete": "delete_comment", "issue comment move": "move_comments_to_thread",
	"issue comment resolve": "resolve_comment", "issue comment unresolve": "unresolve_comment",
	"issue session list": "list_sessions", "issue session rename": "rename_session",
	"issue session handoff": "handoff_session",

	"artifact create": "create_artifact", "artifact get": "get_artifact",
	"artifact update": "update_artifact", "artifact list": "list_artifacts",
	"artifact delete": "delete_artifact", "artifact set-folder": "set_artifact_folder",
	"artifact folder list": "list_artifact_folders", "artifact folder create": "create_artifact_folder",
	"artifact folder update": "update_artifact_folder", "artifact folder delete": "delete_artifact_folder",
	"document list": "search_artifacts", "document get": "get_artifact",
	"document create": "create_artifact", "document update": "update_artifact", "document delete": "delete_artifact",
	"note read": "get_artifact", "note search": "search_artifacts",
	"attachment upload": "add_attachment",

	"wakeup create": "schedule_wakeup", "wakeup list": "list_wakeups",
	"wakeup get": "list_wakeups", "wakeup cancel": "cancel_wakeup",

	"workflow list": "list_workflows", "workflow get": "get_workflow",
	"workflow create": "create_workflow", "workflow update": "update_workflow",
	"workflow delete": "delete_workflow", "workflow toggle": "toggle_workflow",
	"workflow activate": "activate_workflow", "workflow for-issue": "get_active_workflow",

	"command list": "list_commands", "command get": "get_command",
	"command create": "create_command", "command update": "update_command",
	"command delete": "delete_command",

	"workflow eval list": "list_evals", "workflow eval get": "get_eval",
	"workflow eval create": "create_eval", "workflow eval update": "update_eval",
	"workflow eval delete": "delete_eval", "workflow eval runs": "list_eval_runs",
	"workflow eval record-run": "record_eval_run", "workflow eval bindings": "list_eval_bindings",
	"workflow eval bind": "bind_eval", "workflow eval unbind": "unbind_eval",
}

func taskCallableForCommand(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	lineage := make([]string, 0, 4)
	for current := cmd; current != nil; current = current.Parent() {
		lineage = append(lineage, current.Name())
	}
	for left, right := 0, len(lineage)-1; left < right; left, right = left+1, right-1 {
		lineage[left], lineage[right] = lineage[right], lineage[left]
	}
	if len(lineage) > 0 && lineage[0] == rootCmd.Name() {
		lineage = lineage[1:]
	}
	return taskCallableByCommand[strings.Join(lineage, " ")]
}
