package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// opencodeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var opencodeBlockedArgs = map[string]blockedArgMode{
	"--format": blockedWithValue, // json output format for daemon communication
	"--attach": blockedWithValue, // server URL is daemon-managed in keep-alive mode
	"--port":   blockedWithValue, // server port is daemon-managed in keep-alive mode
}

// opencodeBackend implements Backend by spawning `opencode run --format json`
// and reading streaming JSON events from stdout — the same pattern as Claude.
//
// When waitForSubAgents is enabled (default), the daemon also spawns a
// dedicated `opencode serve` process and uses `--attach` so that any
// background sub-agents launched by plugins (e.g. oh-my-openagent) keep
// running after the parent run exits. The daemon then polls the server's
// /session/status endpoint until all sessions are idle before completing.
//
// The waitForSubAgents knob is opencode-specific and intentionally lives
// on this struct rather than on the generic agent.Config so the public
// Backend contract stays uniform across runtimes (claude, codex, …).
// It defaults to enabled and can be turned off via the
// MULTICA_OPENCODE_DISABLE_SUBAGENT_WAIT env var, captured at construction
// time.
type opencodeBackend struct {
	cfg              Config
	waitForSubAgents bool
}

// newOpencodeBackend constructs an opencodeBackend with the wait-for-sub-agents
// behavior resolved from the environment. Tests can mutate waitForSubAgents
// directly after construction.
func newOpencodeBackend(cfg Config) *opencodeBackend {
	return &opencodeBackend{
		cfg:              cfg,
		waitForSubAgents: resolveOpencodeWaitForSubAgents(),
	}
}

func (b *opencodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "opencode"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("opencode executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}

	waitForSubAgents := b.waitForSubAgents

	// Start the persistent server BEFORE the run. We tie its lifetime to ctx
	// (not the per-run timeout context) so a SIGTERM cleanup at the very end
	// of Execute() can wait gracefully even after the run timed out. If the
	// server fails to come up, log and fall back silently to the legacy
	// embedded-server mode so the task still runs.
	var server *opencodeServer
	if waitForSubAgents {
		srv, err := startOpencodeServer(ctx, execPath, buildEnv(b.cfg.Env), b.cfg.Logger)
		if err != nil {
			b.cfg.Logger.Warn("opencode persistent server unavailable, falling back to embedded mode", "error", err)
		} else {
			server = srv
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{"run", "--format", "json"}
	if server != nil {
		args = append(args, "--attach", server.URL)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--prompt", opts.SystemPrompt)
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, opencodeBlockedArgs, b.cfg.Logger)...)
	args = append(args, prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	// Auto-approve all tool use in daemon mode.
	env = append(env, `OPENCODE_PERMISSION={"*":"allow"}`)
	if server != nil {
		// Strip any inherited password and inject the per-task one so `run
		// --attach` authenticates against our private serve process.
		env = stripEnvKey(env, "OPENCODE_SERVER_PASSWORD")
		env = append(env, "OPENCODE_SERVER_PASSWORD="+server.Password)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if server != nil {
			server.Stop(5 * time.Second)
		}
		cancel()
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[opencode:stderr] ")

	if err := cmd.Start(); err != nil {
		if server != nil {
			server.Stop(5 * time.Second)
		}
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	b.cfg.Logger.Info("opencode started",
		"pid", cmd.Process.Pid,
		"cwd", opts.Cwd,
		"model", opts.Model,
		"wait_sub_agents", server != nil,
	)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the run context is cancelled so the scanner unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		// Always tear down the persistent server when this goroutine exits,
		// regardless of how (success, timeout, panic propagation, etc.).
		defer func() {
			if server != nil {
				server.Stop(5 * time.Second)
			}
		}()

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)

		// Wait for the run process to exit.
		exitErr := cmd.Wait()
		runDuration := time.Since(startTime)

		// If the run finished cleanly and we have a persistent server, give
		// the embedded BackgroundManager (oh-my-openagent et al.) a chance
		// to finish its sub-agent sessions before we report completion.
		if server != nil && scanResult.status == "completed" && exitErr == nil && runCtx.Err() == nil {
			b.cfg.Logger.Info("opencode run exited; waiting for sub-agents to finish",
				"session", scanResult.sessionID,
			)
			waitTimeout := remainingTimeout(runCtx, timeout)
			if waitErr := server.WaitForIdle(runCtx, waitTimeout, b.cfg.Logger, scanResult.sessionID); waitErr != nil {
				b.cfg.Logger.Warn("opencode sub-agent wait ended with error",
					"error", waitErr,
				)
			}
		}

		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("opencode timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("opencode exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("opencode finished",
			"pid", cmd.Process.Pid,
			"status", scanResult.status,
			"run_duration", runDuration.Round(time.Millisecond).String(),
			"total_duration", duration.Round(time.Millisecond).String(),
			"wait_sub_agents", server != nil,
		)

		// Build usage map. OpenCode doesn't report model per-step, so we
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

// resolveOpencodeWaitForSubAgents picks the effective wait mode from the
// environment. Default is enabled; MULTICA_OPENCODE_DISABLE_SUBAGENT_WAIT
// set to "1" / "true" / "yes" (case-insensitive, trimmed) disables it.
func resolveOpencodeWaitForSubAgents() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_OPENCODE_DISABLE_SUBAGENT_WAIT"))) {
	case "1", "true", "yes":
		return false
	}
	return true
}

// remainingTimeout returns the time remaining before runCtx's deadline,
// or fallback if the context has no deadline. Always returns at least 5s
// so the wait loop has a chance to make at least one observation.
func remainingTimeout(runCtx context.Context, fallback time.Duration) time.Duration {
	const minWait = 5 * time.Second
	d, ok := runCtx.Deadline()
	if !ok {
		return fallback
	}
	left := time.Until(d)
	if left < minWait {
		return minWait
	}
	return left
}

// stripEnvKey returns env with any KEY=... entries for the given key removed.
func stripEnvKey(env []string, key string) []string {
	prefix := key + "="
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ── Persistent opencode server (`opencode serve` + `run --attach`) ──

// opencodeServer holds a running `opencode serve` process owned by the daemon.
// The server outlives the `opencode run` command so that background sub-agent
// sessions launched by plugins keep running and can be polled to completion.
type opencodeServer struct {
	URL      string             // base URL, e.g. http://127.0.0.1:54321
	Password string             // basic-auth password shared with the attached run
	cmd      *exec.Cmd          // the serve process
	cancel   context.CancelFunc // tied to the server's independent context
	logger   *slog.Logger
}

// opencodeServerListenRe matches the announce line from `opencode serve`,
// e.g. "opencode server listening on http://127.0.0.1:54321".
// Source: opencode/packages/opencode/src/cli/cmd/serve.ts:16.
var opencodeServerListenRe = regexp.MustCompile(`opencode server listening on (https?://[^\s]+)`)

// startOpencodeServer launches `opencode serve` on a random local port and
// blocks (with a 10s handshake timeout) until the server announces its URL.
// On success it returns a handle the caller uses to poll the server and stop
// it. On failure the server process (if any) is reaped before returning.
//
// The server runs under its own background context so a per-run timeout on
// `opencode run` does not kill the server prematurely. Cancellation of the
// caller's parent ctx still propagates because we register a watcher.
func startOpencodeServer(parent context.Context, execPath string, parentEnv []string, logger *slog.Logger) (*opencodeServer, error) {
	password, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate opencode server password: %w", err)
	}

	serverCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(serverCtx, execPath, "serve", "--port", "0", "--hostname", "127.0.0.1")

	serverEnv := stripEnvKey(append([]string{}, parentEnv...), "OPENCODE_SERVER_PASSWORD")
	serverEnv = append(serverEnv, "OPENCODE_SERVER_PASSWORD="+password)
	// The serve process must auto-approve permissions like the run does, so
	// plugin-spawned sub-agents are never blocked on a UI prompt.
	if !envHasKey(serverEnv, "OPENCODE_PERMISSION") {
		serverEnv = append(serverEnv, `OPENCODE_PERMISSION={"*":"allow"}`)
	}
	cmd.Env = serverEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode serve stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(logger, "[opencode-serve:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}

	logger.Info("opencode serve started", "pid", cmd.Process.Pid)

	urlCh := make(chan string, 1)
	scanErrCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		announced := false
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("[opencode-serve:stdout] " + line)
			if !announced {
				if m := opencodeServerListenRe.FindStringSubmatch(line); m != nil {
					urlCh <- m[1]
					announced = true
				}
			}
		}
		if err := scanner.Err(); err != nil && !announced {
			scanErrCh <- err
		}
	}()

	// Watcher: if the parent ctx is cancelled (task abort, daemon shutdown),
	// kill the server too. Also handles the normal serverCtx cancellation
	// path so this goroutine always exits.
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-serverCtx.Done():
		}
	}()

	handshakeDeadline := time.NewTimer(10 * time.Second)
	defer handshakeDeadline.Stop()

	select {
	case url := <-urlCh:
		return &opencodeServer{
			URL:      url,
			Password: password,
			cmd:      cmd,
			cancel:   cancel,
			logger:   logger,
		}, nil
	case err := <-scanErrCh:
		cancel()
		_ = cmd.Wait()
		return nil, fmt.Errorf("opencode serve stdout error before announce: %w", err)
	case <-handshakeDeadline.C:
		cancel()
		_ = cmd.Wait()
		return nil, fmt.Errorf("opencode serve did not announce listen URL within 10s")
	case <-parent.Done():
		cancel()
		_ = cmd.Wait()
		return nil, parent.Err()
	}
}

// Stop terminates the server. Sends SIGTERM and waits up to gracefulTimeout
// for a clean exit; then escalates to SIGKILL. Safe to call multiple times.
func (s *opencodeServer) Stop(gracefulTimeout time.Duration) {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	pid := s.cmd.Process.Pid
	s.logger.Debug("opencode serve: SIGTERM", "pid", pid)
	_ = s.cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Debug("opencode serve: exited cleanly", "pid", pid)
	case <-time.After(gracefulTimeout):
		s.logger.Warn("opencode serve: SIGTERM timeout, sending SIGKILL", "pid", pid)
		_ = s.cmd.Process.Kill()
		<-done
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// WaitForIdle polls the server's /session/status endpoint until 3 successive
// polls return zero non-idle sessions (after optional filtering), the timeout
// elapses, or ctx is done.
//
// /session/status returns a map[sessionID]Status that omits idle sessions
// (cf. opencode session/status.ts:74-78: "data.delete(sessionID)" on idle).
//
// mainSessionID: OpenCode often keeps the **parent** run session in "busy"
// in SessionStatus even after `opencode run` exits (child sessions are
// separate IDs). Waiting for a fully empty map would then block until the
// global timeout. When mainSessionID is non-empty, that key is ignored so we
// only wait for sub-agent / child sessions to go idle.
//
// To avoid completing before child sessions appear in /session/status, idle
// debounce only starts once we have seen a non-main busy session or
// opencodeSubagentIdleGrace has elapsed (see var in opencode.go).
//
// The 3-poll debounce avoids false positives between two sub-agents that
// briefly relay each other (e.g. one finishes, BackgroundManager schedules
// the next ~100ms later: two consecutive empty responses by chance, third
// catches the new session).
func (s *opencodeServer) WaitForIdle(ctx context.Context, timeout time.Duration, logger *slog.Logger, mainSessionID string) error {
	const (
		pollInterval         = 3 * time.Second
		requiredStablePolls  = 3
		httpTimeoutPerPoll   = 10 * time.Second
		maxConsecutiveErrors = 5
	)

	deadline := time.Now().Add(timeout)
	httpClient := &http.Client{Timeout: httpTimeoutPerPoll}
	statusURL := strings.TrimRight(s.URL, "/") + "/session/status"

	consecutiveIdle := 0
	consecutiveErrors := 0
	sawOtherBusy := false
	waitStart := time.Now()
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("waiting for opencode sub-agents timed out after %s", timeout)
		}

		raw, err := s.queryActiveSessions(ctx, httpClient, statusURL)
		if err != nil {
			consecutiveErrors++
			consecutiveIdle = 0
			logger.Warn("opencode serve: status poll failed",
				"error", err,
				"consecutive_errors", consecutiveErrors,
			)
			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("opencode serve: %d consecutive status polls failed; assuming server is dead", consecutiveErrors)
			}
		} else {
			consecutiveErrors = 0
			active := nonIdleSessionsExcludingMain(raw, mainSessionID)
			if len(active) > 0 {
				sawOtherBusy = true
			}
			canTrustIdle := sawOtherBusy || time.Since(waitStart) >= opencodeSubagentIdleGrace
			if len(active) == 0 {
				if !canTrustIdle {
					consecutiveIdle = 0
					logger.Debug("opencode serve: idle poll deferred (sub-agent grace)",
						"elapsed", time.Since(waitStart).Round(time.Second),
						"grace", opencodeSubagentIdleGrace,
					)
				} else {
					consecutiveIdle++
					logger.Debug("opencode serve: idle poll",
						"consecutive", consecutiveIdle,
						"required", requiredStablePolls,
					)
					if consecutiveIdle >= requiredStablePolls {
						return nil
					}
				}
			} else {
				consecutiveIdle = 0
				logger.Debug("opencode serve: still busy",
					"active_sessions", len(active),
					"first", firstKey(active),
				)
			}
		}

		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// queryActiveSessions fetches /session/status and returns the decoded map.
// Exported via interface only conceptually — this is a method to allow
// future swapping in tests via a fake HTTP transport.
func (s *opencodeServer) queryActiveSessions(ctx context.Context, c *http.Client, statusURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", s.Password)

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var sessions map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decode /session/status: %w", err)
	}
	return sessions, nil
}

// ── Small helpers ──

func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

func firstKey(m map[string]any) string {
	for k := range m {
		return k
	}
	return ""
}

// opencodeSubagentIdleGrace is the minimum time after opencode run exits before we
// treat an empty filtered /session/status as "no sub-agents" when we have never
// seen any non-main busy session. This avoids a race: child sessions are created
// asynchronously right after the parent run returns, so the status map can be
// {} or {parent} briefly even while background agents are starting.
var opencodeSubagentIdleGrace = 45 * time.Second

// nonIdleSessionsExcludingMain returns a copy of raw minus mainSessionID.
// Keys in /session/status are already non-idle only; we drop the parent
// session so WaitForIdle tracks child/sub-agent work. If mainSessionID is
// empty, returns raw unchanged.
func nonIdleSessionsExcludingMain(raw map[string]any, mainSessionID string) map[string]any {
	if mainSessionID == "" || len(raw) == 0 {
		return raw
	}
	out := make(map[string]any)
	for k, v := range raw {
		if k == mainSessionID {
			continue
		}
		out[k] = v
	}
	return out
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ── Event handlers ──

// eventResult holds the accumulated state from processing the event stream.
type eventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage // accumulated token usage across all steps
}

// processEvents reads JSON lines from r, dispatches events to ch, and returns
// the accumulated result. This is the core scanner loop, extracted for testability.
func (b *opencodeBackend) processEvents(r io.Reader, ch chan<- Message) eventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event opencodeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.SessionID != "" {
			sessionID = event.SessionID
		}

		switch event.Type {
		case "text":
			b.handleTextEvent(event, ch, &output)
		case "tool_use":
			b.handleToolUseEvent(event, ch)
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			// Accumulate token usage from step_finish events.
			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
		}
	}

	// Check for scanner errors (e.g. broken pipe, read errors).
	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("opencode stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	return eventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
	}
}

func (b *opencodeBackend) handleTextEvent(event opencodeEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

// handleToolUseEvent processes "tool_use" events from opencode. A single
// tool_use event contains both the call and result in part.state when the
// tool has completed (state.status == "completed").
func (b *opencodeBackend) handleToolUseEvent(event opencodeEvent, ch chan<- Message) {
	// Extract input from state.input (the tool invocation parameters).
	var input map[string]any
	if event.Part.State != nil && event.Part.State.Input != nil {
		_ = json.Unmarshal(event.Part.State.Input, &input)
	}

	// Emit the tool-use message.
	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   event.Part.Tool,
		CallID: event.Part.CallID,
		Input:  input,
	})

	// If the tool has completed, also emit a tool-result message.
	if event.Part.State != nil && event.Part.State.Status == "completed" {
		outputStr := extractToolOutput(event.Part.State.Output)
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   event.Part.Tool,
			CallID: event.Part.CallID,
			Output: outputStr,
		})
	}
}

// handleErrorEvent processes "error" events from opencode. OpenCode can exit
// with RC=0 even on errors (e.g. invalid model), so error events are the
// reliable signal for failures.
func (b *opencodeBackend) handleErrorEvent(event opencodeEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown opencode error"
	}

	b.cfg.Logger.Warn("opencode error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

// extractToolOutput converts the tool state output (which may be a string or
// structured object) into a string.
func extractToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

// ── JSON types for `opencode run --format json` stdout events ──

// opencodeEvent represents a single JSON line from `opencode run --format json`.
//
// Event types observed in real output:
//
//	"step_start"  — agent step begins
//	"text"        — text output from agent (part.text)
//	"tool_use"    — tool invocation with call and result (part.tool, part.callID, part.state)
//	"error"       — error from opencode (error.name, error.data.message)
//	"step_finish" — agent step completes (includes token usage)
type opencodeEvent struct {
	Type      string            `json:"type"`
	Timestamp int64             `json:"timestamp,omitempty"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      opencodeEventPart `json:"part"`
	Error     *opencodeError    `json:"error,omitempty"`
}

// opencodeEventPart represents the part field in an opencode event.
type opencodeEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	// Text events
	Text string `json:"text,omitempty"`

	// Tool use events
	Tool   string             `json:"tool,omitempty"`
	CallID string             `json:"callID,omitempty"`
	State  *opencodeToolState `json:"state,omitempty"`

	// step_finish token usage
	Tokens *opencodeTokens `json:"tokens,omitempty"`
}

// opencodeTokens represents token usage in a step_finish event.
type opencodeTokens struct {
	Input  int64                `json:"input"`
	Output int64                `json:"output"`
	Cache  *opencodeCacheTokens `json:"cache,omitempty"`
}

type opencodeCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// opencodeToolState represents the state of a tool invocation.
type opencodeToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
}

// opencodeError represents an error event from opencode.
type opencodeError struct {
	Name string           `json:"name,omitempty"`
	Data *opencodeErrData `json:"data,omitempty"`
}

// Message returns the human-readable error message.
func (e *opencodeError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type opencodeErrData struct {
	Message string `json:"message,omitempty"`
}
