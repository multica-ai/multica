package daemon

// CEREBRO-PATCH(daemon-client): cerebro modification of upstream file

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

	// Identity headers sent on every request as X-Client-*. Populated by
	// SetIdentity(); empty values are simply omitted.
	platform string
	version  string
	os       string
}

// NewClient creates a new daemon API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: 30 * time.Second},
		platform: "daemon",
		os:       normalizeGOOS(runtime.GOOS),
	}
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

func (c *Client) StartTask(ctx context.Context, taskID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/start", taskID), map[string]any{}, nil)
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
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/complete", taskID), body, nil)
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
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/fail", taskID), body, nil)
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
// detect if a task was cancelled while it was executing.
func (c *Client) GetTaskStatus(ctx context.Context, taskID string) (string, error) {
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/status", taskID), &resp); err != nil {
		return "", err
	}
	return resp.Status, nil
}

// HeartbeatResponse contains the server's response to a heartbeat, including any pending actions.
// CEREBRO-PATCH(daemon-heartbeat-pending-ping): cerebro keeps an explicit struct (rather
// than an alias to protocol.DaemonHeartbeatAckPayload) so it can carry the cerebro-only
// PendingPing field alongside upstream's protocol-aliased pending actions.
type HeartbeatResponse struct {
	RuntimeID   string `json:"runtime_id"`
	Status      string `json:"status"`
	RuntimeGone bool   `json:"runtime_gone,omitempty"`
	// CEREBRO-PATCH(heartbeat-account-id-ack): JEH-881 registered cerebro account_id.
	CerebroAccountID        string                   `json:"cerebro_account_id,omitempty"`
	PendingPing             *PendingPing             `json:"pending_ping,omitempty"`
	PendingUpdate           *PendingUpdate           `json:"pending_update,omitempty"`
	PendingModelList        *PendingModelList        `json:"pending_model_list,omitempty"`
	PendingLocalSkills      *PendingLocalSkills      `json:"pending_local_skills,omitempty"`
	PendingLocalSkillImport *PendingLocalSkillImport `json:"pending_local_skill_import,omitempty"`
}

// PendingPing represents a ping test request from the server.
type PendingPing struct {
	ID string `json:"id"`
	// SandboxEnabled mirrors the runtime's per-runtime sandbox override
	// (JEH-418) at the moment the server issued this ping request.
	// nil = inherit the daemon's env-var default. Without this field, an
	// admin who disabled sandbox via the UI would still see pings fail
	// because the ping path ran sandboxed against env-var-default=true.
	SandboxEnabled *bool `json:"sandbox_enabled,omitempty"`
}

// Upstream pending-action types are aliased to the wire protocol so HTTP and WS
// heartbeat paths share a single type and a single decoder shape.
type (
	PendingUpdate           = protocol.DaemonHeartbeatPendingUpdate
	PendingModelList        = protocol.DaemonHeartbeatPendingModelList
	PendingLocalSkills      = protocol.DaemonHeartbeatPendingLocalSkills
	PendingLocalSkillImport = protocol.DaemonHeartbeatPendingLocalSkillImport
)

// SendHeartbeatOpts bundles optional fields the daemon attaches to a
// CEREBRO-PATCH(client): persona integration additions.
// heartbeat. Each field is independently optional — the zero value of
// SendHeartbeatOpts gives the original `(runtime_id only)` wire shape.
type SendHeartbeatOpts struct {
	// Account piggybacks the runtime's detected login identity (JEH-997).
	// nil = daemon could not derive an identity yet; the server skips its
	// account-registration hook entirely.
	Account *protocol.DaemonHeartbeatAccount
	// CLIVersion is the daemon-detected agent CLI version (W4.2).
	// Empty = no advertised version; server keeps its stored value.
	CLIVersion string
	// Capabilities is an optional snapshot of the runtime's tool surface
	// (W4.2). Server refreshes the runtime's capabilities row when set.
	Capabilities map[string]any
}

// SendHeartbeat posts a runtime liveness ping. opts carries the optional
// account piggyback (JEH-997) and CLI-version + capabilities snapshot
// (W4.2). Zero-value opts keeps the wire shape minimal.
func (c *Client) SendHeartbeat(ctx context.Context, runtimeID string, opts SendHeartbeatOpts) (*HeartbeatResponse, error) {
	body := protocol.DaemonHeartbeatRequestPayload{
		RuntimeID:    runtimeID,
		Account:      opts.Account,
		CLIVersion:   opts.CLIVersion,
		Capabilities: opts.Capabilities,
	}
	var resp HeartbeatResponse
	if err := c.postJSON(ctx, "/api/daemon/heartbeat", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RefreshCapabilities is the operator-triggered manual path (W4.2).
// Daemons send a fresh capabilities snapshot to the runtime row
// without waiting for the next heartbeat.
func (c *Client) RefreshCapabilities(ctx context.Context, runtimeID, cliVersion string, capabilities map[string]any) error {
	body := map[string]any{"capabilities": capabilities}
	if cliVersion != "" {
		body["cli_version"] = cliVersion
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/refresh-capabilities", runtimeID), body, nil)
}

// CEREBRO-PATCH(daemon-client-report-ping-result): cerebro-only ping result
// reporter consumed by daemon.handlePing.
func (c *Client) ReportPingResult(ctx context.Context, runtimeID, pingID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/ping/%s/result", runtimeID, pingID), result, nil)
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

// RuntimeToolScanServer is the wire shape for one server's tools/list result
// in the daemon scan report. Empty Tools with non-empty Error signals an
// unreachable MCP server; the server records the error but keeps prior rows.
// CEREBRO-PATCH(daemon-client-runtime-tool-scan): JEH-1710 daemon→server scan
// wire types. Mirrors handler.RuntimeToolScanServer / RuntimeToolScanTool.
type RuntimeToolScanServer struct {
	Name  string                  `json:"name"`
	Tools []RuntimeToolScanTool   `json:"tools"`
	Error string                  `json:"error,omitempty"`
}

// RuntimeToolScanTool is one entry inside a server's tools/list response.
type RuntimeToolScanTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// GetRuntimeMcpConfig fetches the runtime's tools_config blob so the daemon
// can iterate the declared MCP servers without claiming a task first.
// Returns an empty {"mcpServers":{}} document when nothing is configured.
func (c *Client) GetRuntimeMcpConfig(ctx context.Context, runtimeID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/mcp-config", runtimeID), &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ReportRuntimeToolScan POSTs the per-server tools/list result back to the
// server, which upserts the (runtime, server, tool) registry rows.
func (c *Client) ReportRuntimeToolScan(ctx context.Context, runtimeID string, scannedAt time.Time, servers []RuntimeToolScanServer) error {
	body := map[string]any{
		"scanned_at": scannedAt.UTC().Format(time.RFC3339Nano),
		"servers":    servers,
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/tool-scan", runtimeID), body, nil)
}

// WorkspaceInfo holds minimal workspace metadata returned by the API.
type WorkspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListWorkspaces fetches all workspaces the authenticated user belongs to.
func (c *Client) ListWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	var workspaces []WorkspaceInfo
	if err := c.getJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
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
// is the run's terminal timestamp (zero for non-terminal runs); the GC loop
// uses it as the TTL anchor instead of UpdatedAt because autopilot_run rows
// have no updated_at column.
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

// PersonaAgent mirrors handler.PersonaAgentEntry — every agent in a
// workspace whose persona_sandbox is set, joined with its runtime row.
type PersonaAgent struct {
	ID                    string          `json:"id"`
	Name                  string          `json:"name"`
	PersonaSandbox        string          `json:"persona_sandbox"`
	RuntimeID             string          `json:"runtime_id"`
	RuntimeName           string          `json:"runtime_name"`
	Provider              string          `json:"provider"`
	RuntimePersonaSandbox string          `json:"runtime_persona_sandbox"`
	McpConfig             json.RawMessage `json:"mcp_config,omitempty"`
}

// ListWorkspacePersonaAgents fetches the agents-with-persona for a workspace.
// Daemon uses it at start to seed persona-actor attributes (E3 part 3).
func (c *Client) ListWorkspacePersonaAgents(ctx context.Context, workspaceID string) ([]PersonaAgent, error) {
	var resp struct {
		Agents []PersonaAgent `json:"agents"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/agents/persona", workspaceID), &resp); err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

type WorkspaceReposResponse struct {
	WorkspaceID  string     `json:"workspace_id"`
	Repos        []RepoData `json:"repos"`
	ReposVersion string     `json:"repos_version"`
}

func (c *Client) GetWorkspaceRepos(ctx context.Context, workspaceID string) (*WorkspaceReposResponse, error) {
	var resp WorkspaceReposResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/repos", workspaceID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any, respBody any) error {
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

	resp, err := c.client.Do(req)
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
