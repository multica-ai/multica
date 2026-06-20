package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// dirgeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. These own non-interactive mode,
// output parsing, session bookkeeping, and permission prompts.
var dirgeBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedStandalone,
	"--print":           blockedStandalone,
	"--output-format":   blockedWithValue,
	"--session":         blockedWithValue,
	"-s":                blockedWithValue,
	"--model":           blockedWithValue,
	"--max-agent-turns": blockedWithValue,
	"--accept-all":      blockedStandalone,
	"--yolo":            blockedStandalone,
	"--restrictive":     blockedStandalone,
	"-R":                blockedStandalone,
	"--auto-confirm":    blockedWithValue,
	"--continue":        blockedStandalone,
	"-c":                blockedStandalone,
	"--resume":          blockedWithValue,
	"-r":                blockedWithValue,
	"--no-session":      blockedStandalone,
}

// dirgeBackend implements Backend by spawning Dirge in headless print mode and
// parsing the final JSON result object from stdout.
type dirgeBackend struct {
	cfg Config
}

func (b *dirgeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "dirge"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("dirge executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	sessionID := resolveDirgeSessionID(opts.ResumeSessionID)
	args := buildDirgeArgs(prompt, sessionID, opts, b.cfg.Logger)

	configDir, cleanupConfig, err := prepareDirgeConfig(opts.McpConfig, b.cfg.Env)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("dirge: invalid mcp_config: %w", err)
	}

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	if configDir != "" {
		env = upsertEnv(env, "DIRGE_CONFIG_DIR", configDir)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanupConfig()
		return nil, fmt.Errorf("dirge stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[dirge:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if opts.SystemPrompt != "" {
		b.cfg.Logger.Debug("dirge ignoring ExecOptions.SystemPrompt; using cwd-scoped context files", "cwd", opts.Cwd)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		cleanupConfig()
		return nil, fmt.Errorf("start dirge: %w", err)
	}

	b.cfg.Logger.Info("dirge started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model, "session_id", sessionID)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer cleanupConfig()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

		stdoutBytes, readErr := io.ReadAll(stdout)
		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		stdoutText := string(stdoutBytes)
		finalStatus := "completed"
		finalOutput := ""
		finalError := ""

		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			finalStatus = "timeout"
			finalError = fmt.Sprintf("dirge timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			finalStatus = "aborted"
			finalError = "execution cancelled"
		default:
			if readErr != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("read dirge stdout: %v", readErr)
			} else {
				result, err := parseDirgeResult(stdoutText)
				if err != nil {
					finalStatus = "failed"
					if waitErr != nil {
						finalError = fmt.Sprintf("dirge exited with error: %v", waitErr)
					} else {
						finalError = err.Error()
					}
				} else {
					finalOutput = result.Result
					if finalOutput != "" {
						trySend(msgCh, Message{Type: MessageText, Content: finalOutput})
					}
					if result.IsError || strings.HasPrefix(result.Subtype, "error") {
						finalStatus = "failed"
						finalError = result.Result
						if finalError == "" {
							finalError = "dirge failed"
						}
						if result.Subtype != "" {
							finalError = fmt.Sprintf("%s: %s", result.Subtype, finalError)
						}
					} else if waitErr != nil {
						finalStatus = "failed"
						finalError = fmt.Sprintf("dirge exited with error: %v", waitErr)
					}
				}
			}
		}

		if finalError != "" {
			finalError = withAgentStderr(finalError, "dirge", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("dirge finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			// Dirge's headless JSON result includes an internal run id, not
			// the CLI --session value needed for the next resume. Return the
			// Multica-owned session id we passed to Dirge.
			SessionID: sessionID,
			Usage:     map[string]TokenUsage{},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func buildDirgeArgs(prompt, sessionID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"--print",
		"--yolo",
		"--auto-confirm", "yes",
		"--session", sessionID,
		"--output-format", "json",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-agent-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, dirgeBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, dirgeBlockedArgs, logger)...)
	args = append(args, "--", prompt)
	return args
}

func resolveDirgeSessionID(resumeID string) string {
	if strings.TrimSpace(resumeID) != "" {
		return resumeID
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}
	return "multica-" + hex.EncodeToString(b[:])
}

type dirgeHeadlessResult struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	IsError    bool   `json:"is_error"`
	Result     string `json:"result"`
	SessionID  string `json:"session_id"`
	DurationMs int64  `json:"duration_ms"`
}

func parseDirgeResult(stdout string) (dirgeHeadlessResult, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return dirgeHeadlessResult{}, fmt.Errorf("dirge produced no stdout")
	}
	if result, ok := decodeDirgeResult(trimmed); ok {
		return result, nil
	}
	lines := strings.Split(trimmed, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if result, ok := decodeDirgeResult(line); ok {
			return result, nil
		}
	}
	return dirgeHeadlessResult{}, fmt.Errorf("dirge did not emit a parseable JSON result")
}

func decodeDirgeResult(s string) (dirgeHeadlessResult, bool) {
	var result dirgeHeadlessResult
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return dirgeHeadlessResult{}, false
	}
	if result.Type != "result" {
		return dirgeHeadlessResult{}, false
	}
	return result, true
}

func prepareDirgeConfig(raw json.RawMessage, extraEnv map[string]string) (string, func(), error) {
	if !hasManagedDirgeMcpConfig(raw) {
		return "", func() {}, nil
	}

	servers, err := extractDirgeMcpServers(raw)
	if err != nil {
		return "", func() {}, err
	}

	base := map[string]json.RawMessage{}
	if basePath := dirgeBaseConfigPath(extraEnv); basePath != "" {
		data, err := os.ReadFile(basePath)
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &base); err != nil {
				return "", func() {}, fmt.Errorf("parse existing Dirge config %s: %w", basePath, err)
			}
		} else if err != nil && !os.IsNotExist(err) {
			return "", func() {}, fmt.Errorf("read existing Dirge config %s: %w", basePath, err)
		}
	}

	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return "", func() {}, err
	}
	base["mcp_servers"] = serversRaw
	delete(base, "mcpServers")

	dir, err := os.MkdirTemp("", "multica-dirge-config-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp Dirge config dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	data, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write Dirge config: %w", err)
	}
	return dir, cleanup, nil
}

func hasManagedDirgeMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return !bytes.Equal(trimmed, []byte("null"))
}

func extractDirgeMcpServers(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse mcp_config json: %w", err)
	}

	serversRaw, ok := root["mcpServers"]
	if !ok {
		serversRaw, ok = root["mcp_servers"]
	}
	servers := map[string]json.RawMessage{}
	if !ok || len(bytes.TrimSpace(serversRaw)) == 0 || bytes.Equal(bytes.TrimSpace(serversRaw), []byte("null")) {
		return servers, nil
	}
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return nil, fmt.Errorf("parse mcpServers: %w", err)
	}
	for name, rawEntry := range servers {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			return nil, fmt.Errorf("mcp_servers.%s must be a JSON object: %w", name, err)
		}
		if len(entry) == 0 {
			return nil, fmt.Errorf("mcp_servers.%s must declare either `command` or `url`", name)
		}
		if !hasNonEmptyJSONText(entry["command"]) && !hasNonEmptyJSONText(entry["url"]) {
			return nil, fmt.Errorf("mcp_servers.%s must declare either `command` or `url`", name)
		}
	}
	return servers, nil
}

func hasNonEmptyJSONText(raw json.RawMessage) bool {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return strings.TrimSpace(s) != ""
}

func dirgeBaseConfigPath(extraEnv map[string]string) string {
	if dir := strings.TrimSpace(extraEnv["DIRGE_CONFIG_DIR"]); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	if dir := strings.TrimSpace(os.Getenv("DIRGE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "dirge", "config.json")
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, prefix+value)
}
