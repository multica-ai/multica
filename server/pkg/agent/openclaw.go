package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// openclawBackend implements Backend by spawning `openclaw agent --message <prompt>
// --output-format stream-json --yes` and reading streaming NDJSON events from
// stdout — similar to the opencode backend.
type openclawBackend struct {
	cfg Config
}

func (b *openclawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "openclaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("openclaw executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}
	args := []string{"agent", "--local", "--json", "--session-id", sessionID}
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}
	args = append(args, "--message", prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	// openclaw writes its --json output to stderr, not stdout.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openclaw stderr pipe: %w", err)
	}
	cmd.Stdout = newLogWriter(b.cfg.Logger, "[openclaw:stdout] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start openclaw: %w", err)
	}

	b.cfg.Logger.Info("openclaw started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processOutput(stderr, msgCh)

		// Wait for process exit.
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("openclaw timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("openclaw exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("openclaw finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. OpenClaw doesn't report model per-step, so we
		// attribute all usage to the configured model (or "unknown").
		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
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

// ── Event handlers ──

// openclawEventResult holds accumulated state from processing the event stream.
type openclawEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage
}

// processOutput reads the JSON output from openclaw --json stderr and returns
// the parsed result. OpenClaw writes its JSON result to stderr, which may also
// contain non-JSON log lines. We find the result JSON by trying each '{' until
// one successfully unmarshals as an openclawResult with payloads.
func (b *openclawBackend) processOutput(r io.Reader, ch chan<- Message) openclawEventResult {
	data, err := io.ReadAll(r)
	if err != nil {
		return openclawEventResult{status: "failed", errMsg: fmt.Sprintf("read stderr: %v", err)}
	}

	raw := string(data)

	// Try each '{' position until we find valid openclawResult JSON.
	// Earlier '{' chars may appear in log/error lines (e.g. raw_params={...}).
	var result openclawResult
	jsonStart := -1
	for i := 0; i < len(raw); i++ {
		if raw[i] != '{' {
			continue
		}
		if err := json.Unmarshal([]byte(raw[i:]), &result); err == nil && result.Payloads != nil {
			jsonStart = i
			break
		}
	}

	// Log non-JSON lines before the result
	if jsonStart > 0 {
		for _, line := range strings.Split(raw[:jsonStart], "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				b.cfg.Logger.Debug("[openclaw:stderr] " + line)
			}
		}
	}

	if jsonStart < 0 {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			b.cfg.Logger.Debug("[openclaw:stderr] " + trimmed)
			return openclawEventResult{status: "completed", output: trimmed}
		}
		return openclawEventResult{status: "failed", errMsg: "openclaw returned no parseable output"}
	}

	// Extract text from payloads
	var output strings.Builder
	for _, p := range result.Payloads {
		if p.Text != "" {
			if output.Len() > 0 {
				output.WriteString("\n")
			}
			output.WriteString(p.Text)
		}
	}

	// Extract session ID and usage from meta
	var sessionID string
	var usage TokenUsage
	if result.Meta.AgentMeta != nil {
		if sid, ok := result.Meta.AgentMeta["sessionId"].(string); ok {
			sessionID = sid
		}
		if u, ok := result.Meta.AgentMeta["usage"].(map[string]any); ok {
			usage.InputTokens = openclawInt64(u, "input")
			usage.OutputTokens = openclawInt64(u, "output")
			usage.CacheReadTokens = openclawInt64(u, "cacheRead")
			usage.CacheWriteTokens = openclawInt64(u, "cacheWrite")
		}
	}

	// Send final text as a message
	if output.Len() > 0 {
		trySend(ch, Message{Type: MessageText, Content: output.String()})
	}

	return openclawEventResult{
		status:    "completed",
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
	}
}

// openclawInt64 safely extracts an int64 from a JSON-decoded map value (which
// may be float64 due to Go's JSON number handling).
func openclawInt64(data map[string]any, key string) int64 {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// ── JSON types for `openclaw agent --json` output ──

// openclawResult represents the JSON output from `openclaw agent --json`.
type openclawResult struct {
	Payloads []openclawPayload `json:"payloads"`
	Meta     openclawMeta      `json:"meta"`
}

type openclawPayload struct {
	Text string `json:"text"`
}

type openclawMeta struct {
	DurationMs int64          `json:"durationMs"`
	AgentMeta  map[string]any `json:"agentMeta"`
}
