package agent

import (
	"bufio"
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

// grokBackend implements Backend by spawning the xAI Grok Build CLI in
// headless mode with --output-format streaming-json. The wire format is
// newline-delimited JSON objects with a top-level "type" field:
//
//	{"type":"thought","data":"..."}
//	{"type":"text","data":"..."}
//	{"type":"end","stopReason":"EndTurn","sessionId":"...","requestId":"..."}
//
// See https://docs.x.ai/build/cli/headless-scripting
type grokBackend struct {
	cfg Config
}

// grokBlockedArgs are flags owned by the daemon that must not be overridden
// by user-configured custom_args.
var grokBlockedArgs = map[string]blockedArgMode{
	"-p":                  blockedWithValue,
	"--single":            blockedWithValue,
	"--print":             blockedWithValue,
	"--output-format":     blockedWithValue,
	"--always-approve":    blockedStandalone,
	"--no-auto-update":    blockedStandalone,
	"-r":                  blockedWithValue,
	"--resume":            blockedWithValue,
	"-c":                  blockedStandalone,
	"--continue":          blockedStandalone,
	"-s":                  blockedWithValue,
	"--session-id":        blockedWithValue,
	"agent":               blockedStandalone,
	"stdio":               blockedStandalone,
	"--permission-mode":   blockedWithValue,
	"--system-prompt-override": blockedWithValue,
}

type grokEventState struct {
	output      strings.Builder
	sessionID   string
	finalStatus string
	finalError  string
	endSeen     bool
}

func newGrokEventState() *grokEventState {
	return &grokEventState{finalStatus: "completed"}
}

type grokStreamEvent struct {
	Type       string `json:"type"`
	Data       string `json:"data"`
	StopReason string `json:"stopReason"`
	SessionID  string `json:"sessionId"`
	RequestID  string `json:"requestId"`
}

// handleGrokEvent processes one streaming-json line and returns messages to
// emit. Extracted so tests exercise the same parsing path as production.
func handleGrokEvent(evt grokStreamEvent, st *grokEventState) []Message {
	var msgs []Message

	switch evt.Type {
	case "thought":
		if evt.Data != "" {
			msgs = append(msgs, Message{Type: MessageThinking, Content: evt.Data})
		}
	case "text":
		if evt.Data != "" {
			st.output.WriteString(evt.Data)
			msgs = append(msgs, Message{Type: MessageText, Content: evt.Data})
		}
	case "error":
		if evt.Data != "" {
			st.finalStatus = "failed"
			if st.finalError == "" {
				st.finalError = evt.Data
			}
			msgs = append(msgs, Message{Type: MessageError, Content: evt.Data})
		}
	case "end":
		st.endSeen = true
		if evt.SessionID != "" {
			st.sessionID = evt.SessionID
		}
		if reason := strings.TrimSpace(evt.StopReason); reason != "" && !grokStopReasonOK(reason) {
			st.finalStatus = "failed"
			if st.finalError == "" {
				st.finalError = "grok stopped: " + reason
			}
		}
	}

	return msgs
}

func grokStopReasonOK(reason string) bool {
	switch strings.ToLower(reason) {
	case "endturn", "end_turn", "stop", "complete", "completed":
		return true
	default:
		return false
	}
}

func (b *grokBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "grok"
	}
	if _, err := exec.LookPath(execName); err != nil {
		return nil, fmt.Errorf("grok executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildGrokArgs(prompt, opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execName, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execName, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[grok:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start grok: %w", err)
	}

	b.cfg.Logger.Info("grok started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		st := newGrokEventState()

		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt grokStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				slog.Warn("grok event parse failed", "err", err, "line", line)
				continue
			}

			for _, m := range handleGrokEvent(evt, st) {
				trySend(msgCh, m)
			}
			if st.endSeen {
				// Grok Build can emit the terminal end event while the worker
				// process keeps running. Treat end as the protocol boundary so
				// the daemon can report completion (same pattern as cursor.go).
				cancel()
				break
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("grok stdout scanner error", "err", err)
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			st.finalStatus = "timeout"
			st.finalError = fmt.Sprintf("grok timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled && st.finalStatus == "completed" && !st.endSeen {
			st.finalStatus = "aborted"
			st.finalError = "execution cancelled"
		} else if exitErr != nil && st.finalStatus == "completed" && !st.endSeen {
			st.finalStatus = "failed"
			st.finalError = fmt.Sprintf("grok exited with error: %v", exitErr)
		}
		if st.finalError != "" {
			st.finalError = withAgentStderr(st.finalError, "grok", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("grok finished", "pid", cmd.Process.Pid, "status", st.finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     st.finalStatus,
			Output:     st.output.String(),
			Error:      st.finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  st.sessionID,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildGrokArgs assembles argv for a one-shot headless grok invocation:
//
//	grok -p "<prompt>" --output-format streaming-json --always-approve
//	     [--cwd <dir>] [--model <model>] [--resume <session-id>] [--reasoning-effort <level>]
func buildGrokArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "streaming-json",
		"--always-approve",
	}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-r", opts.ResumeSessionID)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--reasoning-effort", opts.ThinkingLevel)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, grokBlockedArgs, logger)...)
	return args
}

var (
	grokModelLineRe = regexp.MustCompile(`^\s*[\*\-]\s+(\S+)(?:\s+\(default\))?\s*$`)
	grokModelIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// IsGrokBuildCLI reports whether executablePath is the xAI Grok Build binary.
// Unrelated third-party CLIs also install a `grok` command; they must be
// rejected so the daemon does not register the wrong runtime.
func IsGrokBuildCLI(ctx context.Context, executablePath string) bool {
	if strings.TrimSpace(executablePath) == "" {
		return false
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "--help")
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	s := string(out)
	return strings.Contains(s, "Grok Build") && strings.Contains(s, "--output-format")
}

// ResolveGrokBuildExecutable finds the xAI Grok Build binary. When cmd is a
// bare name it prefers the official ~/.grok/bin/grok install over unrelated
// `grok` binaries that may appear earlier on PATH (e.g. npm global installs).
// An absolute or relative cmd that fails validation is not silently replaced.
func ResolveGrokBuildExecutable(ctx context.Context, cmd string, extraCandidates ...string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = "grok"
	}
	pinned := strings.ContainsAny(cmd, "/\\")
	if pinned {
		path := cmd
		if resolved, err := exec.LookPath(cmd); err == nil {
			path = resolved
		}
		if IsGrokBuildCLI(ctx, path) {
			return path, true
		}
		return "", false
	}

	seen := make(map[string]struct{})
	var candidates []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	for _, p := range grokBuildInstallPaths() {
		add(p)
	}
	for _, p := range extraCandidates {
		add(p)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		add(filepath.Join(dir, cmd))
	}

	for _, path := range candidates {
		if !isExecutableFile(path) {
			continue
		}
		if IsGrokBuildCLI(ctx, path) {
			return path, true
		}
	}
	return "", false
}

func grokBuildInstallPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".grok", "bin", "grok")}
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func grokModelIDValid(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") ||
		strings.Contains(id, "node_modules") || strings.HasPrefix(id, "file:") {
		return false
	}
	return grokModelIDRe.MatchString(id)
}

// discoverGrokModels runs `grok models` and parses the human-readable catalog.
// On any failure it falls back to a short static list so the UI still works.
func discoverGrokModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "grok"
	}
	if path, ok := ResolveGrokBuildExecutable(ctx, executablePath); ok {
		executablePath = path
	} else if _, err := exec.LookPath(executablePath); err != nil {
		return grokStaticModels(), nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, executablePath, "models")
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return grokStaticModels(), nil
	}
	raw := string(out)
	if !strings.Contains(raw, "Available models:") {
		return grokStaticModels(), nil
	}

	var models []Model
	var defaultID string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Default model:") {
			defaultID = strings.TrimSpace(strings.TrimPrefix(line, "Default model:"))
			continue
		}
		m := grokModelLineRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		id := strings.TrimSpace(m[1])
		if !grokModelIDValid(id) {
			continue
		}
		model := Model{ID: id, Label: id, Provider: "xai"}
		if id == defaultID || strings.Contains(line, "(default)") {
			model.Default = true
		}
		models = append(models, model)
	}
	if len(models) == 0 {
		return grokStaticModels(), nil
	}
	if defaultID == "" {
		for i := range models {
			if models[i].Default {
				defaultID = models[i].ID
				break
			}
		}
	}
	return models, nil
}

func grokStaticModels() []Model {
	return []Model{
		{ID: "grok-composer-2.5-fast", Label: "Composer 2.5", Provider: "xai", Default: true},
		{ID: "grok-build", Label: "Grok Build", Provider: "xai"},
	}
}