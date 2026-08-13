package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// zcodeBlockedArgs are the flags the daemon hardcodes for every zcode turn.
// User-configured custom_args cannot override them, because doing so would
// break the daemon↔zcode communication contract. Both the long forms and the
// short aliases are covered so callers cannot smuggle in an override via the
// alias of a managed flag; the value-taking flags are blocked in both the
// "--flag value" and "--flag=value" forms (filterCustomArgs handles both).
//
//	--prompt / -p / --print  : carries the turn text (managed)
//	--json / --stream-json   : selects the output protocol the backend parses
//	--resume / -c / --continue : pins the conversation the daemon continues
//	--cwd                    : the work directory (managed via opts.Cwd)
//	--max-turns              : turn budget (managed via opts.MaxTurns)
var zcodeBlockedArgs = map[string]blockedArgMode{
	"--prompt":      blockedWithValue,
	"-p":            blockedWithValue,
	"--print":       blockedWithValue,
	"--json":        blockedStandalone,
	"--stream-json": blockedStandalone,
	"--resume":      blockedWithValue,
	"-c":            blockedStandalone,
	"--continue":    blockedStandalone,
	"--cwd":         blockedWithValue,
	"--max-turns":   blockedWithValue,
}

// zcodeBackend implements Backend by spawning the ZCode CLI in non-interactive
// JSON summary mode (`zcode --prompt <prompt> --json [--resume <sessionId>]
// [--cwd <dir>]`) and parsing the single JSON object it prints on stdout when
// the turn ends.
//
// This is the CAPABILITY-GATED FALLBACK. The agent.New("zcode") factory
// returns zcodeStreamBackend, which at Execute time probes whether the
// installed zcode CLI advertises --stream-json. The published zcode-app-cli
// only supports --json, so a standard install lands here; a streaming-enabled
// build (zcode-cli-stream, or a future upstream release) takes the streaming
// path instead. Both paths are covered by tests.
//
// ZCode is a terminal client for the official ZCode Desktop agent runtime
// (a GLM-family runtime). The `--prompt --json` mode runs a whole turn and
// prints one summary object:
//
//	{"sessionId":"sess_...","response":"<final text>","usage":{...}}
//
// The backend is therefore non-streaming: Execute starts the process, waits
// for it to exit, parses the summary, and emits exactly one MessageText
// followed by the Result. There is no incremental text while the agent
// works — the daemon's inactivity watchdog sees a single message at the end.
//
// Session continuity: zcode identifies sessions by a `sess_...` id returned
// in the summary. Execute returns it as Result.SessionID; the daemon feeds
// it back as ExecOptions.ResumeSessionID, which the backend forwards as
// `--resume <id>` so the next turn continues the same conversation.
//
// Model selection: zcode has no command-line model override; the model is
// fixed by ~/.zcode/cli/config.json (`model.main`). ModelSelectionSupported
// returns false for zcode so the UI renders a disabled "Managed by runtime"
// picker instead of advertising a knob that silently does nothing.
type zcodeBackend struct {
	cfg Config
}

// zcodeJSONResult is the shape of `zcode --prompt <text> --json`'s stdout.
// Only the fields the backend consumes are decoded; the rest (traceId,
// turnId, eventCount, projection) are ignored.
type zcodeJSONResult struct {
	SessionID string `json:"sessionId"`
	Response  string `json:"response"`
	Usage     struct {
		Source            string `json:"source"`
		ModelRequestCount int    `json:"modelRequestCount"`
		InputTokens       int64  `json:"inputTokens"`
		OutputTokens      int64  `json:"outputTokens"`
		TotalTokens       int64  `json:"totalTokens"`
		CacheReadTokens   int64  `json:"cacheReadTokens"`
		CacheWriteTokens  int64  `json:"cacheWriteTokens"`
		ReasoningTokens   int64  `json:"reasoningTokens"`
	} `json:"usage"`
}

func (b *zcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("zcode prompt must not be empty")
	}

	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "zcode"
	}
	if _, err := exec.LookPath(execName); err != nil {
		return nil, fmt.Errorf("zcode executable not found at %q: %w", execName, err)
	}

	// Capability gate: this backend drives the --json summary protocol
	// (--prompt/--json/--resume and the {sessionId,response,usage} shape) that
	// every published zcode-app-cli build supports. An install that advertises
	// neither --json nor --stream-json is not a compatible ZCode build — fail
	// closed with a clear message rather than spawning a CLI that errors on
	// --json. Gated on the --help flag surface (not a semver floor) because
	// zcode-app-cli's version string is non-standard and drifts across releases.
	if !zcodeSupportsJSONSummary(execName) {
		return nil, fmt.Errorf("zcode at %q does not advertise the --json summary protocol; install a compatible zcode-cli build (needs --prompt/--json)", execName)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildZcodeArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execName, args...)
	hideAgentWindow(cmd)
	// Run zcode in its own process group so cancellation reaches the whole
	// tree — zcode plus any tool subprocess it spawns — not just the direct
	// child. The teardown is shared with the streaming backend via
	// startAgentProcessGroupCancel below; without it a grandchild holding the
	// stdout pipe would keep the blocking read (and thus Result) pending after
	// cancellation, leaving only the daemon's idle watchdog to end the run
	// while the orphan kept running.
	configureAgentProcessGroup(cmd)
	// args contain the task prompt; never expose it in daemon logs.
	b.cfg.Logger.Info("agent command", "exec", execName, "provider", "zcode")
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	// zcode writes its JSON summary to stdout and human-readable diagnostics to
	// stderr. Capture stdout fully via the pipe (it is the turn result) and
	// capture stderr into both the daemon log and a bounded tail, so
	// auth/config/network/invalid-resume failures surface to the user instead
	// of an opaque exit status.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[zcode:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start zcode: %w", err)
	}
	b.cfg.Logger.Info("zcode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 1)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead/reused pid.
	procDone := make(chan struct{})
	startAgentProcessGroupCancel(cmd, runCtx, procDone, stdout, zcodeTerminateGrace())

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()

		// zcode runs a full turn before exiting; read stdout fully (the JSON
		// summary is the turn result). EOF arrives once zcode AND every
		// subprocess it spawned have closed stdout — the process-group teardown
		// above guarantees that even under cancellation, so the read cannot hang
		// on a wedged descendant.
		stdoutBytes, readErr := io.ReadAll(stdout)
		exitErr := cmd.Wait()
		close(procDone)
		durationMs := time.Since(startTime).Milliseconds()
		stderrTail := stderrBuf.Tail()

		failed := exitErr != nil || readErr != nil
		// resumeWasRejected inspects the failure text (error + stderr) for the
		// phrases zcode prints when --resume points at a missing/dead session,
		// so the daemon can fall back to a fresh session instead of looping on
		// the rejected id. emitted="" because the JSON summary carries no
		// session id on failure, so only the phrase match decides.
		resumeRejected := resumeWasRejected(opts.ResumeSessionID, "", failed,
			fmt.Sprintf("zcode exited: %v", exitErr), stderrTail)

		// A context-deadline exit is reported as a timeout so the daemon
		// surfaces it correctly rather than as an opaque failure.
		if exitErr != nil && runCtx.Err() == context.DeadlineExceeded {
			errMsg := withAgentStderr(fmt.Sprintf("zcode timed out after %s", timeout), "zcode", stderrTail)
			resCh <- Result{Status: "timeout", Error: errMsg, DurationMs: durationMs, ResumeRejected: resumeRejected}
			return
		}
		if exitErr != nil {
			errMsg := withAgentStderr(fmt.Sprintf("zcode exited: %v", exitErr), "zcode", stderrTail)
			resCh <- Result{Status: "failed", Error: errMsg, DurationMs: durationMs, ResumeRejected: resumeRejected}
			return
		}
		if readErr != nil {
			errMsg := withAgentStderr(fmt.Sprintf("zcode: read stdout: %v", readErr), "zcode", stderrTail)
			resCh <- Result{Status: "failed", Error: errMsg, DurationMs: durationMs, ResumeRejected: resumeRejected}
			return
		}

		var result zcodeJSONResult
		if err := json.Unmarshal(stdoutBytes, &result); err != nil {
			// Malformed summary: surface the parse error with the bounded stdout
			// sample and stderr tail so the user can see what zcode printed.
			errMsg := withAgentStderr(fmt.Sprintf("zcode: parse JSON summary: %v", err), "zcode", stderrTail)
			resCh <- Result{
				Status: "failed", Output: truncateForLog(stdoutBytes), Error: errMsg,
				DurationMs: durationMs, ResumeRejected: resumeRejected,
			}
			return
		}

		// Emit the single assistant text. Non-streaming backends deliver the
		// whole response at once; the daemon renders it the same way as a
		// streamed final message.
		if response := strings.TrimSpace(result.Response); response != "" {
			trySend(msgCh, Message{Type: MessageText, Content: response})
		}

		usage := map[string]TokenUsage{}
		// The Usage map is keyed by model name; zcode does not report which
		// model served the turn in the summary, so key it under the runtime
		// label. The daemon's usage rollup treats the key as an opaque bucket.
		usage["zcode"] = TokenUsage{
			InputTokens:      result.Usage.InputTokens,
			OutputTokens:     result.Usage.OutputTokens,
			CacheReadTokens:  result.Usage.CacheReadTokens,
			CacheWriteTokens: result.Usage.CacheWriteTokens,
		}

		resCh <- Result{
			Status:     "completed",
			Output:     result.Response,
			SessionID:  result.SessionID,
			Usage:      usage,
			DurationMs: durationMs,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildZcodeArgs constructs the argv for a zcode turn. The prompt is passed
// via --prompt (zcode does not read piped stdin like pi does; an empty
// --prompt value is rejected upstream, and stdin is unused), --json selects
// the summary format the backend parses, and --resume continues a prior
// session when the daemon provides one. Custom args (per-agent then
// daemon-wide) are appended last so they can extend zcode without overriding
// the protocol-critical flags blocked above.
func buildZcodeArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"--prompt", prompt, "--json"}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	// ExtraArgs (MULTICA_ZCODE_ARGS, daemon-wide) precede CustomArgs
	// (per-agent), matching the documented precedence and the other backends
	// that accept both.
	args = append(args, filterCustomArgs(opts.ExtraArgs, zcodeBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, zcodeBlockedArgs, logger)...)
	return args
}

// truncateForLog bounds how much of a failed turn's stdout is embedded in an
// error string — zcode's --json summary is small, but a misconfigured runtime
// that prints to stdout could be arbitrarily large.
func truncateForLog(b []byte) string {
	const max = 512
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
