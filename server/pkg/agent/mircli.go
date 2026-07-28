package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// miraBackend implements Backend by shelling out to Mir CLI (`mircli`, the
// ByteDance-internal Mira agent client). It is a *knowledge/memory-oriented*
// runtime intended for Q&A, retrieval, PRD drafting, and rubber-duck flows —
// NOT for editing files on the daemon host. mircli's `-p --json` non-interactive
// path executes bash/read/write tools inside the Mira cloud sandbox (e2b), so
// nothing it "runs" ever touches this machine (see comments in mircli.py's
// LOCAL_TOOL_REGISTRY and its `--print` handler). Use traecli / antigravity /
// claude for anything that has to modify the workdir.
//
// Cross-session memory (mira-memory) is still fully available in `-p` mode —
// that's the reason to have a mira backend at all. Long-term recall is stored
// on the Mira server, keyed off the logged-in cookie, so a fresh session_id
// still sees history the user asked it to remember.
//
// mircli returns a single JSON envelope per turn from `-p --json`:
//
//	{
//	  "session_id": "2f75edd1",
//	  "message": {"role": "assistant", "content": "..."},
//	  "duration_ms": 12345,
//	  "model": "Cloud-O-4.7"
//	}
//
// stream-json (`--output-format stream-json`) emits an `init` NDJSON row up
// front carrying session_id, then a `result` NDJSON row with duration_ms and
// the assistant text. We use stream-json because it surfaces session_id
// synchronously before completion, giving the daemon an early resume pointer
// (the same reason traecli/antigravity backends emit MessageStatus with a
// SessionID field early).
//
// Model selection quirk: mircli has NO per-invocation `--model` / `-m` flag.
// The selected model is persisted to `$MIRA_HOME/model` and re-read on every
// process start. When opts.Model is set the backend runs `mircli model <key>`
// once before the actual turn to update that file, then runs the turn. This
// only works safely when the daemon serialises mira turns (which the task queue
// already does per-runtime) — a concurrent mira turn on a different model would
// otherwise overwrite the pointer mid-run. If we ever move mira off the shared
// task queue, revisit this.
type miraBackend struct {
	cfg Config
}

func (b *miraBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "mircli"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("mircli executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// Persist model choice via `mircli model <key>` before running the turn.
	// mircli only reads models from $MIRA_HOME/model at startup, so this is
	// the ONLY way to steer a `-p` turn to a specific model. Fail closed if
	// the model key is not recognised — mircli exits non-zero and prints
	// "unknown model", which is more useful surfaced up front than as a
	// mid-turn opaque failure.
	if strings.TrimSpace(opts.Model) != "" {
		modelCtx, modelCancel := context.WithTimeout(runCtx, 10*time.Second)
		modelCmd := exec.CommandContext(modelCtx, execPath, "model", strings.TrimSpace(opts.Model))
		hideAgentWindow(modelCmd)
		modelCmd.Env = buildEnv(b.cfg.Env)
		modelOut, modelErr := modelCmd.CombinedOutput()
		modelCancel()
		if modelErr != nil {
			cancel()
			return nil, fmt.Errorf("mircli model %s: %w (output: %s)", opts.Model, modelErr, strings.TrimSpace(string(modelOut)))
		}
	}

	args := buildMircliArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mircli stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[mira:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start mircli: %w", err)
	}

	b.cfg.Logger.Info("mircli started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
		var output strings.Builder
		var sessionID string
		var streamModel string
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// mircli's stream-json emits one JSON object per line: an `init`
			// event first (with session_id) then a `result` event with the
			// final assistant text.
			var ev mircliStreamEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				// Non-JSON lines can appear when mircli prints a bootstrap
				// notice before the real stream starts (e.g. the Claude Code
				// AI contribution hook self-check). Log them so operators can
				// investigate but don't fail the turn.
				b.cfg.Logger.Debug("mircli non-json stream line", "line", line)
				continue
			}
			switch ev.Type {
			case "system":
				if ev.Subtype == "init" && ev.SessionID != "" {
					sessionID = ev.SessionID
					if ev.Model != "" {
						streamModel = ev.Model
					}
					// Surface the session id early so the daemon can pin
					// resume pointers before the turn finishes.
					trySend(msgCh, Message{Type: MessageStatus, Status: "session", SessionID: sessionID})
				}
			case "result":
				if ev.SessionID != "" {
					sessionID = ev.SessionID
				}
				text := strings.TrimSpace(ev.Result)
				if text != "" {
					if output.Len() > 0 {
						output.WriteByte('\n')
					}
					output.WriteString(text)
					trySend(msgCh, Message{Type: MessageText, Content: text})
				}
				if ev.Subtype != "" && ev.Subtype != "success" {
					// e.g. "error" — record it so we can classify the turn as
					// failed instead of a silent empty success.
					finalStatus = "failed"
					finalError = fmt.Sprintf("mircli result subtype=%s", ev.Subtype)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("mircli stdout scanner error", "err", err)
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("mircli timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("mircli exited with error: %v", waitErr)
		}
		if finalError != "" {
			finalError = withAgentStderr(finalError, "mircli", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("mircli finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String(), "session_id", sessionID)

		usage := map[string]TokenUsage{}
		// mircli doesn't emit per-turn token usage in stream-json today; leave
		// Usage empty rather than fabricate zeros under `streamModel`. Retain
		// the model string in the log line above so operators can still see
		// which model the turn actually ran on.
		_ = streamModel

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

// mircliStreamEvent is the union shape of mircli's `--output-format stream-json`
// NDJSON rows. See _run_print_mode in ~/.mira/mircli.py: the `system/init` row
// carries session_id+model+cwd+version, the `result` row carries the assistant
// text and duration_ms. Unknown/future fields are intentionally ignored.
type mircliStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Result    string `json:"result"`
}

// mircliBlockedArgs are the flags the daemon owns; user-supplied CustomArgs
// that try to override them are dropped with a warning. Overriding these would
// break session capture (--output-format), resume bookkeeping (--session-id),
// or non-interactive operation (--print / --resume).
var mircliBlockedArgs = map[string]blockedArgMode{
	"-p":                   blockedStandalone,
	"--print":              blockedStandalone,
	"--json":               blockedStandalone, // daemon uses --output-format stream-json instead
	"--output-format":      blockedWithValue,
	"--resume":             blockedStandalone, // daemon manages resumption via ResumeSessionID
	"--session-id":         blockedWithValue,
	"-y":                   blockedStandalone, // daemon always sets yolo
	"--yolo":               blockedStandalone,
	"--append-system-prompt": blockedWithValue, // daemon wires SystemPrompt through this flag
	"--system-prompt-mode": blockedWithValue,
}

// buildMircliArgs assembles the argv for a daemon-compatible mircli invocation:
//
//	mircli -p --output-format stream-json -y
//	    [--session-id <resume-id>]
//	    [--append-system-prompt <SystemPrompt>]
//	    [--query-timeout <duration>]
//	    <prompt>
//
// mircli has no `--model` flag; the daemon steers the model via a preceding
// `mircli model <key>` call in Execute. mircli has no `--cwd`; it inherits from
// the parent process, which is set on the command via cmd.Dir.
func buildMircliArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"-y",
	}
	if strings.TrimSpace(opts.ResumeSessionID) != "" {
		args = append(args, "--session-id", strings.TrimSpace(opts.ResumeSessionID))
	}
	if strings.TrimSpace(opts.SystemPrompt) != "" {
		// mircli's --append-system-prompt appends to the built-in system prompt
		// for the run, matching the semantics of the daemon's SystemPrompt
		// contract (SystemPrompt = "add this on top of the runtime's own").
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.Timeout > 0 {
		// mircli's --query-timeout accepts Go-style duration strings ("5m",
		// "30s"). Feed it opts.Timeout directly rather than a mircli-specific
		// derived value; the daemon's watchdogs already own liveness.
		args = append(args, "--query-timeout", opts.Timeout.String())
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, mircliBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, mircliBlockedArgs, logger)...)
	// mircli treats every non-flag positional as part of the prompt (joined
	// with spaces). Pass it as one arg so shell-sensitive characters and
	// newlines survive intact.
	args = append(args, prompt)
	return args
}

// mircliStaticModels reflects the model catalog mircli itself ships (see
// `mircli model --list`). IDs are the short keys mircli accepts on
// `mircli model <key>`; Labels are the pretty display strings the CLI prints.
// The catalog is baked-in because mircli has no machine-readable
// `--list --json` today — its `--list` output is TUI-formatted with ANSI
// escapes and the current-model marker in column 1. Discovery could later
// parse that once we're willing to maintain the regex; the static list is
// close enough for a knowledge-only backend that will follow user selection.
func mircliStaticModels() []Model {
	return []Model{
		{ID: "opus4.8", Label: "Cloud-O-4.8 (Opus 4.8)", Provider: "mira"},
		{ID: "opus4.7", Label: "Cloud-O-4.7 (Opus 4.7)", Provider: "mira"},
		{ID: "opus4.6", Label: "Cloud-O-4.6 (Opus 4.6)", Provider: "mira"},
		{ID: "gpt5.5", Label: "GPT-5.5", Provider: "openai"},
		{ID: "gpt5.4", Label: "GPT-5.4", Provider: "openai"},
		{ID: "gemini3.1", Label: "Gemini 3.1 Pro", Provider: "google"},
		{ID: "gemini3.5f", Label: "Gemini 3.5 Flash", Provider: "google"},
		{ID: "glm5.1", Label: "GLM-5.1", Provider: "zhipu"},
		{ID: "minimax2.7", Label: "MiniMax M2.7", Provider: "minimax"},
		{ID: "kimi", Label: "Kimi-k2.5", Provider: "kimi"},
		{ID: "seed2pro", Label: "Seed-2.0-Pro", Provider: "bytedance"},
		{ID: "seed2code", Label: "Seed-2.0-Code", Provider: "bytedance"},
		{ID: "deepseek", Label: "DeepSeek-V4-Pro", Provider: "deepseek"},
	}
}

