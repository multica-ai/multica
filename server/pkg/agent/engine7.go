package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// engine7NoParseableOutput is the engine7-specific no-output phrase. Kept as
// its own constant so dashboards grepping one provider's phrase never
// accidentally match the other's.
const engine7NoParseableOutput = "engine7 returned no parseable output"

// minEngine7Version is the lowest engine7 version whose `cc-run` speaks the
// full Claude Code stream-json protocol this backend drives: bare -p with
// stdin prompt delivery landed in 7.1.63 (bf1b4cbc), --model validation in
// 7.1.65 (d7df89b1). Older versions mis-parse a valueless -p as taking the
// next flag as the prompt.
const minEngine7Version = "7.1.65"

// engine7VersionPattern extracts a three-segment dotted version from
// arbitrary `engine7 --version` output (e.g. "engine7 7.1.50").
var engine7VersionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// engine7BlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Mirrors the claude set: engine7
// cc-run shares the Claude Code headless surface (-p … --output-format
// stream-json). The Twinsun team develops and integration-tests against the
// CC protocol generation (cc-run), so the CC dialect is the maintained one;
// the openclaw `agent --message` surface was never exercised against the
// daemon.
var engine7BlockedArgs = map[string]blockedArgMode{
	"-p":              blockedStandalone, // non-interactive mode
	"--output-format": blockedWithValue,  // stream-json protocol
	"--input-format":  blockedWithValue,  // daemon feeds stdin
	"--verbose":       blockedStandalone, // stream-json requires verbose
	"--model":         blockedWithValue,  // per-run model selection
	"--session-id":    blockedWithValue,  // managed by daemon
	"--resume":        blockedWithValue,  // daemon owns session resumption
	"--max-turns":     blockedWithValue,  // daemon budget control
}

// engine7Backend implements Backend by spawning
// `engine7 cc-run -p --output-format stream-json --input-format stream-json`
// and reading CC stream-json events from stdout. engine7 (栖 — the Twinsun
// family of personal AI agents) speaks the Claude Code headless protocol via
// its cc-run subcommand, so this backend reuses the claude argv shape, stdin
// writer, and stream parser helpers (claudeSDKMessage / handleAssistant /
// handleUser / finalizeStreamResult are shared at package level). If
// engine7's CLI surface ever diverges, this file is the seam to grow
// engine7-specific handling.
type engine7Backend struct {
	cfg Config
	// claude lends its event handlers for the CC stream-json scanner loop;
	// engine7 owns only the spawn (argv + stdin) and the version gate.
	claude *claudeBackend
}

func newEngine7Backend(cfg Config) *engine7Backend {
	return &engine7Backend{cfg: cfg, claude: &claudeBackend{cfg: cfg}}
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

	args := buildEngine7Args(opts, b.cfg.Logger)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "cc-run")))
	cmd.WaitDelay = 500 * time.Millisecond
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("engine7 stdin pipe: %w", err)
	}
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

	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }

	// The prompt travels as the first stdin user-message frame (CC stream-json
	// input contract); cc-run is spawned with a bare -p (daemon 0.4.38
	// protocol: no inline value) and reads the task from stdin.
	writeDone := make(chan error, 1)
	go func() {
		err := writeClaudeInput(stdin, prompt)
		if err != nil {
			closeStdin()
		}
		writeDone <- err
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer closeStdin()

		startTime := time.Now()
		var lastAssistantText string
		var finalResultText string
		sawResult := false
		resultIsError := false
		terminalReasonError := ""
		var sessionID string
		usage := make(map[string]TokenUsage)

		scanner := newAgentStreamScanner(stdout)

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
				turn := b.claude.handleAssistant(msg, msgCh, usage)
				lastAssistantText = turn.resolveFallback(lastAssistantText)
			case "user":
				b.claude.handleUser(msg, msgCh)
			case "system":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				sawResult = true
				finalResultText = msg.ResultText
				resultIsError = msg.IsError
				terminalReasonError = claudeTerminalReasonFailure(msg.TerminalReason, msg.ResultText)
				sessionID = msg.SessionID
				if resultUsage := claudeResultUsage(msg, opts.Model); len(resultUsage) > 0 {
					usage = resultUsage
				}
				closeStdin()
			case "log":
				if msg.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   msg.Log.Level,
						Content: msg.Log.Message,
					})
				}
			case "control_request":
				b.claude.handleControlRequest(msg, stdin)
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}

		closeStdin()

		exitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)
		writeErr := <-writeDone

		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"engine7",
			timeout,
			runCtx.Err(),
			writeErr,
			exitErr,
			sessionID,
			streamTerminalState{
				lastAssistantText:   lastAssistantText,
				finalResultText:     finalResultText,
				sawResult:           sawResult,
				resultIsError:       resultIsError,
				scanErr:             scanErr,
				terminalReasonError: terminalReasonError,
			},
			"",
		)

		b.cfg.Logger.Info("engine7 finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError),
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildEngine7Args assembles the argv for a one-shot `engine7 cc-run`
// invocation. Same contract as buildClaudeArgs: the daemon owns the protocol
// flags and feeds the prompt via stdin; user custom_args ride along except
// for the blocked set.
func buildEngine7Args(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"cc-run",
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	customArgs := filterCustomArgs(opts.CustomArgs, engine7BlockedArgs, logger)
	args = append(args, customArgs...)
	return args
}

// checkEngine7Version runs `<execPath> --version` and gates on
// minEngine7Version.
func checkEngine7Version(ctx context.Context, runtimeCmd Command) error {
	ctx, cancel := context.WithTimeout(ctx, detectVersionTimeout)
	defer cancel()

	cmd := runtimeCmd.exec(ctx, "--version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get engine7 version: %w", err)
	}

	m := engine7VersionPattern.FindStringSubmatch(string(output))
	if m == nil {
		return fmt.Errorf("could not parse engine7 version from %q", strings.TrimSpace(string(output)))
	}
	detected := fmt.Sprintf("%s.%s.%s", m[1], m[2], m[3])
	if compareEngine7Version(detected, minEngine7Version) < 0 {
		return fmt.Errorf("engine7 %s is below the minimum supported version %s. Run `npm install -g engine7@latest` to upgrade and try again.", detected, minEngine7Version)
	}
	return nil
}

// compareEngine7Version compares two dotted versions numerically, segment by
// segment. Returns -1/0/+1.
func compareEngine7Version(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var ai, bi int
		if i < len(aParts) {
			ai, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bi, _ = strconv.Atoi(bParts[i])
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}
