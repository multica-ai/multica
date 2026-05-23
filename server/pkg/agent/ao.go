package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// aoBlockedArgs are flags hardcoded by the daemon that must not be overridden
// by user-configured custom_args. AO is a factory-manager dispatcher here, not
// an interactive terminal opener, so the backend owns the prompt and refuses
// to open a terminal window from daemon context.
var aoBlockedArgs = map[string]blockedArgMode{
	"--prompt": blockedWithValue,
	"--open":   blockedStandalone,
}

// aoBackend implements Backend by spawning `ao spawn --prompt <prompt>` and
// returning dispatch evidence to Multica. AO itself owns the long-running
// factory-manager session; Multica stores the AO session id/output as task
// evidence rather than pretending AO speaks Codex app-server or ACP.
type aoBackend struct {
	cfg Config
}

func (b *aoBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "ao"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("ao executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	aoCwd, err := resolveAOWorkdir(opts.Cwd, b.cfg.Env)
	if err != nil {
		cancel()
		return nil, err
	}
	preparedPrompt, err := prepareAOPrompt(prompt, opts.SystemPrompt, aoCwd)
	if err != nil {
		cancel()
		return nil, err
	}
	args := buildAOArgs(preparedPrompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = 10 * time.Second
	if aoCwd != "" {
		cmd.Dir = aoCwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args, "cwd", aoCwd)

	msgCh := make(chan Message, 16)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		start := time.Now()
		trySend(msgCh, Message{Type: MessageStatus, Status: "dispatching to ao"})

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		duration := time.Since(start)

		stdoutText := strings.TrimSpace(stdout.String())
		stderrText := strings.TrimSpace(stderr.String())
		combined := combineAOOutput(stdoutText, stderrText)
		sessionID := extractAOSessionID(combined)

		status := "completed"
		errMsg := ""
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			status = "timeout"
			errMsg = fmt.Sprintf("ao timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			status = "aborted"
			errMsg = "execution cancelled"
		case err != nil:
			status = "failed"
			errMsg = strings.TrimSpace(fmt.Sprintf("ao spawn failed: %v\n%s", err, stderrText))
		}

		output := formatAOResultOutput(combined, sessionID)
		if output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: output})
		}

		b.cfg.Logger.Info("ao dispatch finished",
			"status", status,
			"session_id", sessionID,
			"duration", duration.Round(time.Millisecond).String(),
		)

		resCh <- Result{
			Status:     status,
			Output:     output,
			Error:      errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func buildAOArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"spawn"}
	customArgs := filterCustomArgs(opts.CustomArgs, aoBlockedArgs, logger)
	if opts.Model != "" && opts.Model != "default" && !customArgsContains(customArgs, "--agent") {
		args = append(args, "--agent", opts.Model)
	}
	args = append(args, "--prompt", prompt)
	args = append(args, customArgs...)
	return args
}

const aoInlinePromptLimit = 3900

func prepareAOPrompt(prompt, systemPrompt, aoCwd string) (string, error) {
	fullPrompt := prompt
	if systemPrompt != "" {
		fullPrompt = systemPrompt + "\n\n" + prompt
	}
	if len([]rune(fullPrompt)) <= aoInlinePromptLimit {
		return fullPrompt, nil
	}
	if aoCwd == "" {
		return truncateRunes(fullPrompt, aoInlinePromptLimit), nil
	}

	dispatchDir := filepath.Join(aoCwd, ".multica-ao-dispatches")
	if err := os.MkdirAll(dispatchDir, 0o700); err != nil {
		return "", fmt.Errorf("create AO dispatch directory: %w", err)
	}
	dispatchFile := filepath.Join(dispatchDir, "multica-dispatch-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".md")
	if err := os.WriteFile(dispatchFile, []byte(fullPrompt), 0o600); err != nil {
		return "", fmt.Errorf("write AO dispatch prompt: %w", err)
	}

	preview := truncateRunes(fullPrompt, 1400)
	return fmt.Sprintf(`Multica assigned a task whose full brief exceeds AO's inline prompt limit.

Read the full brief from this local file first:
%s

Execute that full brief. Do not rely only on this preview.

Preview:
%s`, dispatchFile, preview), nil
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func resolveAOWorkdir(taskCwd string, env map[string]string) (string, error) {
	for _, key := range []string{"MULTICA_AO_WORKDIR", "AO_WORKDIR"} {
		if configured := strings.TrimSpace(envOrProcess(env, key)); configured != "" {
			dir, ok := findAOConfigDir(configured)
			if !ok {
				return "", fmt.Errorf("%s=%q does not contain agent-orchestrator.yaml", key, configured)
			}
			return dir, nil
		}
	}

	if dir, ok := findAOConfigDir(taskCwd); ok {
		return dir, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir, ok := findAOConfigDir(cwd); ok {
			return dir, nil
		}
	}
	return taskCwd, nil
}

func envOrProcess(env map[string]string, key string) string {
	if env != nil {
		if value, ok := env[key]; ok {
			return value
		}
	}
	return os.Getenv(key)
}

func findAOConfigDir(start string) (string, bool) {
	if start == "" {
		return "", false
	}
	current, err := filepath.Abs(start)
	if err != nil {
		current = start
	}
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		if hasAOConfig(current) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func hasAOConfig(dir string) bool {
	for _, name := range []string{"agent-orchestrator.yaml", "agent-orchestrator.yml"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func combineAOOutput(stdoutText, stderrText string) string {
	switch {
	case stdoutText == "":
		return stderrText
	case stderrText == "":
		return stdoutText
	default:
		return stdoutText + "\n\n[ao stderr]\n" + stderrText
	}
}

func formatAOResultOutput(raw, sessionID string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" && sessionID == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("AO dispatch accepted.")
	if sessionID != "" {
		b.WriteString("\nAO session: ")
		b.WriteString(sessionID)
	}
	if raw != "" {
		b.WriteString("\n\nAO output:\n")
		b.WriteString(raw)
	}
	return b.String()
}

var aoSessionIDPattern = regexp.MustCompile(`(?i)\b(?:session|session_id|sessionId|spawned|created)\s*[:=]?\s*([a-z][a-z0-9_-]*-\d+)\b`)

func extractAOSessionID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if id := extractAOSessionIDFromJSON([]byte(raw)); id != "" {
		return id
	}
	if m := aoSessionIDPattern.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	// AO session names commonly look like cg-1 or cg-rev-1. Keep this as a
	// fallback only after label/JSON parsing so ordinary prose does not win over
	// structured evidence.
	fallback := regexp.MustCompile(`\b([a-z][a-z0-9_-]*-\d+)\b`)
	if m := fallback.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	return ""
}

func extractAOSessionIDFromJSON(data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	return findAOSessionIDValue(v)
}

func findAOSessionIDValue(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"sessionId", "session_id", "session", "name", "id"} {
			if s, ok := x[key].(string); ok {
				if id := extractAOSessionID(s); id != "" {
					return id
				}
			}
		}
		for _, child := range x {
			if id := findAOSessionIDValue(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range x {
			if id := findAOSessionIDValue(child); id != "" {
				return id
			}
		}
	case string:
		if m := aoSessionIDPattern.FindStringSubmatch(x); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}
