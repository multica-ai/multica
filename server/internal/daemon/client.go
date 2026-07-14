package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// requestError is returned by postJSON/getJSON when the server responds with an error status.
type requestError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *requestError) Error() string {
	return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// isWorkspaceNotFoundError returns true if the error is a 404 with "workspace not found" body.
func isWorkspaceNotFoundError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(reqErr.Body), "workspace not found")
}

// isTaskNotFoundError returns true if the error is a 404 with "task not found"
// body. The daemon uses this to detect that a task was deleted server-side
// (issue removed, agent reassigned, ...) while the local agent was still
// running, so it can interrupt the agent rather than letting it keep
// emitting tool calls against a dead task.
func isTaskNotFoundError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(reqErr.Body), "task not found")
}

// isUnauthorizedError returns true if the error is a 401 from the server.
// Used by the token-renewal loop to surface a clear "re-login required"
// message instead of a generic transport-level retry.
func isUnauthorizedError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	return reqErr.StatusCode == http.StatusUnauthorized
}

// isRuntimeNotFoundError returns true if the error is a 404 with "runtime not
// found" body. The daemon uses this to detect that the runtime row was deleted
// server-side (UI Delete, 7-day offline GC) while the daemon was still
// heartbeating against the dead UUID, so it can prune the stale runtime from
// its local state and re-register instead of looping on the dead ID forever.
//
// Server-side, this body is paired with pgx.ErrNoRows specifically (other DB
// errors return 500), so a transient DB hiccup cannot make the daemon
// self-cleanup.
func isRuntimeNotFoundError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(reqErr.Body), "runtime not found")
}

// Client handles HTTP communication with the Multica server daemon API.
type Client struct {
	baseURL string
	token   string
	client  *http.Client

	// bundleClient downloads skill bundles. Unlike client it carries no fixed
	// Timeout: bundles can be large and slow on jittery links, so the caller
	// supplies a per-request, size-scaled deadline via context instead of
	// being capped by the 30s control-plane timeout that fits heartbeat /
	// claim but not a multi-megabyte body read. (GitHub #4505)
	bundleClient *http.Client

	// Identity headers sent on every request as X-Client-*. Populated by
	// SetIdentity(); empty values are simply omitted.
	platform string
	version  string
	os       string

	workspaceMu                    sync.Mutex
	workspaceETag                  string
	workspaceCache                 []WorkspaceInfo
	workspaceCacheValid            bool
	legacyWorkspaceEndpointEnabled bool
}

// NewClient creates a new daemon API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:      baseURL,
		client:       &http.Client{Timeout: 30 * time.Second, Transport: cloneDefaultTransport()},
		bundleClient: &http.Client{},
		platform:     "daemon",
		os:           normalizeGOOS(runtime.GOOS),
	}
}

func cloneDefaultTransport() http.RoundTripper {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return http.DefaultTransport
}

// CloseIdleConnections drops pooled control-plane HTTP connections. The
// daemon calls this after repeated heartbeat transport failures so a stale
// keep-alive socket from a server restart cannot delay recovery indefinitely.
func (c *Client) CloseIdleConnections() {
	if c == nil || c.client == nil {
		return
	}
	c.client.CloseIdleConnections()
}

// normalizeGOOS maps Go's runtime.GOOS values to the protocol vocabulary
// used by X-Client-OS / client_os ("macos" / "windows" / "linux").
func normalizeGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

// SetVersion records the daemon's CLI version, sent as X-Client-Version.
// Called by Daemon.Run after config is loaded.
func (c *Client) SetVersion(v string) {
	c.version = v
}

// setIdentityHeaders attaches X-Client-Platform/Version/OS to req when set.
func (c *Client) setIdentityHeaders(req *http.Request) {
	if c.platform != "" {
		req.Header.Set("X-Client-Platform", c.platform)
	}
	if c.version != "" {
		req.Header.Set("X-Client-Version", c.version)
	}
	if c.os != "" {
		req.Header.Set("X-Client-OS", c.os)
	}
	req.Header.Set("X-Client-Capabilities", daemonClientCapabilities())
}

// daemonClientCapabilities is the X-Client-Capabilities value the daemon
// advertises on BOTH the HTTP control-plane requests and the WS handshake, so a
// claim built over WS gets the same capability gating (skill refs,
// coalesced-comments) as the HTTP path. rpc-v1 advertises WS request/response
// support (MUL-4257).
func daemonClientCapabilities() string {
	return strings.Join([]string{
		protocol.DaemonCapabilitySkillBundlesV1,
		protocol.DaemonCapabilityCoalescedCommentsV1,
		protocol.DaemonCapabilityRPCV1,
	}, ",")
}

// SetToken sets the auth token for authenticated requests.
func (c *Client) SetToken(token string) {
	c.token = token
}

// Token returns the current auth token.
func (c *Client) Token() string {
	return c.token
}

func (c *Client) ClaimTask(ctx context.Context, runtimeID string) (*Task, error) {
	var resp struct {
		Task *Task `json:"task"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/tasks/claim", runtimeID), map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

// batchClaimRequestTimeout is the short, request-scoped deadline for the
// machine-level batch claim (MUL-4257). Unlike the per-runtime claim — which
// gets the full 30s control-plane timeout because a stall there only blocks
// that one runtime's goroutine — the batch call covers every runtime the
// daemon hosts in a single request, so a slow claim would delay ALL of them
// (the head-of-line coupling the per-runtime pollers were split to avoid,
// MUL-1744). Bounding the batch to a few seconds caps that worst-case
// starvation; a claim that commits server-side after the client gives up is
// recovered by ReclaimStaleDispatchedTasksForRuntimes on the next poll. Kept
// comfortably above p99 claim latency so recovery stays the exception.
const batchClaimRequestTimeout = 5 * time.Second

// ClaimTasks is the machine-level (MUL-4257) batch counterpart of ClaimTask:
// it asks the server, in a single request, to claim up to maxTasks tasks across
// every runtime the daemon hosts. daemonID scopes the request to this machine —
// the server rejects any runtime_id whose runtime.daemon_id doesn't match, so a
// stale/crossed runtime set can't claim another machine's tasks. Each returned
// Task carries its own RuntimeID so the daemon routes it to the matching
// runtime locally. The request runs under a short, request-scoped deadline
// (batchClaimRequestTimeout) rather than the shared 30s control-plane timeout so
// one slow claim cannot stall the whole batch; the deadline propagates to the
// server and cancels the in-flight query there too.
func (c *Client) ClaimTasks(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	reqCtx, cancel := context.WithTimeout(ctx, batchClaimRequestTimeout)
	defer cancel()
	var resp struct {
		Tasks []*Task `json:"tasks"`
	}
	if err := c.postJSON(reqCtx, "/api/daemon/tasks/claim", map[string]any{
		"daemon_id":   daemonID,
		"runtime_ids": runtimeIDs,
		"max_tasks":   maxTasks,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// isBatchClaimUnsupported reports whether err is a 404 from the batch claim
// endpoint — i.e. the server predates the /api/daemon/tasks/claim route and the
// daemon must fall back to the legacy per-runtime claim (MUL-4257). The batch
// handler itself never returns 404, so a 404 here means the route is
// unregistered on an un-upgraded server.
func isBatchClaimUnsupported(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	return reqErr.StatusCode == http.StatusNotFound
}

// claimTasksLegacy is the pre-batch compatibility fallback (MUL-4257): claim per
// runtime via the legacy POST /api/daemon/runtimes/{id}/tasks/claim so a new
// daemon still works against a server that has no batch route. Returns up to
// maxTasks tasks. A per-runtime error is only propagated when nothing has been
// claimed yet; otherwise the partial result is returned and the next poll
// retries the rest.
func (c *Client) claimTasksLegacy(ctx context.Context, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	if maxTasks <= 0 {
		return nil, nil
	}
	out := make([]*Task, 0, maxTasks)
	for _, rid := range runtimeIDs {
		if len(out) >= maxTasks {
			break
		}
		task, err := c.ClaimTask(ctx, rid)
		if err != nil {
			if len(out) == 0 {
				return nil, err
			}
			return out, nil
		}
		if task != nil {
			out = append(out, task)
		}
	}
	return out, nil
}

// ResolveSkillBundle downloads a single skill bundle. It uses bundleClient (no
// fixed timeout) so the deadline is governed entirely by ctx, which the daemon
// scales to the bundle's size, and retries transient transport blips within
// whatever budget ctx leaves. Resolving one skill per request — rather than the
// agent's whole bundle in one atomic body read — lets each download fit its own
// deadline and be cached independently, so a slow link makes incremental
// progress instead of failing the entire set on every dispatch. (GitHub #4505)
func (c *Client) ResolveSkillBundle(ctx context.Context, runtimeID, taskID string, ref SkillRefData) (SkillData, error) {
	var resp struct {
		Bundles []SkillData `json:"bundles"`
	}
	path := fmt.Sprintf("/api/daemon/runtimes/%s/tasks/%s/skill-bundles/resolve", runtimeID, taskID)
	if err := c.postJSONViaWithRetry(ctx, c.bundleClient, path, map[string]any{
		"skills": []SkillRefData{ref},
	}, &resp, skillBundleResolveRetrySchedule); err != nil {
		return SkillData{}, err
	}
	if len(resp.Bundles) != 1 {
		return SkillData{}, fmt.Errorf("resolve skill bundle: expected 1 bundle, got %d", len(resp.Bundles))
	}
	return resp.Bundles[0], nil
}

func (c *Client) ExtendTaskPrepareLease(ctx context.Context, runtimeID, taskID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/tasks/%s/prepare-lease", runtimeID, taskID), map[string]any{}, nil)
}

func (c *Client) StartTask(ctx context.Context, taskID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/start", taskID), map[string]any{}, nil)
}

// MarkTaskWaitingLocalDirectory parks a freshly-dispatched task in the
// waiting_local_directory state on the server. The daemon calls this after
// it has claimed a task whose project carries a local_directory resource
// but the path mutex is held by another in-flight task. reason is a short
// human-readable hint (e.g. "<path>") surfaced by the UI alongside the
// status. Idempotent on the daemon's side — calling twice with the same
// reason is a no-op once the row is already waiting_local_directory (the
// underlying SQL filters on status='dispatched', so the second call is a
// 400 the daemon swallows and proceeds to wait).
func (c *Client) MarkTaskWaitingLocalDirectory(ctx context.Context, taskID, reason string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/wait-local-directory", taskID), map[string]any{
		"reason": reason,
	}, nil)
}

func (c *Client) ReportProgress(ctx context.Context, taskID, summary string, step, total int) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/progress", taskID), map[string]any{
		"summary": summary,
		"step":    step,
		"total":   total,
	}, nil)
}

// TaskMessageData represents a single agent execution message for batch reporting.
type TaskMessageData struct {
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`
	Tool    string         `json:"tool,omitempty"`
	Content string         `json:"content,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
}

func (c *Client) ReportTaskMessages(ctx context.Context, taskID string, messages []TaskMessageData) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/messages", taskID), map[string]any{
		"messages": messages,
	}, nil)
}

func (c *Client) CompleteTask(ctx context.Context, taskID, output, branchName, sessionID, workDir string) error {
	body := map[string]any{"output": output}
	if branchName != "" {
		body["branch_name"] = branchName
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	return c.postJSONWithRetry(ctx, fmt.Sprintf("/api/daemon/tasks/%s/complete", taskID), body, nil, defaultTerminalRetrySchedule)
}

func (c *Client) ReportTaskUsage(ctx context.Context, taskID string, usage []TaskUsageEntry) error {
	if len(usage) == 0 {
		return nil
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/usage", taskID), map[string]any{
		"usage": usage,
	}, nil)
}

func (c *Client) FailTask(ctx context.Context, taskID, errMsg, sessionID, workDir, failureReason string) error {
	body := map[string]any{"error": errMsg}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	if failureReason != "" {
		body["failure_reason"] = failureReason
	}
	return c.postJSONWithRetry(ctx, fmt.Sprintf("/api/daemon/tasks/%s/fail", taskID), body, nil, defaultTerminalRetrySchedule)
}

// PinTaskSession persists the agent's session_id and work_dir on the task
// row mid-flight so a daemon crash doesn't lose the resume pointer.
func (c *Client) PinTaskSession(ctx context.Context, taskID, sessionID, workDir string) error {
	if sessionID == "" && workDir == "" {
		return nil
	}
	body := map[string]any{}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/session", taskID), body, nil)
}

// RecoverOrphans tells the server to fail any dispatched/running tasks the
// previous daemon process for this runtime left behind. The server will
// auto-retry eligible tasks.
func (c *Client) RecoverOrphans(ctx context.Context, runtimeID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/recover-orphans", runtimeID), map[string]any{}, nil)
}

// GetTaskStatus returns the current status of a task. Used by the daemon to
// detect terminal/interruption signals (cancelled, failed, completed, or a
// 404 task-not-found) while a task is executing.
func (c *Client) GetTaskStatus(ctx context.Context, taskID string) (string, error) {
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/status", taskID), &resp); err != nil {
		return "", err
	}
	return resp.Status, nil
}

// HeartbeatResponse, PendingUpdate, etc. alias the wire types so HTTP and WS
// heartbeat paths share a single type and a single decoder shape. Aliases
// (rather than wrappers) keep call sites unchanged.
type (
	HeartbeatResponse       = protocol.DaemonHeartbeatAckPayload
	PendingUpdate           = protocol.DaemonHeartbeatPendingUpdate
	PendingModelList        = protocol.DaemonHeartbeatPendingModelList
	PendingLocalSkills      = protocol.DaemonHeartbeatPendingLocalSkills
	PendingLocalSkillImport = protocol.DaemonHeartbeatPendingLocalSkillImport
)

func (c *Client) SendHeartbeat(ctx context.Context, runtimeID string) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	if err := c.postJSON(ctx, "/api/daemon/heartbeat", map[string]any{
		"runtime_id":            runtimeID,
		"supports_batch_import": true,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ReportUpdateResult sends the CLI update result back to the server.
func (c *Client) ReportUpdateResult(ctx context.Context, runtimeID, updateID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/update/%s/result", runtimeID, updateID), result, nil)
}

// ReportModelListResult sends the model-discovery result back to the server.
func (c *Client) ReportModelListResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/models/%s/result", runtimeID, requestID), result, nil)
}

// ReportLocalSkillListResult sends the runtime-local-skill inventory back to the server.
func (c *Client) ReportLocalSkillListResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/local-skills/%s/result", runtimeID, requestID), result, nil)
}

// ReportLocalSkillImportResult sends a runtime-local-skill bundle back to the server.
func (c *Client) ReportLocalSkillImportResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/local-skills/import/%s/result", runtimeID, requestID), result, nil)
}

// WorkspaceInfo holds minimal workspace metadata returned by the API.
type WorkspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RenewTokenResponse mirrors handler.RenewPATResponse — kept loose (string +
// bool) because the daemon never parses the timestamp itself; it just logs it
// for operator visibility.
type RenewTokenResponse struct {
	ExpiresAt string `json:"expires_at"`
	Renewed   bool   `json:"renewed"`
}

// RenewToken asks the server to extend the daemon's current PAT in place when
// it's within the server-side renewal window. The server is authoritative on
// the threshold — the daemon doesn't know the token's expires_at locally —
// so this is safe to call on any cadence; the only thing extra calls cost is
// one round trip and one cheap SELECT.
func (c *Client) RenewToken(ctx context.Context) (*RenewTokenResponse, error) {
	var resp RenewTokenResponse
	if err := c.postJSON(ctx, "/api/tokens/current/renew", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListWorkspaces fetches the minimal workspace membership set used by the
// daemon. New servers expose a daemon-specific endpoint with ETag support;
// when an installed daemon talks to an older server, the first 404 switches
// this process to the legacy full-workspace endpoint for compatibility.
func (c *Client) ListWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	c.workspaceMu.Lock()
	defer c.workspaceMu.Unlock()

	if c.legacyWorkspaceEndpointEnabled {
		return c.listLegacyWorkspaces(ctx)
	}

	const path = "/api/daemon/workspaces"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	c.setIdentityHeaders(req)
	if c.workspaceETag != "" {
		req.Header.Set("If-None-Match", c.workspaceETag)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		c.legacyWorkspaceEndpointEnabled = true
		c.workspaceETag = ""
		c.workspaceCache = nil
		c.workspaceCacheValid = false
		return c.listLegacyWorkspaces(ctx)
	}
	if resp.StatusCode == http.StatusNotModified {
		if !c.workspaceCacheValid {
			return nil, fmt.Errorf("GET %s returned 304 without a cached workspace set", path)
		}
		return append([]WorkspaceInfo(nil), c.workspaceCache...), nil
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &requestError{Method: http.MethodGet, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}

	var workspaces []WorkspaceInfo
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		return nil, err
	}
	c.workspaceETag = resp.Header.Get("ETag")
	c.workspaceCache = append([]WorkspaceInfo(nil), workspaces...)
	c.workspaceCacheValid = true
	return append([]WorkspaceInfo(nil), workspaces...), nil
}

func (c *Client) listLegacyWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	var workspaces []WorkspaceInfo
	if err := c.getJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

func (c *Client) usesLegacyWorkspaceEndpoint() bool {
	c.workspaceMu.Lock()
	defer c.workspaceMu.Unlock()
	return c.legacyWorkspaceEndpointEnabled
}

// IssueGCStatus holds the minimal issue info returned by the GC check endpoint.
type IssueGCStatus struct {
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetIssueGCCheck returns the status and updated_at of an issue for GC decisions.
func (c *Client) GetIssueGCCheck(ctx context.Context, issueID string) (*IssueGCStatus, error) {
	var resp IssueGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ChatSessionGCStatus mirrors IssueGCStatus for chat sessions.
type ChatSessionGCStatus struct {
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetChatSessionGCCheck returns the status of a chat session for GC decisions.
// A 404 from this endpoint indicates the session row was hard-deleted (the
// user explicitly removed it), which the caller treats as an immediate-clean
// signal.
func (c *Client) GetChatSessionGCCheck(ctx context.Context, sessionID string) (*ChatSessionGCStatus, error) {
	var resp ChatSessionGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/chat-sessions/%s/gc-check", sessionID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AutopilotRunGCStatus carries the status of an autopilot run. CompletedAt
// is the run's terminal timestamp (zero for non-terminal runs). The GC loop
// reclaims a terminal run's never-reused workdir as soon as it sees the
// terminal status, so it no longer gates on CompletedAt; the field is kept for
// the API response contract and diagnostics.
type AutopilotRunGCStatus struct {
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completed_at"`
}

// GetAutopilotRunGCCheck returns the status of an autopilot run for GC decisions.
func (c *Client) GetAutopilotRunGCCheck(ctx context.Context, runID string) (*AutopilotRunGCStatus, error) {
	var resp AutopilotRunGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/autopilot-runs/%s/gc-check", runID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TaskGCStatus carries the agent_task_queue status for quick-create cleanup.
// Quick-create tasks have no separate parent record, so GC keys directly on
// the task itself.
type TaskGCStatus struct {
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completed_at"`
}

// GetTaskGCCheck returns the status of an agent task for GC decisions.
func (c *Client) GetTaskGCCheck(ctx context.Context, taskID string) (*TaskGCStatus, error) {
	var resp TaskGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/gc-check", taskID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Deregister(ctx context.Context, runtimeIDs []string) error {
	return c.postJSON(ctx, "/api/daemon/deregister", map[string]any{
		"runtime_ids": runtimeIDs,
	}, nil)
}

// RegisterResponse holds the server's response to a daemon registration.
type RegisterResponse struct {
	Runtimes     []Runtime       `json:"runtimes"`
	Repos        []RepoData      `json:"repos"`
	ReposVersion string          `json:"repos_version"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

func (c *Client) Register(ctx context.Context, req map[string]any) (*RegisterResponse, error) {
	var resp RegisterResponse
	if err := c.postJSON(ctx, "/api/daemon/register", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspaceReposResponse struct {
	WorkspaceID  string          `json:"workspace_id"`
	Repos        []RepoData      `json:"repos"`
	ReposVersion string          `json:"repos_version"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

func (c *Client) GetWorkspaceRepos(ctx context.Context, workspaceID string) (*WorkspaceReposResponse, error) {
	var resp WorkspaceReposResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/repos", workspaceID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RuntimeProfile mirrors the server's workspace custom runtime profile
// (MUL-3284). protocol_family is the provider used for task routing (it
// selects the agent backend), while command_name is the actual executable
// the daemon resolves on PATH and launches. fixed_args are launch arguments
// every agent on this runtime inherits.
type RuntimeProfile struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Visibility     string   `json:"visibility"`
	Enabled        bool     `json:"enabled"`
}

// RuntimeProfilesResponse is the body of
// GET /api/daemon/workspaces/{workspaceID}/runtime-profiles. The server only
// returns enabled profiles for the workspace.
type RuntimeProfilesResponse struct {
	WorkspaceID     string           `json:"workspace_id"`
	RuntimeProfiles []RuntimeProfile `json:"runtime_profiles"`
}

// GetRuntimeProfiles fetches the workspace's enabled custom runtime profiles.
// Mirrors GetWorkspaceRepos. Callers must treat this as best-effort: an older
// server with no profiles route returns 404, which the daemon swallows and
// continues with built-in runtimes only.
func (c *Client) GetRuntimeProfiles(ctx context.Context, workspaceID string) (*RuntimeProfilesResponse, error) {
	var resp RuntimeProfilesResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/runtime-profiles", workspaceID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// defaultTerminalRetrySchedule is the backoff used by postJSONWithRetry for
// terminal task callbacks (CompleteTask / FailTask). N entries → N+1 attempts
// in the worst case (one immediate + N retries). Five backoffs totalling
// 124s is wide enough to ride out the short upstream blips we've seen
// (MUL-2780) without leaving the task stuck if the outage outlives the
// window.
var defaultTerminalRetrySchedule = []time.Duration{
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	32 * time.Second,
	64 * time.Second,
}

// skillBundleResolveRetrySchedule rides out brief transport blips on a single
// bundle download. Kept short on purpose: the real budget is the size-scaled
// context deadline the daemon sets per skill, and a skill that still fails is
// retried on the next dispatch once its siblings are cached. N entries → N+1
// attempts. (GitHub #4505)
var skillBundleResolveRetrySchedule = []time.Duration{
	500 * time.Millisecond,
	2 * time.Second,
}

// retrySleep is the sleep used between retry attempts. Pulled into a package
// variable so tests can swap in an instant sleep without rewriting the
// caller's schedule.
var retrySleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isTransientError reports whether err looks like a hiccup that's likely to
// resolve on retry: connection / TLS / I/O errors at the transport layer
// (including client timeouts surfacing as context.DeadlineExceeded inside
// http.Client.Do), 5xx server responses, and 408/429 rate-limit-style 4xx
// codes. Other 4xx codes are treated as permanent — retrying a 400 (bad
// body) or 404 (task not found) only burns time.
//
// The caller is responsible for separately bailing on parent-context
// cancellation; this predicate cannot distinguish "the daemon is shutting
// down" from "the HTTP client timed out a single attempt" because both
// reach here as context errors wrapped by net/http.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		if reqErr.StatusCode >= 500 {
			return true
		}
		if reqErr.StatusCode == http.StatusRequestTimeout || reqErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return false
	}
	return true
}

// postJSONWithRetry posts a JSON body with bounded exponential backoff,
// intended for "must reach the server" terminal callbacks (CompleteTask /
// FailTask). It retries transient errors per isTransientError and stops
// immediately on permanent 4xx responses so we don't burn the schedule on
// requests the server has already rejected.
//
// schedule controls the sleeps between attempts. With N entries the helper
// performs N+1 attempts in the worst case (one initial + N retries). The
// returned error is the last response from the server, so callers can still
// inspect it with isTransientError to decide whether to fall back to a
// different terminal call (e.g. complete → fail on permanent error only).
//
// The server-side CompleteTask / FailTask treat "already terminal" as an
// idempotent success (see service/task.go), so a duplicate replay from a
// retry is safe even if the server's prior response was lost in transit.
func (c *Client) postJSONWithRetry(ctx context.Context, path string, reqBody any, respBody any, schedule []time.Duration) error {
	return c.postJSONViaWithRetry(ctx, c.client, path, reqBody, respBody, schedule)
}

// postJSONViaWithRetry is postJSONWithRetry over an explicit http.Client, so
// large-body endpoints can run on bundleClient (deadline from ctx) while the
// control-plane keeps its fixed 30s client.
func (c *Client) postJSONViaWithRetry(ctx context.Context, httpClient *http.Client, path string, reqBody any, respBody any, schedule []time.Duration) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}
		err := c.postJSONVia(ctx, httpClient, path, reqBody, respBody)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransientError(err) {
			return err
		}
		if attempt >= len(schedule) {
			return err
		}
		if sleepErr := retrySleep(ctx, schedule[attempt]); sleepErr != nil {
			return err
		}
	}
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any, respBody any) error {
	return c.postJSONVia(ctx, c.client, path, reqBody, respBody)
}

// postJSONVia is postJSON over an explicit http.Client. Callers pick the client
// to control the timeout regime: c.client (fixed 30s) for control-plane calls,
// c.bundleClient (deadline from ctx) for large skill-bundle downloads.
func (c *Client) postJSONVia(ctx context.Context, httpClient *http.Client, path string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	c.setIdentityHeaders(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &requestError{Method: http.MethodPost, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if respBody == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) getJSON(ctx context.Context, path string, respBody any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	c.setIdentityHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &requestError{Method: http.MethodGet, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if respBody == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}
