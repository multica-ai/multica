package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// forgeBlockedArgs are flags the daemon hardcodes for the ForgeCode one-shot
// invocation; user custom_args must not override them.
var forgeBlockedArgs = map[string]blockedArgMode{
	"--prompt":          blockedWithValue, // user prompt is owned by the daemon
	"-p":                blockedWithValue,
	"--conversation-id": blockedWithValue, // daemon pins the conversation id for resume + dump
	"--cid":             blockedWithValue,
	"--directory":       blockedWithValue, // task workdir anchor
	"-C":                blockedWithValue,
}

// forgeBackend implements Backend by spawning `forge -C <cwd> --cid <id> -p
// <prompt>` — ForgeCode's one-shot mode (no TUI).
//
// ForgeCode (forgecode.dev) does NOT emit a streaming JSON/NDJSON event
// protocol like Claude Code or OpenCode. Its `-p` mode renders terminal
// Markdown (with ANSI styling) to stdout. This backend therefore:
//
//   - forwards each stdout line as a text Message (ANSI stripped),
//   - uses the process exit code (0 = completed, 1 = failed) as the
//     authoritative status,
//   - reads structured token usage from a best-effort post-run
//     `forge conversation dump <cid>` (the only machine-readable source).
//
// Model selection: ForgeCode has no --model flag. The active model is set via
// FORGE_SESSION__PROVIDER_ID / FORGE_SESSION__MODEL_ID env vars (or the
// persisted .forge.toml). The daemon translates a "provider/model" value
// (from MULTICA_FORGE_MODEL or the per-agent Model override) into those two
// env vars.
type forgeBackend struct {
	cfg Config
}

func (b *forgeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "forge"
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("forge executable not found at %q: %w", execPath, err)
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// ForgeCode resumes a prior conversation by reusing its conversation id.
	// When the daemon hands us a prior session id we pin it so the run is a
	// continuation; otherwise generate a fresh UUID the post-run dump can
	// read back.
	convID := opts.ResumeSessionID
	if convID == "" {
		convID = uuid.NewString()
	}

	// Build argv. Order mirrors the documented one-shot invocation:
	//   forge [-C <cwd>] [--cid <convID>] [custom_args] -p <prompt>
	args := []string{}
	if opts.Cwd != "" {
		args = append(args, "-C", opts.Cwd)
	}
	args = append(args, "--cid", convID)
	args = append(args, filterCustomArgs(opts.CustomArgs, forgeBlockedArgs, b.cfg.Logger)...)
	args = append(args, "-p", prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	if opts.Cwd != "" {
		// Override PWD so the child process's discovery root matches the
		// task workdir, mirroring the OpenCode backend's reasoning.
		env = append(env, "PWD="+opts.Cwd)
	}
	// ForgeCode selects the model from FORGE_SESSION__PROVIDER_ID /
	// FORGE_SESSION__MODEL_ID. The Model value (from MULTICA_FORGE_MODEL or
	// the per-agent override) is expected in "provider/model" form.
	if modelVal := strings.TrimSpace(opts.Model); modelVal != "" {
		if provider, model, ok := splitForgeModel(modelVal); ok {
			env = append(env, "FORGE_SESSION__PROVIDER_ID="+provider)
			env = append(env, "FORGE_SESSION__MODEL_ID="+model)
		} else {
			b.cfg.Logger.Warn("forge model value is not provider/model; leaving model to .forge.toml", "model", modelVal)
		}
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("forge stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[forge:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start forge: %w", err)
	}

	b.cfg.Logger.Info("forge started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "conversation_id", convID, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so the scanner unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processStream(stdout, msgCh)

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		status := scanResult.status
		errMsg := scanResult.errMsg
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			status = "timeout"
			errMsg = fmt.Sprintf("forge timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			status = "aborted"
			errMsg = "execution cancelled"
		case exitErr != nil && status == "completed":
			// ForgeCode exits 1 on error (main.rs). A non-zero exit with no
			// preceding error marker is still a failure.
			status = "failed"
			errMsg = fmt.Sprintf("forge exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("forge finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())

		// Best-effort structured usage. ForgeCode does not stream token
		// counts, so `forge conversation dump <cid>` is the only source.
		// Failure is non-fatal: the run already succeeded/failed on its
		// own merits, and an older ForgeCode build (or a revoked cid)
		// simply reports no usage.
		usage := b.readConversationUsage(execPath, convID, opts.Cwd, opts.Model)

		resCh <- Result{
			Status:     status,
			Output:     scanResult.output,
			Error:      errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  convID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// forgeStreamResult accumulates state from the stdout scan.
type forgeStreamResult struct {
	status string
	errMsg string
	output string
}

// processStream reads ForgeCode's rendered-Markdown stdout line by line and
// forwards each non-empty line as a text Message. Because ForgeCode has no
// structured event stream, tool calls and token usage are indistinguishable
// from prose in this pass; the post-run conversation dump recovers the
// structured bits where available.
func (b *forgeBackend) processStream(r io.Reader, ch chan<- Message) forgeStreamResult {
	var output strings.Builder
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		// ForgeCode renders ANSI styling for the terminal. Strip it so the
		// forwarded text is readable in the Multica UI.
		clean := stripANSI(line)
		if strings.TrimSpace(clean) == "" {
			continue
		}
		output.WriteString(clean)
		output.WriteString("\n")
		trySend(ch, Message{Type: MessageText, Content: clean})
	}

	if err := scanner.Err(); err != nil {
		b.cfg.Logger.Warn("forge stdout scanner error", "error", err)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", err)
		}
	}

	return forgeStreamResult{status: finalStatus, errMsg: finalError, output: output.String()}
}

// readConversationUsage runs `forge conversation dump <convID>` and extracts
// token usage from the serialized conversation. Returns nil when the dump is
// unavailable, unparseable, or reports no usage — the caller treats nil as
// "no usage reported" rather than a failure.
func (b *forgeBackend) readConversationUsage(execPath, convID, cwd, model string) map[string]TokenUsage {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath, "conversation", "dump", convID)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		b.cfg.Logger.Debug("forge conversation dump failed (non-fatal)", "conversation_id", convID, "error", err, "stderr", strings.TrimSpace(stderr.String()))
		return nil
	}

	usage, err := parseForgeConversationUsage(raw, model)
	if err != nil {
		b.cfg.Logger.Debug("forge conversation dump parse failed (non-fatal)", "conversation_id", convID, "error", err)
		return nil
	}
	return usage
}

// parseForgeConversationUsage extracts token usage from a `forge conversation
// dump <id>` JSON document. The dump schema carries a top-level usage object
// whose fields vary across ForgeCode versions; this parser tolerates the
// known shapes (prompt_tokens / completion_tokens / total_tokens, and the
// input / output aliases) and returns nil when no usable counters are found.
func parseForgeConversationUsage(raw []byte, model string) (map[string]TokenUsage, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse conversation dump json: %w", err)
	}

	u := extractForgeUsage(doc)
	if u == nil {
		return nil, nil
	}
	key := strings.TrimSpace(model)
	if key == "" {
		key = "unknown"
	}
	return map[string]TokenUsage{key: *u}, nil
}

// extractForgeUsage walks a parsed conversation dump looking for a usage
// object. ForgeCode stores accumulated usage under several possible keys
// depending on version ("usage", "token_usage", nested under "stats"); this
// helper checks the common locations and normalizes the field names.
func extractForgeUsage(doc map[string]any) *TokenUsage {
	for _, key := range []string{"usage", "token_usage", "stats"} {
		if v, ok := doc[key]; ok {
			if m, ok := v.(map[string]any); ok {
				if u := forgeUsageFromMap(m); u != nil {
					return u
				}
			}
		}
	}
	// Some dumps nest usage under a "conversation" wrapper.
	if conv, ok := doc["conversation"].(map[string]any); ok {
		for _, key := range []string{"usage", "token_usage", "stats"} {
			if v, ok := conv[key]; ok {
				if m, ok := v.(map[string]any); ok {
					if u := forgeUsageFromMap(m); u != nil {
						return u
					}
				}
			}
		}
	}
	return nil
}

// forgeUsageFromMap reads token counters from a usage-shaped map, tolerating
// both the ForgeCode field names (prompt_tokens, completion_tokens) and the
// OpenAI-style aliases (input, output). Returns nil when no counter is
// present.
func forgeUsageFromMap(m map[string]any) *TokenUsage {
	in, hasIn := forgeIntField(m, "prompt_tokens", "input", "input_tokens")
	out, hasOut := forgeIntField(m, "completion_tokens", "output", "output_tokens")
	if !hasIn && !hasOut {
		return nil
	}
	return &TokenUsage{InputTokens: in, OutputTokens: out}
}

// forgeIntField returns the first non-zero int value found under any of the
// given keys, plus whether any key matched. JSON numbers decode as float64.
func forgeIntField(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				if n != 0 {
					return int64(n), true
				}
			case int:
				if n != 0 {
					return int64(n), true
				}
			case int64:
				if n != 0 {
					return n, true
				}
			}
		}
	}
	return 0, false
}

// splitForgeModel splits a "provider/model" string (e.g.
// "anthropic/claude-sonnet-4-20250514") into the provider id and model id
// ForgeCode expects. Returns ok=false when there is no slash, so the caller
// can leave model selection to the persisted .forge.toml.
func splitForgeModel(s string) (provider, model string, ok bool) {
	idx := strings.Index(s, "/")
	if idx <= 0 || idx >= len(s)-1 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}

// ansiEscape matches CSI and OSC ANSI escape sequences emitted by terminal
// renderers (ForgeCode's stream_renderer uses SGR codes for color/style).
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][AB012]`)

// stripANSI removes ANSI escape sequences from s so the forwarded text is
// readable outside a terminal. ForgeCode's one-shot mode renders styled
// Markdown; without stripping, the Multica UI shows raw escape bytes.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
