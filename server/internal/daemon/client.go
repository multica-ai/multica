package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const workspaceHeader = "X-Workspace-ID"

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

// Client handles HTTP communication with the Multica server daemon API.
type Client struct {
	baseURL string
	token   string
	client  *http.Client
}

// WorkspaceSkillFile represents a skill file payload returned by the workspace skill API.
type WorkspaceSkillFile struct {
	ID      string `json:"id,omitempty"`
	SkillID string `json:"skill_id,omitempty"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WorkspaceSkill represents a workspace skill returned by the server API.
type WorkspaceSkill struct {
	ID          string               `json:"id"`
	WorkspaceID string               `json:"workspace_id,omitempty"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Content     string               `json:"content"`
	Config      any                  `json:"config"`
	Files       []WorkspaceSkillFile `json:"files,omitempty"`
}

// CreateWorkspaceSkillRequest is the request body for creating a workspace skill.
type CreateWorkspaceSkillRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Content     string               `json:"content"`
	Config      any                  `json:"config,omitempty"`
	Files       []WorkspaceSkillFile `json:"files,omitempty"`
}

// UpdateWorkspaceSkillRequest is the request body for updating a workspace skill.
type UpdateWorkspaceSkillRequest struct {
	Name        *string              `json:"name,omitempty"`
	Description *string              `json:"description,omitempty"`
	Content     *string              `json:"content,omitempty"`
	Config      any                  `json:"config,omitempty"`
	Files       []WorkspaceSkillFile `json:"files,omitempty"`
}

// NewClient creates a new daemon API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
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

func (c *Client) FailTask(ctx context.Context, taskID, errMsg string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/fail", taskID), map[string]any{
		"error": errMsg,
	}, nil)
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

func (c *Client) ReportUsage(ctx context.Context, runtimeID string, entries []map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/usage", runtimeID), map[string]any{
		"entries": entries,
	}, nil)
}

// HeartbeatResponse contains the server's response to a heartbeat, including any pending actions.
type HeartbeatResponse struct {
	Status        string         `json:"status"`
	PendingPing   *PendingPing   `json:"pending_ping,omitempty"`
	PendingUpdate *PendingUpdate `json:"pending_update,omitempty"`
}

// PendingPing represents a ping test request from the server.
type PendingPing struct {
	ID string `json:"id"`
}

// PendingUpdate represents a CLI update request from the server.
type PendingUpdate struct {
	ID            string `json:"id"`
	TargetVersion string `json:"target_version"`
}

func (c *Client) SendHeartbeat(ctx context.Context, runtimeID string) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	if err := c.postJSON(ctx, "/api/daemon/heartbeat", map[string]string{
		"runtime_id": runtimeID,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ReportPingResult(ctx context.Context, runtimeID, pingID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/ping/%s/result", runtimeID, pingID), result, nil)
}

// ReportUpdateResult sends the CLI update result back to the server.
func (c *Client) ReportUpdateResult(ctx context.Context, runtimeID, updateID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/update/%s/result", runtimeID, updateID), result, nil)
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

func (c *Client) Deregister(ctx context.Context, runtimeIDs []string) error {
	return c.postJSON(ctx, "/api/daemon/deregister", map[string]any{
		"runtime_ids": runtimeIDs,
	}, nil)
}

// RegisterResponse holds the server's response to a daemon registration.
type RegisterResponse struct {
	Runtimes []Runtime  `json:"runtimes"`
	Repos    []RepoData `json:"repos"`
}

func (c *Client) Register(ctx context.Context, req map[string]any) (*RegisterResponse, error) {
	var resp RegisterResponse
	if err := c.postJSON(ctx, "/api/daemon/register", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListWorkspaceSkills(ctx context.Context, workspaceID string) ([]WorkspaceSkill, error) {
	var skills []WorkspaceSkill
	if err := c.getJSONWithHeaders(ctx, "/api/skills", map[string]string{
		workspaceHeader: workspaceID,
	}, &skills); err != nil {
		return nil, err
	}
	return skills, nil
}

func (c *Client) CreateWorkspaceSkill(ctx context.Context, workspaceID string, req CreateWorkspaceSkillRequest) (*WorkspaceSkill, error) {
	var skill WorkspaceSkill
	if err := c.postJSONWithHeaders(ctx, "/api/skills", map[string]string{
		workspaceHeader: workspaceID,
	}, req, &skill); err != nil {
		return nil, err
	}
	return &skill, nil
}

func (c *Client) UpdateWorkspaceSkill(ctx context.Context, workspaceID, skillID string, req UpdateWorkspaceSkillRequest) (*WorkspaceSkill, error) {
	var skill WorkspaceSkill
	if err := c.putJSONWithHeaders(ctx, fmt.Sprintf("/api/skills/%s", skillID), map[string]string{
		workspaceHeader: workspaceID,
	}, req, &skill); err != nil {
		return nil, err
	}
	return &skill, nil
}

func (c *Client) DeleteWorkspaceSkill(ctx context.Context, workspaceID, skillID string) error {
	return c.deleteWithHeaders(ctx, fmt.Sprintf("/api/skills/%s", skillID), map[string]string{
		workspaceHeader: workspaceID,
	})
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any, respBody any) error {
	return c.postJSONWithHeaders(ctx, path, nil, reqBody, respBody)
}

func (c *Client) postJSONWithHeaders(ctx context.Context, path string, headers map[string]string, reqBody any, respBody any) error {
	return c.doJSON(ctx, http.MethodPost, path, headers, reqBody, respBody)
}

func (c *Client) putJSONWithHeaders(ctx context.Context, path string, headers map[string]string, reqBody any, respBody any) error {
	return c.doJSON(ctx, http.MethodPut, path, headers, reqBody, respBody)
}

func (c *Client) deleteWithHeaders(ctx context.Context, path string, headers map[string]string) error {
	return c.doJSON(ctx, http.MethodDelete, path, headers, nil, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, headers map[string]string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &requestError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if respBody == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) getJSON(ctx context.Context, path string, respBody any) error {
	return c.getJSONWithHeaders(ctx, path, nil, respBody)
}

func (c *Client) getJSONWithHeaders(ctx context.Context, path string, headers map[string]string, respBody any) error {
	return c.doJSON(ctx, http.MethodGet, path, headers, nil, respBody)
}
