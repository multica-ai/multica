package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// engine7NoParseableOutput mirrors openclawNoParseableOutput for the engine7
// backend. Kept as its own constant so dashboards grepping one provider's
// phrase never accidentally match the other's.
const engine7NoParseableOutput = "engine7 returned no parseable output"

// minEngine7Version is the lowest engine7 version whose `agent --json` writes
// the result blob to stdout (same protocol generation as openclaw 2026.5.5 —
// engine7 is Claude-Code-architecture and inherited the same stream contract).
const minEngine7Version = "7.1.50"

// engine7VersionPattern extracts a three-segment dotted version from
// arbitrary `engine7 --version` output (e.g. "engine7 7.1.50").
var engine7VersionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// engine7BlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Mirrors the openclaw set; engine7
// shares the openclaw CLI surface (agent --message … --output-format
// stream-json --yes family) because both are Claude-Code-architecture CLIs.
var engine7BlockedArgs = map[string]blockedArgMode{
	"--local":         blockedStandalone, // local mode for daemon execution
	"--json":          blockedStandalone, // JSON output for daemon communication
	"--session-id":    blockedWithValue,  // managed by daemon for session resumption
	"--message":       blockedWithValue,  // prompt is set by daemon
	"--model":         blockedWithValue,  // engine7 binds models at registration, not per-run
	"--system-prompt": blockedWithValue,  // injected into --message
}

// engine7Backend implements Backend by spawning
// `engine7 agent --message <prompt> --output-format stream-json --yes` and
// reading the JSON result from stdout. It is protocol-compatible with the
// openclaw backend: engine7 (栖, https://github.com/twinsun — the Twinsun
// family of personal AI agents) uses the same CLI contract, so this backend
// reuses the openclaw argument shape, version gate, and output parser rather
// than duplicating ~700 lines. If engine7's CLI surface ever diverges, this
// file is the seam to grow engine7-specific handling.
type engine7Backend struct {
	cfg Config
}

func (b *engine7Backend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "engine7"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("engine7 executable not found at %q: %w", execPath, err)
	}

	if err := checkEngine7Version(ctx, b.cfg.commandAt(execPath)); err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}
	args := buildEngine7Args(prompt, sessionID, opts, b.cfg.Logger)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "agent")))
	cmd.WaitDelay = 500 * time.Millisecond
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("engine7 stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[engine7:stderr] ")

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start engine7: %w", err)
	}

	b.cfg.Logger.Info("engine7 started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		// engine7 emits the same output shapes as openclaw: a final result
		// blob (whole-buffer fast path) or NDJSON events. The openclaw
		// scanner is shared, not copied.
		scanResult := b.processOutput(stdout, msgCh)

		if scanResult.cutShort {
			b.cfg.Logger.Warn("engine7 delivered its result but did not exit; "+
				"treating the complete result as the protocol boundary",
				"pid", cmd.Process.Pid)
			cancel()
		}

		exitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		switch {
		case scanResult.cutShort:
			// Complete result already in hand; the cancellation below it is ours.
		case runCtx.Err() == context.DeadlineExceeded:
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("engine7 timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		case errors.Is(exitErr, exec.ErrWaitDelay) && scanResult.status == "completed":
			b.cfg.Logger.Warn("engine7 exited cleanly but a descendant held a "+
				"pipe past WaitDelay; delivering the parsed result and dropping "+
				"the stderr tail", "pid", cmd.Process.Pid)
		case exitErr != nil && scanResult.status == "completed":
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("engine7 exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("engine7 finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := scanResult.model
			if model == "" {
				model = opts.Model
			}
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildEngine7Args assembles the argv for a one-shot `engine7 agent` invocation.
// Same contract as buildOpenclawArgs: no --model/--system-prompt on the CLI
// (bound at registration / injected into --message), daemon owns --local,
// --json and --session-id.
func buildEngine7Args(prompt, sessionID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"agent"}
	if opts.OpenclawMode != "gateway" {
		args = append(args, "--local")
	}
	args = append(args, "--json", "--session-id", sessionID)
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}
	customArgs := filterCustomArgs(opts.CustomArgs, engine7BlockedArgs, logger)
	if opts.Model != "" && !customArgsContains(customArgs, "--agent") {
		args = append(args, "--agent", opts.Model)
	}
	args = append(args, customArgs...)

	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n" + prompt
	}
	args = append(args, "--message", prompt)
	return args
}

// checkEngine7Version runs `<execPath> --version` and gates on
// minEngine7Version, mirroring checkOpenclawVersion.
func checkEngine7Version(ctx context.Context, runtimeCmd Command) error {
	ctx, cancel := context.WithTimeout(ctx, detectVersionTimeout)
	defer cancel()

	cmd := runtimeCmd.exec(ctx, "--version")
	hideAgentWindow(cmd)
	raw, err := combinedOutputOwned(cmd, runtimeCmd.logger)
	out := string(raw)
	detected, parsed := parseEngine7Version(out)
	if err != nil {
		if !salvageProbeAnswer(runtimeCmd, "--version", parsed, err) {
			return fmt.Errorf("engine7 --version failed: %w", ExplainExecError(err))
		}
	}
	if !parsed {
		return fmt.Errorf("could not parse engine7 version from output: %q", strings.TrimSpace(out))
	}
	if compareEngine7Version(detected, minEngine7Version) < 0 {
		return fmt.Errorf("engine7 %s is below the minimum supported version %s. Run `npm install -g @twinsun/engine7@latest` to upgrade and try again.", detected, minEngine7Version)
	}
	return nil
}

// parseEngine7Version extracts the first three-segment dotted version from
// `engine7 --version` output.
func parseEngine7Version(raw string) (string, bool) {
	m := engine7VersionPattern.FindString(raw)
	if m == "" {
		return "", false
	}
	return m, true
}

// compareEngine7Version compares two three-segment dotted versions
// numerically. Inputs must be well-formed.
func compareEngine7Version(a, b string) int {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		ai, _ := strconv.Atoi(aParts[i])
		bi, _ := strconv.Atoi(bParts[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

// processOutput reuses the openclaw output parser: engine7 emits the same
// final-result blob / NDJSON shapes. The parse result type is the openclaw
// one; see openclawEventResult for the field meanings.
func (b *engine7Backend) processOutput(r io.Reader, ch chan<- Message) openclawEventResult {
	buf, cutShort, readErr := readOpenclawStdout(r, openclawResultIdleGrace)
	if readErr != nil {
		return openclawEventResult{status: "failed", errMsg: fmt.Sprintf("read stdout: %v", readErr)}
	}

	if result, ok := parseWholeBufferOpenclawResult(buf); ok {
		var output strings.Builder
		res := b.buildEngine7EventResult(result, ch, &output)
		res.cutShort = cutShort
		return res
	}

	scanner := newAgentStreamScanner(bytes.NewReader(buf))

	var output strings.Builder
	var sessionID string
	var model string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string
	gotEvents := false

	var rawLines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if event, ok := tryParseOpenclawEvent(line); ok {
			gotEvents = true
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			switch event.Type {
			case "text":
				if event.Text != "" {
					output.WriteString(event.Text)
					trySend(ch, Message{Type: MessageText, Content: event.Text})
				}
			case "tool_use":
				var input map[string]any
				if event.Input != nil {
					_ = json.Unmarshal(event.Input, &input)
				}
				trySend(ch, Message{
					Type:   MessageToolUse,
					Tool:   event.Tool,
					CallID: event.CallID,
					Input:  input,
				})
			case "tool_result":
				trySend(ch, Message{
					Type:   MessageToolResult,
					Tool:   event.Tool,
					CallID: event.CallID,
					Output: event.Text,
				})
			case "error":
				errMsg := event.errorMessage()
				b.cfg.Logger.Warn("engine7 error event", "error", errMsg)
				trySend(ch, Message{Type: MessageError, Content: errMsg})
				finalStatus = "failed"
				finalError = errMsg
			case "lifecycle":
				phase := event.Phase
				if phase == "error" || phase == "failed" || phase == "cancelled" {
					errMsg := event.errorMessage()
					b.cfg.Logger.Warn("engine7 lifecycle failure", "phase", phase, "error", errMsg)
					trySend(ch, Message{Type: MessageError, Content: errMsg})
					finalStatus = "failed"
					finalError = errMsg
				}
			case "step_start":
				trySend(ch, Message{Type: MessageStatus, Status: "running"})
			case "step_finish":
				if event.Usage != nil {
					u := parseOpenclawUsage(event.Usage)
					usage.InputTokens += u.InputTokens
					usage.OutputTokens += u.OutputTokens
					usage.CacheReadTokens += u.CacheReadTokens
					usage.CacheWriteTokens += u.CacheWriteTokens
				}
			}
			continue
		}

		if result, ok := tryParseOpenclawResult(line); ok {
			gotEvents = true
			res := b.buildEngine7EventResult(result, ch, &output)
			if res.sessionID != "" {
				sessionID = res.sessionID
			}
			if res.model != "" {
				model = res.model
			}
			u := res.usage
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
				usage = u
			}
			continue
		}

		b.cfg.Logger.Debug("[engine7:stdout] " + line)
		rawLines = append(rawLines, line)
	}

	if err := scanner.Err(); err != nil {
		return openclawEventResult{status: "failed", errMsg: fmt.Sprintf("read stdout: %v", err)}
	}

	if !gotEvents {
		trimmed := strings.TrimSpace(strings.Join(rawLines, "\n"))
		if trimmed != "" {
			return openclawEventResult{status: "completed", output: trimmed}
		}
		return openclawEventResult{
			status: "failed",
			errMsg: engine7NoParseableOutput,
		}
	}

	return openclawEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		model:     model,
		usage:     usage,
	}
}

// buildEngine7EventResult adapts the final-result blob to the shared
// openclawEventResult. It duplicates openclawBackend.buildOpenclawEventResult
// (~30 lines) rather than reaching into another backend's method set: the
// logic is provider-neutral (payload text + meta extraction), so if engine7's
// result shape ever diverges this stays the only place to change.
func (b *engine7Backend) buildEngine7EventResult(result openclawResult, ch chan<- Message, output *strings.Builder) openclawEventResult {
	for _, p := range result.Payloads {
		if p.Text != "" {
			output.WriteString(p.Text)
			trySend(ch, Message{Type: MessageText, Content: p.Text})
		}
	}

	var sessionID string
	var model string
	var usage TokenUsage
	if result.Meta.AgentMeta != nil {
		if sid, ok := result.Meta.AgentMeta["sessionId"].(string); ok {
			sessionID = sid
		}
		if m, ok := result.Meta.AgentMeta["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
		if u, ok := result.Meta.AgentMeta["usage"].(map[string]any); ok {
			usage = parseOpenclawUsage(u)
		}
	}

	return openclawEventResult{
		status:    "completed",
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
		model:     model,
	}
}
