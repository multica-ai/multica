package agent

import "strings"

// The receptionist does not spawn a daemon child, so cfg.Env is not inherited.
// Put the non-secret daemon task context in the prompt the existing worker
// already executes. A per-command prefix also works across independent shells.
// Credentials stay in task_token, never in model-visible command prefixes.
func museappTaskPrompt(prompt string, env map[string]string, opts ExecOptions, goos string) string {
	cliPath := env["MULTICA_CLI_PATH"]
	if cliPath == "" {
		return prompt // Standalone Backend callers need not use the Multica CLI.
	}
	keys := []string{
		"MULTICA_CLI_PATH", "MULTICA_DAEMON_PORT", "MULTICA_TASK_ID",
		"MULTICA_AGENT_ID", "MULTICA_AGENT_NAME", "MULTICA_TASK_SLOT",
		"MULTICA_TASK_CONFIG_ROOT", "MULTICA_TASK_WORKSPACES_ROOT",
		"MULTICA_AUTOPILOT_ID", "MULTICA_AUTOPILOT_RUN_ID",
		"MULTICA_QUICK_CREATE_TASK_ID", "MULTICA_QUICK_CREATE_ATTACHMENT_IDS",
		"MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_TYPE", "MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_ID",
	}
	values := make(map[string]string, len(keys)+2)
	for _, key := range keys {
		if value := env[key]; value != "" {
			values[key] = value
		}
	}
	keys = append(keys, "MULTICA_SERVER_URL", "MULTICA_WORKSPACE_ID")
	values["MULTICA_SERVER_URL"] = opts.MulticaServerURL
	values["MULTICA_WORKSPACE_ID"] = opts.MulticaWorkspaceID
	var command strings.Builder
	if goos == "windows" {
		command.WriteString("& { ")
		for _, key := range keys {
			if value := values[key]; value != "" {
				command.WriteString("$env:" + key + " = " + powerShellQuote(value) + "; ")
			}
		}
		command.WriteString("& " + powerShellQuote(cliPath) + " @args }")
	} else {
		command.WriteString("env")
		for _, key := range keys {
			if value := values[key]; value != "" {
				command.WriteString(" " + shellQuote(key+"="+value))
			}
		}
		command.WriteString(" " + shellQuote(cliPath))
	}
	return "[Multica task CLI environment]\n" +
		"For every Multica CLI command in the task or AGENTS.md, including commands shown as multica or $MULTICA_CLI_PATH, use the following prefix and append the original command arguments. Use it on every invocation, even in a new shell. The daemon and worker share these paths.\n\n" +
		"```\n" + command.String() + "\n```\n\n" +
		"In each shell invocation, set MULTICA_TOKEN from this task's request.json task_token as instructed by the receptionist worker setup; do not rely on an earlier shell's exports. Never substitute an owner credential or an installed CLI for this task's credential and CLI.\n\n" + prompt
}
