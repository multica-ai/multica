package agent

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMuseAppTaskPromptRunsCLIWithTaskContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell execution")
	}
	cliPath := filepath.Join(t.TempDir(), "task cli's binary")
	script := "#!/bin/sh\nprintf '%s\\n' \"$MULTICA_DAEMON_PORT\" \"$MULTICA_TASK_ID\" \"$MULTICA_AGENT_NAME\" \"$MULTICA_TASK_CONFIG_ROOT\" \"$MULTICA_SERVER_URL\" \"$MULTICA_WORKSPACE_ID\" \"$MULTICA_TOKEN\" \"$*\"\n"
	writeTestExecutable(t, cliPath, []byte(script))
	env := map[string]string{
		"MULTICA_CLI_PATH": cliPath, "MULTICA_DAEMON_PORT": "12345",
		"MULTICA_TASK_ID": "task-current", "MULTICA_AGENT_NAME": "Muse's agent $(exit 99); literal",
		"MULTICA_TASK_CONFIG_ROOT": filepath.Join(t.TempDir(), "task's config"),
		"MULTICA_MUSEAPP_ENDPOINT": "http://untrusted.invalid", "MULTICA_MUSEAPP_TOKEN": "private-receptionist-token",
		"MULTICA_TOKEN": "private-config-token", "OTHER_SECRET": "private-custom-secret",
	}
	opts := ExecOptions{TaskToken: "private-task-token", MulticaServerURL: "http://127.0.0.1:8080", MulticaWorkspaceID: "ws-current"}
	prompt := museappTaskPrompt("check out the repository", env, opts, "linux")
	for _, secret := range []string{opts.TaskToken, env["MULTICA_TOKEN"], env["MULTICA_MUSEAPP_TOKEN"], env["OTHER_SECRET"], env["MULTICA_MUSEAPP_ENDPOINT"]} {
		if strings.Contains(prompt, secret) {
			t.Fatal("credential or unapproved config leaked into task prompt")
		}
	}
	_, rest, ok := strings.Cut(prompt, "```\n")
	if !ok {
		t.Fatal("missing CLI prefix")
	}
	prefix, _, ok := strings.Cut(rest, "\n```")
	if !ok {
		t.Fatal("unterminated CLI prefix")
	}
	cmd := exec.Command("sh", "-c", prefix+" repo checkout https://example.invalid/repo.git")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "MULTICA_TOKEN=mat_current_task"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task CLI: %v: %s", err, output)
	}
	want := strings.Join([]string{env["MULTICA_DAEMON_PORT"], env["MULTICA_TASK_ID"], env["MULTICA_AGENT_NAME"], env["MULTICA_TASK_CONFIG_ROOT"], opts.MulticaServerURL, opts.MulticaWorkspaceID, "mat_current_task", "repo checkout https://example.invalid/repo.git", ""}, "\n")
	if string(output) != want {
		t.Fatalf("task CLI environment = %q, want %q", output, want)
	}
}

func TestMuseAppTaskPromptPowerShellContext(t *testing.T) {
	prompt := museappTaskPrompt("task", map[string]string{"MULTICA_CLI_PATH": `C:\User's Files\multica.exe`, "MULTICA_DAEMON_PORT": "19514"}, ExecOptions{}, "windows")
	if !strings.Contains(prompt, `$env:MULTICA_DAEMON_PORT = '19514'`) || !strings.Contains(prompt, `& 'C:\User''s Files\multica.exe' @args }`) {
		t.Fatalf("missing quoted PowerShell context: %s", prompt)
	}
}
