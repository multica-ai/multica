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
	"strings"
	"time"
)

// cfuseBackend implements Backend by spawning the cfuse CLI with stream-json
// output. cfuse is a Claude Code wrapper that supports the same stream-json
// protocol, so this backend reuses claudeBackend's message parsing and session
// handling. The differences are in argument construction:
//   - --print instead of -p (both are equivalent but cfuse's CLI only has --print)
//   - --approval-mode yolo instead of --permission-mode bypassPermissions
//   - --skip-update to prevent interactive prompts on startup
//   - No --input-format or --verbose (cfuse sets these internally for the engine)
//
// IMPORTANT: cfuse has its own CLI argument parser that runs before delegating
// to the Claude Code engine. Flags like --input-format, --verbose, and
// --permission-mode are engine-only and will cause "Unknown arguments" errors
// when cfuse's login session is expired. Always use cfuse-native flags
// (--approval-mode yolo, --skip-update, etc.) for critical arguments.
type cfuseBackend struct {
	cfg Config
}

func (b *cfuseBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "cfuse"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("cfuse executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildCfuseArgs(opts, b.cfg.Logger)

	var mcpConfigPath string
	var mcpFileCleanup func()
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
	// cfuse inherits Claude Code's env filtering needs — strip any
	// CLAUDECODE/CLAUDE_CODE_ variables that would conflict with cfuse's
	// own Claude Code engine.
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cfuse stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cfuse stdin pipe: %w", err)
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
			stdin = nil
		}
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[cfuse:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start cfuse: %w", err)
	}
	if err := writeClaudeInput(stdin, prompt); err != nil {
		closeStdin()
		cancel()
		_ = cmd.Wait()
		return nil, errors.New(withAgentStderr(fmt.Sprintf("write cfuse input: %v", err), "cfuse", stderrBuf.Tail()))
	}
	closeStdin()

	b.cfg.Logger.Info("cfuse started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	mcpFileCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer os.Remove(mcpConfigPath)
		}

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

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
				handleCfuseAssistant(msg, msgCh, &output, usage)
			case "user":
				handleCfuseUser(msg, msgCh)
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

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("cfuse timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("cfuse exited with error: %v", exitErr)
		}

		if finalError != "" {
			finalError = withAgentStderr(finalError, "cfuse", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("cfuse finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed")
		if reportedSessionID != sessionID {
			b.cfg.Logger.Info("cfuse resume did not land; clearing fresh session id for daemon fallback",
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

// handleCfuseAssistant reuses the same parsing logic as claudeBackend.handleAssistant
// because cfuse emits the same stream-json protocol via its Claude Code engine.
func handleCfuseAssistant(msg claudeSDKMessage, ch chan<- Message, output *strings.Builder, usage map[string]TokenUsage) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

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

// handleCfuseUser reuses the same parsing logic as claudeBackend.handleUser.
func handleCfuseUser(msg claudeSDKMessage, ch chan<- Message) {
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

// cfuseBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args for cfuse.
var cfuseBlockedArgs = map[string]blockedArgMode{
	"--print":          blockedStandalone, // non-interactive mode
	"--output-format":  blockedWithValue,  // stream-json protocol
	"--approval-mode":  blockedWithValue,  // yolo mode for autonomous operation
	"-y":               blockedStandalone, // alias for --approval-mode yolo
	"--yolo":           blockedStandalone, // alias for --approval-mode yolo
	"--mcp-config":     blockedWithValue,  // set by daemon from agent.mcp_config
	"--skip-update":    blockedStandalone, // must be set to prevent interactive prompts
	"-p":               blockedStandalone, // alias for --print
}

func buildCfuseArgs(opts ExecOptions, logger *slog.Logger) []string {
	// cfuse has its own CLI layer that validates arguments before delegating
	// to the Claude Code engine. Only cfuse-native flags are safe here;
	// engine-only flags like --input-format, --verbose, --effort,
	// --permission-mode, --max-turns, and --append-system-prompt are
	// rejected by cfuse's parser when the login session is expired, causing
	// "Unknown arguments" errors and immediate task failure.
	//
	// cfuse internally forwards these engine-only flags to the Claude Code
	// engine when logged in:
	//   --cc --print --verbose --output-format stream-json
	//   --input-format stream-json --skip-update
	//   --setting-sources user,project,local
	args := []string{
		"--print",                        // non-interactive mode (cfuse uses --print, Claude uses -p)
		"--output-format", "stream-json", // stream-json output for daemon parsing
		"--approval-mode", "yolo",        // cfuse-native: auto-approve all operations (equivalent to --permission-mode bypassPermissions)
		"--skip-update",                  // cfuse-specific: prevent interactive engine update prompt
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	// Engine-specific flags: --effort, --max-turns, --append-system-prompt,
	// --resume, and --input-format are NOT cfuse-native. They work when
	// cfuse is logged in (cfuse delegates to the engine which recognizes them)
	// but cause "Unknown arguments" errors when cfuse's login session is
	// expired. We still pass them because they're needed for full
	// functionality, and the login-expiry case is an operational issue
	// that should be fixed at the cfuse/auth level rather than by
	// stripping features.
	if opts.ThinkingLevel != "" {
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
	args = append(args, filterCustomArgs(opts.ExtraArgs, cfuseBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, cfuseBlockedArgs, logger)...)
	return args
}

// detectCfuseVersion attempts to detect the cfuse CLI version. cfuse --version
// may trigger an interactive engine update prompt, so we first try running it
// with a timeout. If that fails, we fall back to extracting the version from
// the binary installation path (e.g. /versions/v2.6.21/cfuse -> 2.6.21).
func detectCfuseVersion(ctx context.Context, execPath string) (string, error) {
	// Try --version with --skip-update to bypass interactive prompts. Also pipe
	// "3" to stdin as a fallback: if --skip-update is not honored by this cfuse
	// version, "3" selects "don't notify again for this version", permanently
	// silencing the prompt and unblocking future runs.
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, execPath, "--version", "--skip-update")
	hideAgentWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return extractCfuseVersionFromPath(execPath), nil
	}
	go func() {
		// Send "3" to permanently skip the engine update prompt, then close stdin.
		io.WriteString(stdin, "3\n")
		stdin.Close()
	}()
	data, err := cmd.Output()
	if err != nil {
		return extractCfuseVersionFromPath(execPath), nil
	}
	version := extractVersionLine(string(data))
	if version != "" {
		return version, nil
	}
	return extractCfuseVersionFromPath(execPath), nil
}

// extractCfuseVersionFromPath extracts a version from the cfuse installation
// path. cfuse installs to paths like
// /Users/x/.local/share/codefuse-cli/versions/v2.6.21/cfuse — we extract
// the v2.6.21 component.
func extractCfuseVersionFromPath(execPath string) string {
	// Walk up the path looking for a "versions/<version>" directory pattern.
	parts := strings.Split(execPath, "/")
	for i := 1; i < len(parts); i++ {
		if parts[i-1] == "versions" && versionRe.MatchString(parts[i]) {
			m := versionRe.FindStringSubmatch(parts[i])
			if len(m) >= 4 {
				return m[1] + "." + m[2] + "." + m[3]
			}
		}
	}
	return ""
}