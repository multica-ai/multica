package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// museappBackend implements Backend by delegating to a remote Muse receptionist
// over HTTP. Unlike every other backend there is no local CLI to spawn: the
// daemon advertises a museapp runtime exactly when MULTICA_MUSEAPP_ENDPOINT is configured
// (see probeAgentCLIs), and Execute POSTs the prompt to the receptionist,
// streams its progress events into Messages, and resolves Result when the
// receptionist reports a terminal state.
//
// Wire protocol (v1):
//
//	POST /v1/execute            {prompt, session_id?, timeout_s?, workdir?} -> {task_id}
//	GET  /v1/tasks/{id}        -> {status, result?, error?}
//	GET  /v1/tasks/{id}/events -> {events: [{seq, type, content}]}
//	POST /v1/tasks/{id}/cancel -> {execution_stopped?}
//	GET  /v1/health            -> {protocol_version, version}
//
// "result" on a completed task is the final answer. Event types are exactly
// text, thinking and tool; text events are running commentary streamed into
// Messages and are used as the answer only when "result" is empty. Once a task
// is terminal no further events are written.
//
// Configuration comes from the daemon's own process environment so no CLI
// discovery is involved:
//
//	MULTICA_MUSEAPP_ENDPOINT  base URL of the receptionist, e.g. http://127.0.0.1:8765.
//	               Keep this on loopback: the bearer token is sent in the
//	               clear over plain http, so a non-loopback http:// URL
//	               would expose it on the network.
//	MULTICA_MUSEAPP_TOKEN     bearer token for the receptionist (optional; sent only when set)
//	MULTICA_MUSEAPP_MODEL     informational model label reported in the probe entry
//
// Endpoint and receptionist-token resolution deliberately ignore cfg.Env.
// It carries the agent's custom_env,
// which any member who can edit the agent controls; honouring MULTICA_MUSEAPP_ENDPOINT
// there would let them point this daemon at an arbitrary URL, and MULTICA_MUSEAPP_TOKEN
// (which they cannot see) would still ride along from the process environment.
// The probe only ever reads the process environment, so reading anything else
// here would also mean probing one receptionist and running tasks on another.
type museappBackend struct {
	cfg Config
}

// museappProtocolVersion is the receptionist wire protocol this backend speaks.
// ProbeMuseAppReceptionist refuses a receptionist reporting anything else so a
// protocol skew fails at probe time, not mid-task.
const museappProtocolVersion = 1

// ErrMuseAppProtocolSkew is returned by ProbeMuseAppReceptionist when the
// receptionist speaks a different protocol version than this backend.
// It is a sentinel so the daemon can distinguish deterministic protocol
// incompatibility (demotable) from transient network failures.
var ErrMuseAppProtocolSkew = fmt.Errorf("museapp backend: protocol skew")

const (
	museappEnvEndpoint = "MULTICA_MUSEAPP_ENDPOINT"
	museappEnvToken    = "MULTICA_MUSEAPP_TOKEN"
	museappEnvModel    = "MULTICA_MUSEAPP_MODEL"
)

// museappPollInterval is the steady-state cadence for task status/event polls.
// The receptionist is expected to be local or near-local (loopback or
// tailnet); 500ms keeps the transcript live without hammering it.
// Atomic so tests can shrink it without racing concurrent poll loops.
var museappPollInterval atomic.Int64 // nanoseconds

// museappMaxConsecutivePollErrors bounds transient network failures before the
// run is failed. Ten misses at the poll interval is ~5s of silence — long
// enough to ride out a blip, short enough not to wedge a task on a dead
// receptionist. Atomic for the same test reason as museappPollInterval.
var museappMaxConsecutivePollErrors atomic.Int32

func init() {
	museappPollInterval.Store(int64(500 * time.Millisecond))
	museappMaxConsecutivePollErrors.Store(10)
}

type museappEndpointConfig struct {
	endpoint string
	token    string
}

func resolveMuseAppConfig() (museappEndpointConfig, error) {
	endpoint := strings.TrimSpace(os.Getenv(museappEnvEndpoint))
	if strings.TrimSpace(endpoint) == "" {
		return museappEndpointConfig{}, fmt.Errorf("museapp backend: %s is not set; configure the receptionist URL to enable the museapp runtime", museappEnvEndpoint)
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return museappEndpointConfig{}, fmt.Errorf("museapp backend: invalid %s %q: %w", museappEnvEndpoint, endpoint, err)
	}
	return museappEndpointConfig{
		endpoint: endpoint,
		token:    strings.TrimSpace(os.Getenv(museappEnvToken)),
	}, nil
}

func (b *museappBackend) authHeader(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// museappHTTPClient is a package var so tests can observe transport behavior;
// the default has a bounded timeout so a wedged receptionist cannot hang
// Execute forever outside ctx cancellation.
var museappHTTPClient = &http.Client{Timeout: 30 * time.Second}

type museappExecuteRequest struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
	TimeoutS  int64  `json:"timeout_s,omitempty"`
	// WorkDir is the daemon-prepared task working directory (issue context,
	// AGENTS.md runtime brief). The receptionist runs the agent with this
	// directory so file-relative task context resolves. This assumes the
	// daemon and the receptionist share a filesystem — the supported
	// deployment runs both on the same host.
	WorkDir string `json:"workdir,omitempty"`
	// TaskToken is the task-scoped Multica API credential (mat_...). The
	// receptionist hands it to the worker so the worker can call the Multica
	// API (read issues, post comments) with the same scoped identity a CLI
	// backend gets via MULTICA_TOKEN. Omitted when the daemon has none.
	TaskToken string `json:"task_token,omitempty"`
	// ServerURL is the Multica API base URL. The worker needs it to know
	// where to call; CLI backends get it as MULTICA_SERVER_URL.
	ServerURL string `json:"server_url,omitempty"`
	// WorkspaceID is the workspace UUID. The worker passes it as a query
	// param on API calls; CLI backends get it as MULTICA_WORKSPACE_ID.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// ProtocolVersion is the wire protocol version this backend speaks.
	// The receptionist MUST validate it BEFORE creating the task and
	// reject with 400 if incompatible. This prevents the "task accepted
	// then rejected" race where a task is queued but the backend refuses
	// to poll it.
	ProtocolVersion int `json:"protocol_version"`
}

type museappExecuteResponse struct {
	TaskID          string `json:"task_id"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

type museappTaskStatus struct {
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// Cancellation or timeout bookkeeping alone does not prove a worker stopped.
	ExecutionStopped bool `json:"execution_stopped,omitempty"`
}

type museappCancelResponse struct {
	ExecutionStopped bool `json:"execution_stopped"`
}

type museappHTTPStatusError struct {
	code    int
	message string
}

func (e *museappHTTPStatusError) Error() string { return e.message }

type museappEvent struct {
	Seq     int64  `json:"seq"`
	Type    string `json:"type"` // "text" | "thinking" | "tool"
	Content string `json:"content"`
	Tool    string `json:"tool,omitempty"`
}

type museappEventsResponse struct {
	Events []museappEvent `json:"events"`
}

type museappHealthResponse struct {
	ProtocolVersion int    `json:"protocol_version"`
	Version         string `json:"version"`
}

// ProbeMuseAppReceptionist checks that a receptionist is reachable at endpoint
// and speaks the wire protocol this backend implements. It is the museapp
// equivalent of CLI version detection and backs the daemon's availability
// probe (no binary exists to --version). A protocol skew is a hard error so
// it surfaces at probe time rather than mid-task.
func ProbeMuseAppReceptionist(ctx context.Context, endpoint, token string) (string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return "", fmt.Errorf("museapp backend: empty endpoint")
	}
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return "", fmt.Errorf("museapp backend: invalid endpoint %q: %w", endpoint, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/health", nil)
	if err != nil {
		return "", fmt.Errorf("museapp backend: build health request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := museappHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("museapp backend: receptionist unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("museapp backend: read health response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("museapp backend: health check HTTP %d: receptionist rejected credentials (check MULTICA_MUSEAPP_TOKEN)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("museapp backend: health check HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var health museappHealthResponse
	if err := json.Unmarshal(raw, &health); err != nil {
		return "", fmt.Errorf("museapp backend: decode health response: %w", err)
	}
	if health.ProtocolVersion != museappProtocolVersion {
		return "", fmt.Errorf("%w: receptionist speaks v%d, backend implements v%d",
			ErrMuseAppProtocolSkew, health.ProtocolVersion, museappProtocolVersion)
	}
	if health.Version == "" {
		health.Version = "unknown"
	}
	return health.Version, nil
}

func (b *museappBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("muse prompt must not be empty")
	}
	mc, err := resolveMuseAppConfig()
	if err != nil {
		return nil, err
	}
	logger := b.cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// B1: Establish a local deadline via runContext (Hermes pattern).
	// opts.Timeout bounds the entire execution locally; the receptionist
	// also receives timeout_s as a hint, but the local context is authoritative.
	// NOTE: Do NOT defer cancel() here — Execute returns immediately with
	// the Session; the goroutine below owns the context lifetime and
	// cancels it when the run finishes.
	runCtx, cancel := runContext(ctx, opts.Timeout)
	if err := runCtx.Err(); err != nil {
		cancel()
		return nil, err
	}

	// timeoutS rounds up: a sub-second timeout must not truncate to 0,
	// which would tell the receptionist "no timeout".
	// When opts.Timeout is 0 (no local deadline), send an explicit 24h
	// rather than 0: the receptionist defaults unset/0 to 1h, which would
	// kill long tasks the Go side is happy to wait for.
	timeoutS := int64(24 * 3600)
	if opts.Timeout > 0 {
		timeoutS = int64((opts.Timeout + time.Second - 1) / time.Second)
	}
	// Go-B3: Verify protocol compatibility before submitting.
	// The probe runs at daemon startup; the receptionist could upgrade
	// between probe and execute. The execute response carries the
	// receptionist's protocol version; postExecute validates it.
	// (A separate health check would add a round trip and break
	// callers without a health endpoint.)
	var execResp museappExecuteResponse
	if err := b.postExecute(runCtx, mc, museappExecuteRequest{
		Prompt:          museappTaskPrompt(prompt, b.cfg.Env, opts, runtime.GOOS),
		SessionID:       opts.ResumeSessionID,
		TimeoutS:        timeoutS,
		WorkDir:         opts.Cwd,
		TaskToken:       opts.TaskToken,
		ServerURL:       opts.MulticaServerURL,
		WorkspaceID:     opts.MulticaWorkspaceID,
		ProtocolVersion: museappProtocolVersion,
	}, &execResp); err != nil {
		cancel()
		var rejected *museappHTTPStatusError
		if errors.As(err, &rejected) && (rejected.code == 400 || rejected.code == 401 || rejected.code == 403) {
			return nil, err // Explicit refusal before a task could be queued.
		}
		if execResp.TaskID != "" {
			if cleanupErr := b.cancelRemoteTask(mc, execResp.TaskID); cleanupErr == nil {
				return nil, err
			} else {
				err = errors.Join(err, cleanupErr)
			}
		}
		return museappUnconfirmedSession(err), nil
	}
	if execResp.TaskID == "" {
		cancel()
		return museappUnconfirmedSession(fmt.Errorf("museapp backend: receptionist returned an empty task id")), nil
	}
	taskID := execResp.TaskID
	logger.Info("muse task started", "task_id", taskID, "cwd", opts.Cwd, "model", opts.Model)
	// The wire protocol has no model field; the worker runs as the user's
	// personal Muse with its own context. Log ignored options explicitly
	// (Hermes pattern) so silent drops are visible.
	if opts.SystemPrompt != "" {
		logger.Debug("muse ignoring ExecOptions.SystemPrompt; worker uses its own user context")
	}
	if opts.MaxTurns > 0 {
		logger.Debug("muse ignoring ExecOptions.MaxTurns; wire protocol has no turn limit", "max_turns", opts.MaxTurns)
	}
	if len(opts.McpConfig) > 0 {
		logger.Debug("muse ignoring ExecOptions.McpConfig; not applicable to remote worker")
	}
	if opts.ThinkingLevel != "" {
		logger.Debug("muse ignoring ExecOptions.ThinkingLevel; not applicable to remote worker", "thinking_level", opts.ThinkingLevel)
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	var terminalObserved atomic.Bool

	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer cancel() // Release the runContext when the run finishes.
		startTime := time.Now()
		// output is what the text events add up to: running commentary
		// ("looking that up…"), not the answer. It is the answer only when
		// the receptionist reports none. answer is the receptionist's own
		// final result and wins whenever it is set.
		var output strings.Builder
		var answer string
		lastSeq := int64(0)
		finalStatus := "completed"
		var finalError string
		pollErrs := 0

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		finish := func() {
			out := output.String()
			if answer != "" {
				out = answer
			}
			resCh <- Result{
				Status:     finalStatus,
				Output:     out,
				Error:      finalError,
				DurationMs: time.Since(startTime).Milliseconds(),
				SessionID:  "muse:" + taskID,
			}
		}
		stopRemote := func() {
			if err := b.cancelRemoteTask(mc, taskID); err != nil {
				finalStatus = "cleanup_unconfirmed"
				finalError += "; " + err.Error()
			}
		}

		// applyEvents pulls the events after lastSeq and folds them into the
		// transcript and the answer. It is best-effort and reports whether the
		// fetch succeeded.
		applyEvents := func() bool {
			var eventsResp museappEventsResponse
			if err := b.getJSON(runCtx, mc, museappTaskPath(taskID, "/events?since="+strconv.FormatInt(lastSeq, 10)), &eventsResp); err != nil {
				logger.Debug("muse events poll failed", "task_id", taskID, "error", err)
				return false
			}
			for _, e := range eventsResp.Events {
				// The receptionist may ignore ?since= and return the full log;
				// seq is the dedup key (the contract guarantees it is
				// monotonically increasing per task).
				if e.Seq <= lastSeq {
					continue
				}
				lastSeq = e.Seq
				switch e.Type {
				case "thinking":
					trySend(msgCh, Message{Type: MessageThinking, Content: e.Content})
				case "tool":
					trySend(msgCh, Message{Type: MessageToolUse, Tool: e.Tool, Content: e.Content})
				case "text":
					if e.Content != "" {
						output.WriteString(e.Content)
						trySend(msgCh, Message{Type: MessageText, Content: e.Content})
					}
				default:
					// The wire protocol defines exactly text, thinking and
					// tool. Anything else is not the receptionist's words to
					// the user, so it never reaches the transcript or the
					// answer.
					logger.Debug("muse ignoring event of unknown type", "task_id", taskID, "seq", e.Seq, "type", e.Type)
				}
			}
			return true
		}

		ticker := time.NewTicker(time.Duration(museappPollInterval.Load()))
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = "muse task timed out"
					if opts.Timeout > 0 {
						finalError = fmt.Sprintf("muse task timed out after %s", opts.Timeout)
					}
				} else {
					finalStatus = "aborted"
					finalError = "muse execution cancelled"
				}
				stopRemote()
				finish()
				return
			case <-ticker.C:
			}

			// Events first so the transcript stays ordered ahead of the
			// terminal status that may arrive on the same round. Event
			// polling is best-effort: the transcript may go quiet, but the
			// status poll below is the source of truth for completion, so a
			// failing events endpoint degrades streaming without failing
			// the run. Only status-poll failures count toward the budget.
			applyEvents()

			var st museappTaskStatus
			if err := b.getJSON(runCtx, mc, museappTaskPath(taskID, ""), &st); err != nil {
				if museappPollFailed(&pollErrs, err, logger, taskID) {
					finalStatus = "failed"
					finalError = fmt.Sprintf("museapp backend: receptionist unreachable: %v", err)
					stopRemote()
					finish()
					return
				}
				continue
			}

			switch st.Status {
			case "completed", "failed", "cancelled":
				if st.Status == "completed" || st.ExecutionStopped {
					terminalObserved.Store(true)
				}
				// The events and status polls are two requests, so the
				// receptionist can emit its last events (the tail of the
				// answer) between them. Once the task is terminal nothing more
				// will be written, so one more fetch is complete — without it
				// a "completed" run silently ends short of its final text.
				if !applyEvents() {
					logger.Warn("muse final events fetch failed; the answer may be missing its tail", "task_id", taskID)
				}
				if st.Status == "completed" {
					// N1: A completed status with an error (e.g. "result too
					// large") is not a success. The Python side reports the
					// problem via the error field; don't mask it by falling
					// back to the transcript and calling it completed.
					if st.Error != "" {
						finalStatus = "failed"
						finalError = st.Error
					} else {
						finalStatus = "completed"
						answer = st.Result
					}
				} else if st.Status == "cancelled" {
					finalStatus = "cancelled"
					finalError = "muse task cancelled remotely"
				} else {
					finalStatus = "failed"
					finalError = st.Error
					if finalError == "" {
						finalError = "muse task failed without an error message"
					}
				}
				if st.Status != "completed" && !st.ExecutionStopped {
					stopRemote()
				}
				finish()
				return
			case "queued", "running":
				// A healthy round: the receptionist is reachable and
				// speaking the protocol. Reset the consecutive-failure
				// budget (unknown statuses and poll errors accumulate
				// against it; see below).
				pollErrs = 0
			default:
				// An unknown status is not a completed run and not a
				// transient we can interpret — count it toward the poll
				// budget so a receptionist speaking a newer protocol fails
				// loudly instead of polling forever.
				if museappPollFailed(&pollErrs, fmt.Errorf("unknown status %q", st.Status), logger, taskID) {
					finalStatus = "failed"
					finalError = fmt.Sprintf("museapp backend: receptionist reported unknown status %q", st.Status)
					stopRemote()
					finish()
					return
				}
			}
		}
	}()

	return &Session{Messages: msgCh, Result: resCh, CleanupRequired: true, TerminalObserved: terminalObserved.Load}, nil
}

func museappUnconfirmedSession(err error) *Session {
	messages := make(chan Message)
	results := make(chan Result, 1)
	results <- Result{Status: "cleanup_unconfirmed", Error: err.Error() + "; remote execution may still be active; check the Muse worker before manually rerunning"}
	close(messages)
	close(results)
	return &Session{Messages: messages, Result: results, CleanupRequired: true, TerminalObserved: func() bool { return false }}
}

// museappTaskPath builds a /v1/tasks/{id}[/suffix] path. The task id comes from
// the receptionist's response, so it is escaped rather than trusted: an id such
// as "../admin" must stay one path segment of a task URL, not become a request
// to a different endpoint carrying the bearer token.
func museappTaskPath(taskID, suffix string) string {
	return "/v1/tasks/" + url.PathEscape(taskID) + suffix
}

// museappPollFailed records one failed poll round and reports whether the
// consecutive-failure budget is exhausted.
func museappPollFailed(pollErrs *int, err error, logger *slog.Logger, taskID string) bool {
	*pollErrs++
	logger.Debug("muse poll failed", "task_id", taskID, "consecutive", *pollErrs, "error", err)
	return *pollErrs >= int(museappMaxConsecutivePollErrors.Load())
}

func (b *museappBackend) postExecute(ctx context.Context, mc museappEndpointConfig, req museappExecuteRequest, out *museappExecuteResponse) error {
	if err := b.doJSONWithConfig(ctx, mc, http.MethodPost, "/v1/execute", req, out); err != nil {
		return err
	}
	// Go-B3: The receptionist reports its protocol version in the execute
	// response. If it speaks a different version, fail fast.
	// Execute must confirm cleanup of an accepted task before returning an error.
	if out.ProtocolVersion != 0 && out.ProtocolVersion != museappProtocolVersion {
		return fmt.Errorf("%w: receptionist speaks v%d, backend implements v%d",
			ErrMuseAppProtocolSkew, out.ProtocolVersion, museappProtocolVersion)
	}
	return nil
}

func (b *museappBackend) getJSON(ctx context.Context, mc museappEndpointConfig, path string, out any) error {
	return b.doJSONWithConfig(ctx, mc, http.MethodGet, path, nil, out)
}

func (b *museappBackend) cancelRemoteTask(mc museappEndpointConfig, taskID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var response museappCancelResponse
	if err := b.doJSONWithConfig(ctx, mc, http.MethodPost, museappTaskPath(taskID, "/cancel"), nil, &response); err != nil {
		return fmt.Errorf("Muse worker stop unconfirmed: %w", err)
	}
	if !response.ExecutionStopped {
		return fmt.Errorf("Muse worker stop unconfirmed: receptionist must confirm execution_stopped=true after the worker and its tools stop; check the worker before manually rerunning")
	}
	return nil
}

func (b *museappBackend) doJSONWithConfig(ctx context.Context, mc museappEndpointConfig, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("museapp backend: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, mc.endpoint+path, reader)
	if err != nil {
		return fmt.Errorf("museapp backend: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	b.authHeader(req, mc.token)
	resp, err := museappHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("museapp backend: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("museapp backend: read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &museappHTTPStatusError{code: resp.StatusCode, message: fmt.Sprintf("museapp backend: %s %s: HTTP %d: receptionist rejected credentials (check MULTICA_MUSEAPP_TOKEN)", method, path, resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &museappHTTPStatusError{code: resp.StatusCode, message: fmt.Sprintf("museapp backend: %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("museapp backend: decode response: %w", err)
	}
	return nil
}
