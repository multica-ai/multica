package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// museBackend implements Backend by delegating to a remote Muse receptionist
// over HTTP. Unlike every other backend there is no local CLI to spawn: the
// daemon advertises a muse runtime exactly when MUSE_ENDPOINT is configured
// (see probeAgentCLIs), and Execute POSTs the prompt to the receptionist,
// streams its progress events into Messages, and resolves Result when the
// receptionist reports a terminal state.
//
// Wire protocol (v1):
//
//	POST /v1/execute            {prompt, session_id?, timeout_s?, workdir?} -> {task_id}
//	GET  /v1/tasks/{id}        -> {status, result?, error?}
//	GET  /v1/tasks/{id}/events -> {events: [{seq, type, content}]}
//	POST /v1/tasks/{id}/cancel -> {ok}
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
//	MUSE_ENDPOINT  base URL of the receptionist, e.g. http://127.0.0.1:8765
//	MUSE_TOKEN     bearer token for the receptionist (optional; sent only when set)
//	MUSE_MODEL     informational model label reported in the probe entry
//
// cfg.Env is deliberately NOT consulted. It carries the agent's custom_env,
// which any member who can edit the agent controls; honouring MUSE_ENDPOINT
// there would let them point this daemon at an arbitrary URL, and MUSE_TOKEN
// (which they cannot see) would still ride along from the process environment.
// The probe only ever reads the process environment, so reading anything else
// here would also mean probing one receptionist and running tasks on another.
type museBackend struct {
	cfg Config
}

// museProtocolVersion is the receptionist wire protocol this backend speaks.
// ProbeMuseReceptionist refuses a receptionist reporting anything else so a
// protocol skew fails at probe time, not mid-task.
const museProtocolVersion = 1

// ErrMuseProtocolSkew is returned by ProbeMuseReceptionist when the
// receptionist speaks a different protocol version than this backend.
// It is a sentinel so the daemon can distinguish deterministic protocol
// incompatibility (demotable) from transient network failures.
var ErrMuseProtocolSkew = fmt.Errorf("muse backend: protocol skew")

const (
	museEnvEndpoint = "MUSE_ENDPOINT"
	museEnvToken    = "MUSE_TOKEN"
	museEnvModel    = "MUSE_MODEL"
)

// musePollInterval is the steady-state cadence for task status/event polls.
// The receptionist is expected to be local or near-local (loopback or
// tailnet); 500ms keeps the transcript live without hammering it.
// Atomic so tests can shrink it without racing concurrent poll loops.
var musePollInterval atomic.Int64 // nanoseconds

// museMaxConsecutivePollErrors bounds transient network failures before the
// run is failed. Ten misses at the poll interval is ~5s of silence — long
// enough to ride out a blip, short enough not to wedge a task on a dead
// receptionist. Atomic for the same test reason as musePollInterval.
var museMaxConsecutivePollErrors atomic.Int32

func init() {
	musePollInterval.Store(int64(500 * time.Millisecond))
	museMaxConsecutivePollErrors.Store(10)
}

type museEndpointConfig struct {
	endpoint string
	token    string
}

func resolveMuseConfig() (museEndpointConfig, error) {
	endpoint := strings.TrimSpace(os.Getenv(museEnvEndpoint))
	if strings.TrimSpace(endpoint) == "" {
		return museEndpointConfig{}, fmt.Errorf("muse backend: %s is not set; configure the receptionist URL to enable the muse runtime", museEnvEndpoint)
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return museEndpointConfig{}, fmt.Errorf("muse backend: invalid %s %q: %w", museEnvEndpoint, endpoint, err)
	}
	return museEndpointConfig{
		endpoint: endpoint,
		token:    strings.TrimSpace(os.Getenv(museEnvToken)),
	}, nil
}

func (b *museBackend) authHeader(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// museHTTPClient is a package var so tests can observe transport behavior;
// the default has a bounded timeout so a wedged receptionist cannot hang
// Execute forever outside ctx cancellation.
var museHTTPClient = &http.Client{Timeout: 30 * time.Second}

type museExecuteRequest struct {
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

type museExecuteResponse struct {
	TaskID          string `json:"task_id"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

type museTaskStatus struct {
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type museEvent struct {
	Seq     int64  `json:"seq"`
	Type    string `json:"type"` // "text" | "thinking" | "tool"
	Content string `json:"content"`
	Tool    string `json:"tool,omitempty"`
}

type museEventsResponse struct {
	Events []museEvent `json:"events"`
}

type museHealthResponse struct {
	ProtocolVersion int    `json:"protocol_version"`
	Version         string `json:"version"`
}

// ProbeMuseReceptionist checks that a receptionist is reachable at endpoint
// and speaks the wire protocol this backend implements. It is the muse
// equivalent of CLI version detection and backs the daemon's availability
// probe (no binary exists to --version). A protocol skew is a hard error so
// it surfaces at probe time rather than mid-task.
func ProbeMuseReceptionist(ctx context.Context, endpoint, token string) (string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return "", fmt.Errorf("muse backend: empty endpoint")
	}
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return "", fmt.Errorf("muse backend: invalid endpoint %q: %w", endpoint, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/health", nil)
	if err != nil {
		return "", fmt.Errorf("muse backend: build health request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := museHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("muse backend: receptionist unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("muse backend: read health response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("muse backend: health check HTTP %d: receptionist rejected credentials (check MUSE_TOKEN)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("muse backend: health check HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var health museHealthResponse
	if err := json.Unmarshal(raw, &health); err != nil {
		return "", fmt.Errorf("muse backend: decode health response: %w", err)
	}
	if health.ProtocolVersion != museProtocolVersion {
		return "", fmt.Errorf("%w: receptionist speaks v%d, backend implements v%d",
			ErrMuseProtocolSkew, health.ProtocolVersion, museProtocolVersion)
	}
	if health.Version == "" {
		health.Version = "unknown"
	}
	return health.Version, nil
}

func (b *museBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("muse prompt must not be empty")
	}
	mc, err := resolveMuseConfig()
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
	var execResp museExecuteResponse
	if err := b.postExecute(runCtx, mc, museExecuteRequest{
		Prompt:          prompt,
		SessionID:       opts.ResumeSessionID,
		TimeoutS:        timeoutS,
		WorkDir:         opts.Cwd,
		TaskToken:       opts.TaskToken,
		ServerURL:       opts.MulticaServerURL,
		WorkspaceID:     opts.MulticaWorkspaceID,
		ProtocolVersion: museProtocolVersion,
	}, &execResp); err != nil {
		// R2: Release the runContext on early failure. The success path
		// hands ownership to the polling goroutine (which defers cancel).
		cancel()
		return nil, err
	}
	if execResp.TaskID == "" {
		cancel()
		return nil, fmt.Errorf("muse backend: receptionist returned an empty task id")
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

		// applyEvents pulls the events after lastSeq and folds them into the
		// transcript and the answer. It is best-effort and reports whether the
		// fetch succeeded.
		applyEvents := func() bool {
			var eventsResp museEventsResponse
			if err := b.getJSON(runCtx, mc, museTaskPath(taskID, "/events?since="+strconv.FormatInt(lastSeq, 10)), &eventsResp); err != nil {
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

		ticker := time.NewTicker(time.Duration(musePollInterval.Load()))
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				// Best-effort remote cancel; the local verdict is authoritative.
				cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = b.postCancel(cancelCtx, mc, taskID)
				cancel()
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

			var st museTaskStatus
			if err := b.getJSON(runCtx, mc, museTaskPath(taskID, ""), &st); err != nil {
				if musePollFailed(&pollErrs, err, logger, taskID) {
					// B2: Local polling failed but the remote task may still be
					// running. Best-effort cancel it so we don't leave an
					// orphaned worker. The local verdict stays "failed" —
					// a cancel failure doesn't change that, and we don't
					// claim the remote worker actually stopped.
					cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					_ = b.postCancel(cancelCtx, mc, taskID)
					cancel()
					finalStatus = "failed"
					finalError = fmt.Sprintf("muse backend: receptionist unreachable: %v", err)
					finish()
					return
				}
				continue
			}

			switch st.Status {
			case "completed", "failed", "cancelled":
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
				if musePollFailed(&pollErrs, fmt.Errorf("unknown status %q", st.Status), logger, taskID) {
					// B2: Same as the poll-failure branch — best-effort
					// cancel the remote task before returning the local
					// failure verdict.
					cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					_ = b.postCancel(cancelCtx, mc, taskID)
					cancel()
					finalStatus = "failed"
					finalError = fmt.Sprintf("muse backend: receptionist reported unknown status %q", st.Status)
					finish()
					return
				}
			}
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// museTaskPath builds a /v1/tasks/{id}[/suffix] path. The task id comes from
// the receptionist's response, so it is escaped rather than trusted: an id such
// as "../admin" must stay one path segment of a task URL, not become a request
// to a different endpoint carrying the bearer token.
func museTaskPath(taskID, suffix string) string {
	return "/v1/tasks/" + url.PathEscape(taskID) + suffix
}

// musePollFailed records one failed poll round and reports whether the
// consecutive-failure budget is exhausted.
func musePollFailed(pollErrs *int, err error, logger *slog.Logger, taskID string) bool {
	*pollErrs++
	logger.Debug("muse poll failed", "task_id", taskID, "consecutive", *pollErrs, "error", err)
	return *pollErrs >= int(museMaxConsecutivePollErrors.Load())
}

func (b *museBackend) postExecute(ctx context.Context, mc museEndpointConfig, req museExecuteRequest, out *museExecuteResponse) error {
	if err := b.doJSONWithConfig(ctx, mc, http.MethodPost, "/v1/execute", req, out); err != nil {
		return err
	}
	// Go-B3: The receptionist reports its protocol version in the execute
	// response. If it speaks a different version, fail fast.
	// The receptionist validates req.protocol_version BEFORE creating the
	// task, so a mismatch here means either an old receptionist (no
	// validation) or a race. If we got a task_id, try to cancel it —
	// don't leave an orphaned task that we'll never poll.
	if out.ProtocolVersion != 0 && out.ProtocolVersion != museProtocolVersion {
		if out.TaskID != "" {
			// Best-effort cleanup of the orphaned task. Use a separate
			// bounded context; don't let cancel failure mask the skew error.
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = b.postCancel(cancelCtx, mc, out.TaskID)
			cancel()
		}
		return fmt.Errorf("%w: receptionist speaks v%d, backend implements v%d",
			ErrMuseProtocolSkew, out.ProtocolVersion, museProtocolVersion)
	}
	return nil
}

func (b *museBackend) getJSON(ctx context.Context, mc museEndpointConfig, path string, out any) error {
	return b.doJSONWithConfig(ctx, mc, http.MethodGet, path, nil, out)
}

func (b *museBackend) postCancel(ctx context.Context, mc museEndpointConfig, taskID string) error {
	return b.doJSONWithConfig(ctx, mc, http.MethodPost, museTaskPath(taskID, "/cancel"), nil, nil)
}

func (b *museBackend) doJSONWithConfig(ctx context.Context, mc museEndpointConfig, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("muse backend: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, mc.endpoint+path, reader)
	if err != nil {
		return fmt.Errorf("muse backend: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	b.authHeader(req, mc.token)
	resp, err := museHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("muse backend: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("muse backend: read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("muse backend: %s %s: HTTP %d: receptionist rejected credentials (check MUSE_TOKEN)", method, path, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("muse backend: %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("muse backend: decode response: %w", err)
	}
	return nil
}
