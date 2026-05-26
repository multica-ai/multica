package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// claudeBackend implements Backend by spawning the Claude Code CLI
// with --output-format stream-json.
type claudeBackend struct {
	cfg Config
}

func (b *claudeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildClaudeArgs(opts, b.cfg.Logger)

	// If the caller provided an MCP config, write it to a temp file and pass
	// --mcp-config <path> so the agent uses a controlled set of MCP servers
	// instead of inheriting from the outer Claude Code session.
	var mcpConfigPath string
	var mcpFileCleanup func() // non-nil while this function owns the temp file
	if len(opts.McpConfig) > 0 {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			cancel()
			return nil, err
		}
		mcpConfigPath = path
		mcpFileCleanup = func() { os.Remove(mcpConfigPath) }
		args = append(args, "--mcp-config", mcpConfigPath)
	}
	// Clean up the temp file if we return before the goroutine takes ownership.
	defer func() {
		if mcpFileCleanup != nil {
			mcpFileCleanup()
		}
	}()

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	// Skill isolation. The platform default is "merge" — the CLI walks
	// `~/.claude/` directly, including its `skills/`, so existing personal
	// workflows that rely on locally installed Claude skills keep working
	// out of the box. The agent owner can opt into "ignore" when a shared
	// agent must be hardened against a broken local skill on one operator's
	// machine, which otherwise crashes the CLI before it reads stdin
	// (GitHub #3052: silent "broken pipe" exits).
	//
	// In "ignore" mode we point CLAUDE_CONFIG_DIR at a per-run scratch dir
	// that mirrors `~/.claude/` as symlinks — except for `skills/`, which
	// is omitted so the CLI cannot discover the host user's
	// `~/.claude/skills/`. Everything else under `~/.claude/` — including
	// the Linux/Windows `.credentials.json` login token, `settings.json`,
	// `agents/`, `commands/`, `plugins/`, etc. — is symlinked through so
	// Claude authentication and global config keep working without an
	// `ANTHROPIC_API_KEY`. Workspace skills (`{cwd}/.claude/skills/`) load
	// from cwd regardless and are not affected by either mode.
	isolateClaudeConfig := opts.SkillsLocal == "ignore"
	var claudeConfigDir string
	var claudeConfigCleanup func()
	if isolateClaudeConfig {
		// Resolve the *effective* Claude config source before we strip
		// CLAUDE_CONFIG_DIR from the child env in buildClaudeEnv.
		// Precedence: agent custom_env > parent process env > `~/.claude`.
		// Mirroring `~/.claude` blindly when the operator has explicitly
		// pointed Claude at a different config dir (e.g. a managed install)
		// would copy the wrong credentials into the scratch dir and break
		// auth — see the second Must Fix in MUL-2603 review.
		hostConfigDir := resolveHostClaudeConfigDir(b.cfg.Env)
		dir, cleanup, err := newIsolatedClaudeConfigDir(opts.Cwd, hostConfigDir, b.cfg.Logger)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("isolate claude config dir: %w", err)
		}
		claudeConfigDir = dir
		claudeConfigCleanup = cleanup
	}
	defer func() {
		if claudeConfigCleanup != nil {
			claudeConfigCleanup()
		}
	}()

	cmd.Env = buildClaudeEnv(b.cfg.Env, claudeConfigDir, b.cfg.Logger)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
			stdin = nil
		}
	}
	// Capture stderr into both the daemon log (as before) and a bounded tail
	// buffer so we can include the last few KB in Result.Error when claude
	// exits unexpectedly. Without the tail, an exit-code-only failure looks
	// like "claude exited with error: exit status 3" — which is useless for
	// root-causing V8 aborts, Bun panics, or any other CLI-side crash.
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}
	if err := writeClaudeInput(stdin, prompt); err != nil {
		// claude almost certainly died during startup (broken pipe). The
		// real reason is sitting in stderrBuf — surface it the same way the
		// post-handshake error path does, otherwise the daemon log is the
		// only place that knows whether it was a V8 abort, a missing native
		// module, or anything else. cmd.Wait() flushes os/exec's stderr
		// copy goroutine, so stderrBuf.Tail() is safe to read.
		closeStdin()
		cancel()
		_ = cmd.Wait()
		return nil, errors.New(withAgentStderr(fmt.Sprintf("write claude input: %v", err), "claude", stderrBuf.Tail()))
	}
	closeStdin()

	b.cfg.Logger.Info("claude started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model, "skills_local", opts.SkillsLocal, "claude_config_dir", claudeConfigDir)

	// cmd.Start() succeeded — transfer temp file ownership to the goroutine.
	mcpFileCleanup = nil
	// Transfer isolated-config cleanup the same way: the goroutine outlives
	// this function, so it owns removal of the scratch dir.
	isolatedConfigCleanup := claudeConfigCleanup
	claudeConfigCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer os.Remove(mcpConfigPath)
		}
		if isolatedConfigCleanup != nil {
			defer isolatedConfigCleanup()
		}

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		// Close stdout when the context is cancelled so scanner.Scan() unblocks.
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

			var msg claudeSDKMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "assistant":
				b.handleAssistant(msg, msgCh, &output, usage)
			case "user":
				b.handleUser(msg, msgCh)
			case "system":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				closeStdin()
				sessionID = msg.SessionID
				if msg.ResultText != "" {
					output.Reset()
					output.WriteString(msg.ResultText)
				}
				if resultUsage := claudeResultUsage(msg, opts.Model); len(resultUsage) > 0 {
					usage = resultUsage
				}
				if msg.IsError {
					finalStatus = "failed"
					finalError = msg.ResultText
				}
			case "log":
				if msg.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   msg.Log.Level,
						Content: msg.Log.Message,
					})
				}
			}
		}

		// Wait for process exit
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("claude timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("claude exited with error: %v", exitErr)
		}

		// cmd.Wait() has returned — os/exec's stderr copy goroutine has
		// observed every byte claude wrote to stderr before exiting, so
		// stderrBuf.Tail() is safe to sample now. Attach the tail to any
		// non-empty failure message; callers upstream surface this as the
		// task's error field, which is the only place users see it.
		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("claude finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed")
		if reportedSessionID != sessionID {
			b.cfg.Logger.Info("claude resume did not land; clearing fresh session id for daemon fallback",
				"requested_resume", opts.ResumeSessionID,
				"emitted_session", sessionID,
			)
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  reportedSessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *claudeBackend) handleAssistant(msg claudeSDKMessage, ch chan<- Message, output *strings.Builder, usage map[string]TokenUsage) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

	// Accumulate token usage per model.
	if content.Usage != nil && content.Model != "" {
		u := usage[content.Model]
		u.InputTokens += content.Usage.InputTokens
		u.OutputTokens += content.Usage.OutputTokens
		u.CacheReadTokens += content.Usage.CacheReadInputTokens
		u.CacheWriteTokens += content.Usage.CacheCreationInputTokens
		usage[content.Model] = u
	}

	for _, block := range content.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   block.Name,
				CallID: block.ID,
				Input:  input,
			})
		}
	}
}

func (b *claudeBackend) handleUser(msg claudeSDKMessage, ch chan<- Message) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

	for _, block := range content.Content {
		if block.Type == "tool_result" {
			resultStr := ""
			if block.Content != nil {
				resultStr = string(block.Content)
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: block.ToolUseID,
				Output: resultStr,
			})
		}
	}
}

func (b *claudeBackend) handleControlRequest(msg claudeSDKMessage, stdin interface{ Write([]byte) (int, error) }) {
	// Auto-approve all tool uses in autonomous/daemon mode.
	var req claudeControlRequestPayload
	if err := json.Unmarshal(msg.Request, &req); err != nil {
		return
	}

	var inputMap map[string]any
	if req.Input != nil {
		_ = json.Unmarshal(req.Input, &inputMap)
	}
	if inputMap == nil {
		inputMap = map[string]any{}
	}

	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": msg.RequestID,
			"response": map[string]any{
				"behavior":     "allow",
				"updatedInput": inputMap,
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		b.cfg.Logger.Warn("claude: failed to marshal control response", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := stdin.Write(data); err != nil {
		b.cfg.Logger.Warn("claude: failed to write control response", "error", err)
	}
}

// ── Claude SDK JSON types ──

type claudeSDKMessage struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`

	// result fields
	ResultText string                            `json:"result,omitempty"`
	IsError    bool                              `json:"is_error,omitempty"`
	DurationMs float64                           `json:"duration_ms,omitempty"`
	NumTurns   int                               `json:"num_turns,omitempty"`
	Usage      *claudeUsage                      `json:"usage,omitempty"`
	ModelUsage map[string]claudeResultModelUsage `json:"modelUsage,omitempty"`

	// log fields
	Log *claudeLogEntry `json:"log,omitempty"`

	// control request fields
	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}

type claudeLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type claudeMessageContent struct {
	Role    string               `json:"role"`
	Model   string               `json:"model"`
	Content []claudeContentBlock `json:"content"`
	Usage   *claudeUsage         `json:"usage,omitempty"`
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type claudeResultModelUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

func claudeResultUsage(msg claudeSDKMessage, fallbackModel string) map[string]TokenUsage {
	if len(msg.ModelUsage) > 0 {
		usage := make(map[string]TokenUsage, len(msg.ModelUsage))
		for model, u := range msg.ModelUsage {
			if model == "" || !claudeUsageHasTokens(u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens) {
				continue
			}
			usage[model] = TokenUsage{
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				CacheReadTokens:  u.CacheReadInputTokens,
				CacheWriteTokens: u.CacheCreationInputTokens,
			}
		}
		if len(usage) > 0 {
			return usage
		}
	}

	model := msg.Model
	if model == "" {
		model = fallbackModel
	}
	if msg.Usage == nil || model == "" || !claudeUsageHasTokens(
		msg.Usage.InputTokens,
		msg.Usage.OutputTokens,
		msg.Usage.CacheReadInputTokens,
		msg.Usage.CacheCreationInputTokens,
	) {
		return nil
	}
	return map[string]TokenUsage{
		model: {
			InputTokens:      msg.Usage.InputTokens,
			OutputTokens:     msg.Usage.OutputTokens,
			CacheReadTokens:  msg.Usage.CacheReadInputTokens,
			CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		},
	}
}

func claudeUsageHasTokens(input, output, cacheRead, cacheWrite int64) bool {
	return input > 0 || output > 0 || cacheRead > 0 || cacheWrite > 0
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type claudeControlRequestPayload struct {
	Subtype  string          `json:"subtype"`
	ToolName string          `json:"tool_name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

// ── Shared helpers ──

func trySend(ch chan<- Message, msg Message) {
	select {
	case ch <- msg:
	default:
		// Channel full — drop message. Final output is accumulated separately
		// in Result.Output, so only streaming consumers are affected.
	}
}

// claudeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// the daemon↔Claude communication protocol.
var claudeBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedStandalone, // non-interactive mode
	"--output-format":   blockedWithValue,  // stream-json protocol
	"--input-format":    blockedWithValue,  // stream-json protocol
	"--permission-mode": blockedWithValue,  // bypassPermissions for autonomous operation
	"--mcp-config":      blockedWithValue,  // set by daemon from agent.mcp_config
	// `--effort` is owned by the per-agent thinking_level picker so a
	// user-supplied custom_arg cannot silently outvote it. The daemon
	// injects --effort only when opts.ThinkingLevel is set; if a user
	// nevertheless writes it in custom_args we drop the duplicate and
	// log a warning rather than letting the CLI receive two conflicting
	// --effort values.
	"--effort": blockedWithValue,
}

func buildClaudeArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
		"--permission-mode", "bypassPermissions",
		// AskUserQuestion is Claude Code's built-in interactive question tool.
		// The daemon runs Claude in non-interactive stream-json mode and has
		// no UI for the prompt to render in, so a call returns an empty
		// answer and the agent ends up "inferring" silently — the user
		// never sees the question (see GitHub #2588). User-facing
		// clarification belongs in an issue comment instead.
		"--disallowedTools", "AskUserQuestion",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		// Slotted right after --model so the per-session effort runs
		// against the same model selection the args advertise; the CLI
		// itself accepts the flag in any order but this ordering makes
		// the launch line readable in `agent command` logs.
		args = append(args, "--effort", opts.ThinkingLevel)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, claudeBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, claudeBlockedArgs, logger)...)
	return args
}

func writeClaudeInput(w io.Writer, prompt string) error {
	data, err := buildClaudeInput(prompt)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

func buildClaudeInput(prompt string) ([]byte, error) {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]string{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal claude input: %w", err)
	}
	return append(data, '\n'), nil
}

// resolveSessionID decides which session id to report on the Result. When the
// caller requested --resume but claude emitted a fresh, different session id
// AND the run failed, the resume did not land (claude prints
// "No conversation found with session ID: ..." to stderr, generates a fresh
// session, and exits). Returning "" in that case keeps the daemon's
// retry-with-fresh-session fallback able to trigger, instead of silently
// persisting a brand-new id as if resume had succeeded.
func resolveSessionID(requestedResume, emitted string, failed bool) string {
	if failed && requestedResume != "" && emitted != "" && emitted != requestedResume {
		return ""
	}
	return emitted
}

func buildEnv(extra map[string]string) []string {
	return mergeEnv(os.Environ(), extra)
}

// buildClaudeEnv builds the env slice for the claude subprocess. When
// claudeConfigDir is non-empty (isolated-skills mode), it overrides
// CLAUDE_CONFIG_DIR for the child so the CLI looks at the per-task
// scratch dir instead of `~/.claude/`. We also strip the parent's
// CLAUDE_CONFIG_DIR so a daemon-host env var cannot accidentally win
// the override race. Callers in "merge" mode pass "" and behaviour
// matches the pre-MUL-2603 buildEnv path.
//
// On macOS isolation also surfaces the host's Claude Code OAuth token
// through CLAUDE_CODE_OAUTH_TOKEN — see buildClaudeEnvWith for why this
// is necessary on Darwin even when .claude.json is already mirrored.
func buildClaudeEnv(extra map[string]string, claudeConfigDir string, logger *slog.Logger) []string {
	return buildClaudeEnvWith(extra, claudeConfigDir, logger, readHostClaudeOAuthToken)
}

// buildClaudeEnvWith is the testable seam behind buildClaudeEnv. Tests
// inject a readOAuthToken closure so they can exercise the auth
// passthrough without touching the real macOS keychain.
func buildClaudeEnvWith(
	extra map[string]string,
	claudeConfigDir string,
	logger *slog.Logger,
	readOAuthToken func() (string, error),
) []string {
	env := mergeEnv(os.Environ(), extra)
	if claudeConfigDir == "" {
		return env
	}
	// Drop any CLAUDE_CONFIG_DIR already in the slice (from os.Environ or
	// from custom_env) before appending the isolated override so the last
	// entry wins deterministically. Track whether the operator already
	// pinned OAuth / API-key auth via env so we know whether to add a
	// keychain-sourced token below. "Pinned" means the value is non-empty:
	// an empty `ANTHROPIC_API_KEY=` / `CLAUDE_CODE_OAUTH_TOKEN=` leaks in
	// from `os.Environ()` on hosts whose login shell unsets it that way,
	// and treating that as an explicit auth choice would silently strand
	// the isolated child at "Not logged in" (MUL-2603 review follow-up).
	filtered := env[:0]
	hasOAuthToken := false
	hasAnthropicKey := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			continue
		}
		if v, ok := strings.CutPrefix(entry, "CLAUDE_CODE_OAUTH_TOKEN="); ok {
			if v == "" {
				// Empty `CLAUDE_CODE_OAUTH_TOKEN=` is noise (login-shell
				// quirk, stale custom_env entry) — drop so it cannot shadow
				// a keychain-injected token later in the slice on platforms
				// where the libc env lookup picks the first match.
				continue
			}
			hasOAuthToken = true
		}
		if v, ok := strings.CutPrefix(entry, "ANTHROPIC_API_KEY="); ok {
			if v == "" {
				// Same treatment as the OAuth token: empty `ANTHROPIC_API_KEY=`
				// must not pose as a pinned API-key auth choice. Drop so the
				// child does not see a confusing empty value either.
				continue
			}
			hasAnthropicKey = true
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, "CLAUDE_CONFIG_DIR="+claudeConfigDir)

	// Auth passthrough — only meaningful in isolation mode, and only on
	// platforms where the child CLI cannot follow the host's auth on its
	// own. On Linux/Windows the OAuth token lives in
	// `$CLAUDE_CONFIG_DIR/.credentials.json` and is already symlinked via
	// mirrorHostClaudeExceptSkills, so readHostClaudeOAuthToken is a
	// no-op there. On macOS Claude Code 2.x scopes the keychain entry by
	// SHA-256(CLAUDE_CONFIG_DIR)[:8]; isolating the config dir changes
	// that suffix, so the child cannot find the host token even though it
	// is sitting in the default `Claude Code-credentials` entry. Surface
	// the access token via CLAUDE_CODE_OAUTH_TOKEN so the child skips
	// keychain lookup entirely (MUL-2603 follow-up regression).
	//
	// Security boundary: CLAUDE_CODE_OAUTH_TOKEN is the host's claude.ai
	// OAuth access token. We rely on Claude Code itself scrubbing it from
	// the env it passes to Bash / hook subprocesses — a property we lock
	// in with TestClaudeCLIScrubsOAuthTokenFromBashSubprocess, which boots
	// the real CLI with a canary token and asserts `printenv` inside the
	// model-driven Bash tool returns empty. If that test ever flips
	// (upstream CLI change), this passthrough must move off env vars
	// before merging.
	//
	// Precedence (highest wins): operator-pinned CLAUDE_CODE_OAUTH_TOKEN
	// in custom_env, then ANTHROPIC_API_KEY (the user explicitly chose
	// API-key auth — do not silently override with OAuth), then the
	// keychain token. "Pinned" is gated on a non-empty value above so an
	// empty `KEY=` inherited from os.Environ does not pose as an explicit
	// choice and disable the keychain reader.
	if !hasOAuthToken && !hasAnthropicKey && readOAuthToken != nil {
		token, err := readOAuthToken()
		if err != nil && logger != nil {
			logger.Warn("claude: read host oauth token failed",
				"error", err,
			)
		}
		if token != "" {
			filtered = append(filtered, "CLAUDE_CODE_OAUTH_TOKEN="+token)
		}
	}
	return filtered
}

func mergeEnv(base []string, extra map[string]string) []string {
	env := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if isFilteredChildEnvKey(key) {
			continue
		}
		env = append(env, entry)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func isFilteredChildEnvKey(key string) bool {
	return key == "CLAUDECODE" ||
		strings.HasPrefix(key, "CLAUDECODE_") ||
		strings.HasPrefix(key, "CLAUDE_CODE_")
}

// resolveHostClaudeConfigDir picks the directory we mirror into the per-task
// scratch CLAUDE_CONFIG_DIR. Precedence:
//
//  1. Agent custom_env CLAUDE_CONFIG_DIR (operator pinned this agent at a
//     specific install — e.g. a managed shared profile).
//  2. Parent process CLAUDE_CONFIG_DIR (daemon-host env var; some self-host
//     deployments set this globally so `claude login` writes there instead
//     of `~/.claude`).
//  3. `~/.claude/` (the documented default for Linux/Windows; macOS keeps
//     credentials in Keychain so the dir still holds settings/agents/etc.).
//
// Returns "" when none of the above are usable (no env, no home dir). The
// caller treats that as "host has no Claude config to mirror" — the scratch
// dir stays empty and the CLI relies on `ANTHROPIC_API_KEY` auth.
func resolveHostClaudeConfigDir(extraEnv map[string]string) string {
	if v, ok := extraEnv["CLAUDE_CONFIG_DIR"]; ok && v != "" {
		return v
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// newIsolatedClaudeConfigDir creates a per-run scratch directory used as
// CLAUDE_CONFIG_DIR when the agent opted out of host-machine skill merging.
// The dir is placed under the task workdir when available so it lives and
// dies with the task's storage allocation; otherwise it falls back to the
// OS temp dir.
//
// The dir is NOT empty: every entry from the effective host Claude config
// (resolveHostClaudeConfigDir) is mirrored through *except* `skills/`. That
// preserves Claude Code's login state (`.credentials.json` on Linux/Windows),
// global settings, plugins, agents, commands, output styles, todos, etc. —
// which all live in this directory and would otherwise be invisible to the
// isolated child — while still keeping the user-global skills directory off
// the CLI's discovery path. Without this passthrough, opting into "ignore"
// mode on a non-API-key host would force the Claude agent to re-authenticate,
// breaking the documented "run `claude` once to log in" install flow.
//
// Each entry uses createDirLink / createFileLink, which try symlink first
// and fall back to a directory junction (Windows `mklink /J`) for dirs or a
// hardlink/copy for files when the platform rejects the symlink (Windows
// without Developer Mode / admin). Failing silently to symlink-only would
// leave Windows hosts with an empty scratch dir, defeating the auth
// passthrough — see MUL-2603 review.
//
// The returned cleanup removes the scratch directory; symlinks, junctions,
// and hardlinks are unlinked without touching the real host file. Safe to
// call from any goroutine exactly once.
func newIsolatedClaudeConfigDir(taskCwd, hostConfigDir string, logger *slog.Logger) (string, func(), error) {
	parent := taskCwd
	if parent == "" {
		parent = os.TempDir()
	}
	dir, err := os.MkdirTemp(parent, "multica-claude-config-*")
	if err != nil {
		// Fall back to the OS temp dir if the cwd refused us (read-only
		// volume, missing parent, etc.). A scratch dir somewhere on the
		// host is still strictly better than letting the CLI auto-discover
		// `~/.claude/skills/`.
		if parent != os.TempDir() {
			dir, err = os.MkdirTemp(os.TempDir(), "multica-claude-config-*")
		}
		if err != nil {
			return "", nil, err
		}
	}

	if hostConfigDir != "" {
		// Best effort. If the host config dir is missing entirely (env-var-
		// auth-only setup) we just leave the scratch dir empty. If individual
		// entries fail to mirror — even after fallback — we log so operators
		// can correlate Claude Code auth failures with the missing mirror,
		// but we do not abort: a partial mirror is still better than letting
		// the CLI discover `~/.claude/skills/` directly.
		if err := mirrorHostClaudeExceptSkills(hostConfigDir, dir); err != nil && logger != nil {
			logger.Warn("claude: mirror host config dir failed",
				"source", hostConfigDir,
				"dest", dir,
				"error", err,
			)
		}
		// Claude Code's default layout (no CLAUDE_CONFIG_DIR set) stores the
		// main config — login state, projects history, recent sessions — at
		// `$HOME/.claude.json`, a *sibling* of `~/.claude/` rather than
		// inside it. The mirror above only walks entries under
		// `hostConfigDir`, so on default hosts the scratch dir never gets
		// `.claude.json` and the CLI exits with `Claude configuration file
		// not found … Not logged in · Please run /login` (MUL-2661).
		if err := mirrorHostClaudeJSONIfMissing(hostConfigDir, dir); err != nil && logger != nil {
			logger.Warn("claude: mirror host .claude.json failed",
				"source", hostConfigDir,
				"dest", dir,
				"error", err,
			)
		}
	}

	cleanup := func() {
		// Best effort — caller logs through normal channels if cleanup fails
		// (a leftover scratch dir is non-fatal; the GC sweeper that reclaims
		// orphaned task workdirs will catch it too). RemoveAll unlinks
		// symlinks / junctions without following them, so the real host
		// `~/.claude/` is not touched. Hardlinks share inodes so unlinking
		// the mirror entry leaves the source file with refcount 1.
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

// mirrorHostClaudeExceptSkills mirrors every direct entry under
// hostClaudeDir into destDir, skipping `skills/`. Each entry tries a
// symlink first and falls back to a directory junction (Windows
// `mklink /J`) or a hardlink/copy (Windows without Developer Mode) so the
// mirror still works on hosts that lack SeCreateSymbolicLinkPrivilege. The
// first per-entry error is returned so callers can log it; we do not abort
// the loop, because the goal — keeping `skills/` out of the CLI's discovery
// path — is met as soon as we own the scratch dir, and a partial mirror is
// still better than no isolation.
func mirrorHostClaudeExceptSkills(hostClaudeDir, destDir string) error {
	return mirrorHostClaudeExceptSkillsWith(hostClaudeDir, destDir, createDirLink, createFileLink)
}

// mirrorHostClaudeExceptSkillsWith is the testable seam behind
// mirrorHostClaudeExceptSkills. Tests inject custom dirLink / fileLink
// closures to exercise the symlink-failure fallback path on platforms
// (Linux/macOS) where os.Symlink would otherwise always succeed.
func mirrorHostClaudeExceptSkillsWith(
	hostClaudeDir, destDir string,
	dirLink, fileLink func(src, dst string) error,
) error {
	entries, err := os.ReadDir(hostClaudeDir)
	if err != nil {
		return err
	}
	var firstErr error
	for _, entry := range entries {
		if entry.Name() == "skills" {
			continue
		}
		src := filepath.Join(hostClaudeDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())
		var linkErr error
		if entry.IsDir() {
			linkErr = dirLink(src, dst)
		} else {
			linkErr = fileLink(src, dst)
		}
		if linkErr != nil && firstErr == nil {
			firstErr = fmt.Errorf("link %s: %w", entry.Name(), linkErr)
		}
	}
	return firstErr
}

// mirrorHostClaudeJSONIfMissing links `$HOME/.claude.json` into destDir as
// `.claude.json` when it is not already there. Claude Code's default layout
// (no CLAUDE_CONFIG_DIR set) stores the main config — login state, project
// history — at `$HOME/.claude.json`, a *sibling* of `~/.claude/`, not inside
// it. Without this passthrough, isolating CLAUDE_CONFIG_DIR strands the CLI
// in a dir without `.claude.json` and it bails with
// `Claude configuration file not found … Not logged in · Please run /login`
// (MUL-2661 regression).
//
// No-op when:
//   - destDir already has `.claude.json` (mirrored from inside a custom
//     CLAUDE_CONFIG_DIR by mirrorHostClaudeExceptSkills);
//   - hostConfigDir is not the default `$HOME/.claude` — a custom
//     CLAUDE_CONFIG_DIR is expected to be self-contained, and silently
//     merging `$HOME/.claude.json` from a different account would mask
//     credential drift;
//   - `$HOME/.claude.json` does not exist (env-var-auth-only or fresh
//     install).
func mirrorHostClaudeJSONIfMissing(hostConfigDir, destDir string) error {
	return mirrorHostClaudeJSONIfMissingWith(hostConfigDir, destDir, os.UserHomeDir, createFileLink)
}

// mirrorHostClaudeJSONIfMissingWith is the testable seam behind
// mirrorHostClaudeJSONIfMissing. Tests inject homeDir / fileLink so they can
// exercise the precedence rules without mutating the process environment.
func mirrorHostClaudeJSONIfMissingWith(
	hostConfigDir, destDir string,
	homeDir func() (string, error),
	fileLink func(src, dst string) error,
) error {
	dst := filepath.Join(destDir, ".claude.json")
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return nil
	}
	if hostConfigDir != filepath.Join(home, ".claude") {
		return nil
	}
	src := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return fileLink(src, dst)
}

// copyFile copies the bytes of src into dst with a fresh file. Used as the
// last-resort fallback inside createFileLink on Windows when both symlink
// and hardlink are unavailable. Kept in the platform-agnostic file so the
// Unix build still compiles (it never calls copyFile, but the symbol must
// exist for the test fallback seam to be exercisable on Linux/macOS).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return nil
}


// blockedArgMode specifies whether a blocked arg takes a value or is standalone.
type blockedArgMode int

const (
	blockedWithValue  blockedArgMode = iota // flag takes a value (next arg or =value)
	blockedStandalone                       // flag is boolean, no value
)

// filterCustomArgs removes protocol-critical flags from user-configured custom
// args to prevent breaking daemon↔agent communication. Each backend defines its
// own blocked set (the flags it hardcodes). This is intentionally narrow — we
// only block args that would break the communication protocol, not every
// possible dangerous flag. Workspace members are trusted to configure agents
// sensibly, same as with custom_env.
func filterCustomArgs(args []string, blocked map[string]blockedArgMode, logger *slog.Logger) []string {
	if len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	skip := false
	for _, arg := range args {
		if skip {
			skip = false
			continue
		}
		// Check if this arg is a blocked flag or starts with "blockedFlag=".
		flag := arg
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			hasInlineValue = true
		}
		mode, isBlocked := blocked[flag]
		if isBlocked {
			logger.Warn("custom_args: blocked protocol-critical flag, skipping", "flag", flag)
			if mode == blockedWithValue && !hasInlineValue {
				// The next arg is the value for this flag — skip it too.
				skip = true
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// writeMcpConfigToTemp writes raw MCP config JSON to a temporary file and returns
// its path. The caller is responsible for removing the file when done.
func writeMcpConfigToTemp(raw json.RawMessage) (string, error) {
	f, err := os.CreateTemp("", "multica-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("create mcp config temp file: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write mcp config temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close mcp config temp file: %w", err)
	}
	return f.Name(), nil
}

func detectCLIVersion(ctx context.Context, execPath string) (string, error) {
	cmd := exec.CommandContext(ctx, execPath, "--version")
	hideAgentWindow(cmd)
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect version for %s: %w", execPath, err)
	}
	return extractVersionLine(string(data)), nil
}

// extractVersionLine pulls the version line out of a `<cli> --version` capture,
// discarding leading shell noise. On Windows, npm-installed CLI shims (notably
// gemini's) emit `chcp` output like `Active code page: 65001` before the real
// version reaches stdout, and the raw concatenation was being persisted as the
// runtime version (see #2516).
//
// The heuristic: return the first non-empty line that contains a semver-shaped
// token (matches versionRe). Full version strings like "2.1.5 (Claude Code)"
// or "codex-cli 0.118.0" survive unchanged because the whole matching line is
// returned. If no line carries a semver token, fall back to the trimmed raw
// output so unusual version formats aren't silently dropped to empty.
func extractVersionLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if versionRe.MatchString(line) {
			return line
		}
	}
	return strings.TrimSpace(raw)
}

// logWriter adapts a *slog.Logger to an io.Writer for capturing stderr.
type logWriter struct {
	logger *slog.Logger
	prefix string
}

func newLogWriter(logger *slog.Logger, prefix string) *logWriter {
	return &logWriter{logger: logger, prefix: prefix}
}

func (w *logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text != "" {
		w.logger.Debug(w.prefix + text)
	}
	return len(p), nil
}
