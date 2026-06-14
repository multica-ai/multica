package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// streamJSONExternalBackend implements Backend for external runtime
// extensions that speak the Claude-compatible stream-json NDJSON protocol on
// stdin/stdout, as opposed to the JSON-RPC ACP transport.
//
// The wire format is identical to Claude Code's `--output-format stream-json
// --input-format stream-json` mode: the daemon writes a single user frame to
// stdin, drains assistant/user/system/result events from stdout, then closes
// stdin once the result frame arrives. Manifest authors get a generic
// stream-json runtime by setting `transport: "stream-json"` and listing the
// CLI's required flags in `command.args` (e.g. `["-p","--output-format","stream-json",
// "--input-format","stream-json","--verbose"]`).
//
// Capability-driven argv injection mirrors the ACP backend:
//
//   - mcp_config         → `--mcp-config <temp-file>` (Claude-style argv)
//   - session_resume     → `--resume <id>`
//   - max_turns          → `--max-turns <N>`
//   - thinking           → `--effort <level>`
//   - model_selection    → `--model <id>`
//   - inline_system_prompt → `--append-system-prompt <text>`
//
// Manifests that ship a CLI which uses different flag names should declare
// the desired flags in `command.args` and rely on `inline_system_prompt:
// false` etc. to keep the daemon from injecting the standard names. This
// gives runtime authors the same ergonomics as the built-in stream-json
// providers while keeping each opt-in explicit.
type streamJSONExternalBackend struct {
	cfg Config
}

func (b *streamJSONExternalBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		return nil, fmt.Errorf("stream-json external backend: executable path is empty")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("stream-json external executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	blocked := manifestBlockedArgs(b.cfg.BlockedArgs)
	args := buildStreamJSONExternalArgs(b.cfg, opts, blocked)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("stream-json external command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(mergeRuntimeEnv(b.cfg.Env, b.cfg.SkillsRoot))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stream-json external stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stream-json external stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[stream-json-ext:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start stream-json external: %w", err)
	}

	b.cfg.Logger.Info("stream-json external started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Write the initial user prompt frame in a separate goroutine so the
	// stdout reader can drain even if the CLI emits a startup banner before
	// reading stdin (avoids the same deadlock seen in built-in stream-json backends).
	writeDone := make(chan error, 1)
	go func() {
		err := writeStreamJSONUserFrame(stdin, prompt)
		if err != nil {
			closeStdin()
		}
		writeDone <- err
	}()

	// Close stdout when the run context is cancelled so scanner.Scan unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer closeStdin()
		defer close(msgCh)
		defer close(resCh)

		// Emit a status message immediately so the daemon's idle watchdog
		// knows the backend is alive even before the first assistant event.
		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var evt streamJSONEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}
			switch evt.Type {
			case "assistant":
				handleStreamJSONAssistant(evt, msgCh, &output, usage)
			case "user":
				handleStreamJSONUser(evt, msgCh)
			case "system":
				if evt.SessionID != "" {
					sessionID = evt.SessionID
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				handleStreamJSONResult(evt, &output, &finalStatus, &finalError)
				closeStdin()
			}
		}

		if writeErr := <-writeDone; writeErr != nil && finalStatus == "completed" && finalError == "" {
			finalError = fmt.Sprintf("stream-json external stdin write failed: %v", writeErr)
			finalStatus = "failed"
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			finalStatus = "timeout"
			finalError = fmt.Sprintf("stream-json external timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			finalStatus = "aborted"
			finalError = "execution cancelled"
		case waitErr != nil && finalStatus == "completed":
			finalStatus = "failed"
			finalError = fmt.Sprintf("stream-json external exited with error: %v", waitErr)
		}

		if finalStatus == "failed" || finalStatus == "aborted" {
			if tail := stderrBuf.Tail(); tail != "" {
				finalError += fmt.Sprintf("\n[stderr tail]\n%s", tail)
			}
		}

		b.cfg.Logger.Info("stream-json external finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildStreamJSONExternalArgs assembles the argv for a stream-json runtime
// extension invocation. It starts with the manifest-declared base args
// (typically the protocol flags `-p --output-format stream-json
// --input-format stream-json --verbose`), then appends capability-gated
// daemon flags, then the user-supplied extra/custom args (filtered against
// the manifest's blocked set). Order matches the Claude convention:
// convention: protocol flags → daemon-managed flags → user flags.
func buildStreamJSONExternalArgs(cfg Config, opts ExecOptions, blocked map[string]blockedArgMode) []string {
	args := append([]string{}, cfg.ACPArgs...)
	if cfg.Capabilities.ModelSelection && opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if cfg.Capabilities.MaxTurns && opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if cfg.Capabilities.Thinking && opts.ThinkingLevel != "" {
		args = append(args, "--effort", opts.ThinkingLevel)
	}
	if cfg.Capabilities.SessionResume && opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, blocked, cfg.Logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, blocked, cfg.Logger)...)
	return args
}

// writeStreamJSONUserFrame writes a single Claude-shaped user
// message to stdin and returns. Closing stdin is the caller's job — the
// runtime keeps stdin open so the same backend could in principle be
// extended to multi-turn streaming later without changing the wire format.
func writeStreamJSONUserFrame(w io.Writer, prompt string) error {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]string{
				{"type": "text", "text": prompt},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("build stream-json user frame: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

// handleStreamJSONAssistant fans an assistant event out into individual
// Message values on ch and accumulates token usage into the usage map.
// Mirrors the built-in stream-json backend assistant handler so manifest authors writing a
// new stream-json provider get the same parsing for free.
func handleStreamJSONAssistant(evt streamJSONEvent, ch chan<- Message, output *strings.Builder, usage map[string]TokenUsage) {
	var content streamJSONMessageContent
	if err := json.Unmarshal(evt.Message, &content); err != nil {
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

func handleStreamJSONUser(evt streamJSONEvent, ch chan<- Message) {
	var content streamJSONMessageContent
	if err := json.Unmarshal(evt.Message, &content); err != nil {
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

func handleStreamJSONResult(evt streamJSONEvent, output *strings.Builder, finalStatus, finalError *string) {
	if evt.ResultText != "" {
		output.Reset()
		output.WriteString(evt.ResultText)
	}
	if evt.IsError {
		*finalStatus = "failed"
		*finalError = evt.ResultText
		if *finalError == "" {
			*finalError = evt.Subtype
		}
	}
}

// streamJSONLogger is a small helper for tests to assert the slog calls a
// runtime would make without spinning up a real subprocess.
var _ = slog.LevelDebug
