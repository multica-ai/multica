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
	"strings"
	"time"
)

// traecliBackend implements Backend by spawning ByteDance's Trae Agent CLI in
// its one-shot, non-interactive `trae-cli run "<task>"` mode.
//
// Why `run` and NOT an ACP/serve transport: the upstream trae-agent CLI
// (bytedance/trae-agent, trae_agent/cli.py) exposes exactly four commands —
// `run`, `interactive`, `show-config`, and `tools`. There is no `acp` or
// `serve` subcommand, so Trae cannot be driven over the shared ACP JSON-RPC
// client the way Hermes / Kimi / Kiro / Qoder are. `interactive` requires an
// attached TTY and cannot run under the daemon, which leaves `run` as the only
// daemon-compatible entry point. This mirrors the Antigravity backend's
// one-shot `agy -p` design rather than the ACP backends.
//
// Like Antigravity, the Trae CLI does not expose a structured event stream on
// stdout — the simple console prints plain assistant/tool text. The backend
// therefore streams stdout line-by-line as MessageText events and, after the
// process exits, reads the structured trajectory file Trae writes
// (`--trajectory-file`) to recover the final result, token usage, and a
// success/failure signal. See docs/TRAJECTORY_RECORDING.md upstream for the
// schema.
//
// Session resumption is intentionally unsupported: `trae-cli run` is stateless
// (each invocation starts a fresh agent and writes a new trajectory), so the
// backend never returns a SessionID and ignores opts.ResumeSessionID. The
// daemon falls back to a fresh run for every turn, which is correct for a
// stateless CLI.
//
// System-prompt / runtime-brief delivery: Trae has no --system-prompt flag and
// no documented context-file (CLAUDE.md / AGENTS.md) discovery, so the daemon
// flags Trae in providerNeedsInlineSystemPrompt and the brief arrives via
// opts.SystemPrompt; Execute prepends it to the task text. Skills are written
// to the default .agent_context/skills/ tree, which the inlined brief points
// the agent at for on-demand reading via Trae's file tools.
type traecliBackend struct {
	cfg Config
}

// traecliBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding any of these would
// break non-interactive operation, the daemon's model selection, or the
// trajectory capture the result parser depends on.
var traecliBlockedArgs = map[string]blockedArgMode{
	"run":               blockedStandalone, // the only daemon-compatible subcommand; hardcoded
	"interactive":       blockedStandalone, // requires a TTY, cannot run under the daemon
	"show-config":       blockedStandalone, // not a task-execution subcommand
	"tools":             blockedStandalone, // not a task-execution subcommand
	"-w":                blockedWithValue,  // --working-dir, managed via opts.Cwd
	"--working-dir":     blockedWithValue,
	"-t":                blockedWithValue, // --trajectory-file, managed by the daemon for result capture
	"--trajectory-file": blockedWithValue,
	"-p":                blockedWithValue, // --provider, managed via opts.Model ("provider/model")
	"--provider":        blockedWithValue,
	"-m":                blockedWithValue, // --model, managed via opts.Model
	"--model":           blockedWithValue,
	"-ct":               blockedWithValue, // --console-type, daemon forces "simple"
	"--console-type":    blockedWithValue,
	"-f":                blockedWithValue, // --file, the task is passed as the positional argument
	"--file":            blockedWithValue,
}

// splitTraecliModel maps Multica's single opts.Model string onto Trae's
// separate --provider / --model flags. The catalog (traecliStaticModels) uses
// the `provider/model` ID convention shared with OpenCode, so the provider is
// everything before the first "/" and the model is the remainder. Splitting on
// the FIRST separator preserves model IDs that themselves contain slashes
// (e.g. OpenRouter's "anthropic/claude-3-5-sonnet" →
// provider="openrouter", model="anthropic/claude-3-5-sonnet").
//
// A bare model with no "/" yields an empty provider, in which case the backend
// omits --provider and lets Trae resolve the provider from its trae_config.yaml
// / environment — matching how the user's own `trae-cli run --model X` behaves.
func splitTraecliModel(model string) (provider, name string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}
	if idx := strings.Index(model, "/"); idx > 0 {
		return model[:idx], model[idx+1:]
	}
	return "", model
}

// buildTraecliArgs assembles the argv for a daemon-compatible one-shot
// `trae-cli run` invocation:
//
//	trae-cli run "<task>" --console-type simple --working-dir <abs cwd>
//	    --trajectory-file <tmp> [--provider <p>] [--model <m>] [custom args]
//
// The task text is passed as the positional argument. cwd is always absolutized
// because Trae rejects a non-absolute --working-dir outright.
func buildTraecliArgs(task, trajectoryPath string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"run", task, "--console-type", "simple"}

	if opts.Cwd != "" {
		abs := opts.Cwd
		if a, err := filepath.Abs(opts.Cwd); err == nil {
			abs = a
		}
		args = append(args, "--working-dir", abs)
	}
	args = append(args, "--trajectory-file", trajectoryPath)

	if provider, model := splitTraecliModel(opts.Model); model != "" {
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		args = append(args, "--model", model)
	}

	// ExtraArgs (daemon-wide defaults) are filtered and applied before the
	// per-agent CustomArgs, matching the Antigravity backend's ordering, so a
	// future daemon-wide MULTICA_TRAECLI_ARGS default would compose correctly.
	args = append(args, filterCustomArgs(opts.ExtraArgs, traecliBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, traecliBlockedArgs, logger)...)
	return args
}

// traecliTrajectory is the subset of Trae's trajectory JSON the backend reads
// to classify the run and report usage. The full schema is documented in
// docs/TRAJECTORY_RECORDING.md upstream; fields we don't consume are ignored.
type traecliTrajectory struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	Success         bool    `json:"success"`
	FinalResult     string  `json:"final_result"`
	ExecutionTime   float64 `json:"execution_time"`
	LLMInteractions []struct {
		Response struct {
			Usage struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"response"`
	} `json:"llm_interactions"`
}

// readTraecliTrajectory parses the trajectory file Trae wrote for this run.
// Best-effort: returns (nil, false) if the file is missing or unparseable, in
// which case the caller falls back to stdout + exit-code classification.
func readTraecliTrajectory(path string) (*traecliTrajectory, bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var traj traecliTrajectory
	if err := json.Unmarshal(data, &traj); err != nil {
		return nil, false
	}
	return &traj, true
}

// traecliUsageFromTrajectory folds every recorded LLM interaction into a single
// TokenUsage. Trae records one entry per provider call, so the per-turn total
// is the sum across the trajectory.
func traecliUsageFromTrajectory(traj *traecliTrajectory) TokenUsage {
	var u TokenUsage
	for _, it := range traj.LLMInteractions {
		u.InputTokens += it.Response.Usage.InputTokens
		u.OutputTokens += it.Response.Usage.OutputTokens
		u.CacheReadTokens += it.Response.Usage.CacheReadInputTokens
		u.CacheWriteTokens += it.Response.Usage.CacheCreationInputTokens
	}
	return u
}

func (b *traecliBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "trae-cli"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("trae-cli executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	trajFile, err := os.CreateTemp("", "multica-traecli-trajectory-*.json")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create trae-cli trajectory file: %w", err)
	}
	trajPath := trajFile.Name()
	_ = trajFile.Close()

	// Trae has no --system-prompt; the daemon hands us the runtime brief via
	// opts.SystemPrompt (providerNeedsInlineSystemPrompt) and we prepend it to
	// the task text so the agent still sees the CLI catalog, workflow, identity,
	// and skill references.
	task := prompt
	if opts.SystemPrompt != "" {
		task = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}

	args := buildTraecliArgs(task, trajPath, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	// Log the argv without the (potentially huge, brief-laden) task text.
	b.cfg.Logger.Info("agent command", "exec", execPath, "args_count", len(args), "model", opts.Model)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(trajPath)
		return nil, fmt.Errorf("trae-cli stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[trae-cli:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(trajPath)
		return nil, fmt.Errorf("start trae-cli: %w", err)
	}

	if opts.ResumeSessionID != "" {
		// `trae-cli run` is stateless — there is no session to resume. Log and
		// proceed with a fresh run rather than silently implying continuity.
		b.cfg.Logger.Info("trae-cli is stateless; ignoring resume request and starting a fresh run",
			"requested_session", opts.ResumeSessionID)
	}
	b.cfg.Logger.Info("trae-cli run started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
		defer os.Remove(trajPath)

		startTime := time.Now()
		var output strings.Builder
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		for scanner.Scan() {
			line := scanner.Text()
			if output.Len() > 0 {
				output.WriteByte('\n')
			}
			output.WriteString(line)
			if strings.TrimSpace(line) != "" {
				trySend(msgCh, Message{Type: MessageText, Content: line})
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("trae-cli stdout scanner error", "err", err)
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		traj, haveTraj := readTraecliTrajectory(trajPath)

		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			finalStatus = "timeout"
			finalError = fmt.Sprintf("trae-cli timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			finalStatus = "aborted"
			finalError = "execution cancelled"
		case waitErr != nil:
			// `trae-cli run` calls sys.exit(1) on any execution error.
			finalStatus = "failed"
			finalError = fmt.Sprintf("trae-cli exited with error: %v", waitErr)
		case haveTraj && !traj.Success:
			// Exited 0 but the agent itself reports the task did not complete
			// successfully (e.g. it hit max-steps). Surface as failed so the
			// daemon doesn't record a silent non-completion as success.
			finalStatus = "failed"
			finalError = "trae-cli reported the task did not complete successfully (trajectory success=false)"
		}

		if finalError != "" {
			finalError = withAgentStderr(finalError, "trae-cli", stderrBuf.Tail())
		}

		// Prefer the trajectory's final_result for the captured output when the
		// stdout console produced nothing useful; otherwise keep the streamed
		// console text (which the user already saw event-by-event).
		finalOutput := output.String()
		if haveTraj && strings.TrimSpace(traj.FinalResult) != "" && strings.TrimSpace(finalOutput) == "" {
			finalOutput = traj.FinalResult
		}

		var usageMap map[string]TokenUsage
		if haveTraj {
			if u := traecliUsageFromTrajectory(traj); u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
				model := opts.Model
				if model == "" {
					// Fall back to the model the trajectory recorded so usage is
					// attributed even when no explicit model was requested.
					if traj.Model != "" {
						model = traj.Model
					} else {
						model = "unknown"
					}
				}
				usageMap = map[string]TokenUsage{model: u}
			}
		}

		b.cfg.Logger.Info("trae-cli finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			// Stateless CLI: no resumable session id.
			SessionID: "",
			Usage:     usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
