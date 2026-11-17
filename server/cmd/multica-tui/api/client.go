package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ── Types ────────────────────────────────────────────────────

type Agent struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Model         string `json:"model"`
	RuntimeID     string `json:"runtime_id"`
	RuntimeMode   string `json:"runtime_mode"`
	Visibility    string `json:"visibility"`
	MaxConcurrent int    `json:"max_concurrent_tasks"`
	Description   string `json:"description"`
}

type Issue struct {
	ID           string `json:"id"`
	Identifier   string `json:"identifier"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	AssigneeID   string `json:"assignee_id"`
	AssigneeType string `json:"assignee_type"`
	Number       int    `json:"number"`
	ProjectID    string `json:"project_id"`
}

type Task struct {
	ID         string  `json:"id"`
	AgentID    string  `json:"agent_id"`
	IssueID    string  `json:"issue_id"`
	Status     string  `json:"status"`
	Kind       string  `json:"kind"`
	Attempt    int     `json:"attempt"`
	StartedAt  *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
	Error      *string `json:"error"`
}

type Runtime struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type DashboardAgentRunTime struct {
	AgentID      string  `json:"agent_id"`
	AgentName    string  `json:"agent_name"`
	TotalSeconds float64 `json:"total_seconds"`
}

// ── Client ───────────────────────────────────────────────────

type Client struct {
	BaseURL     string
	Token       string
	WorkspaceID string
	HTTP        *http.Client
}

func New(baseURL, token, workspaceID string) *Client {
	return &Client{
		BaseURL:     baseURL,
		Token:       token,
		WorkspaceID: workspaceID,
		HTTP: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(method, path string) ([]byte, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.BaseURL, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	q := req.URL.Query()
	if c.WorkspaceID != "" {
		q.Set("workspace_id", c.WorkspaceID)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}
	return body, nil
}

// ── Endpoints ────────────────────────────────────────────────

func (c *Client) ListAgents() ([]Agent, error) {
	raw, err := c.do("GET", "/api/agents")
	if err != nil {
		return nil, err
	}
	var agents []Agent
	if err := json.Unmarshal(raw, &agents); err != nil {
		return nil, fmt.Errorf("parse agents: %w", err)
	}
	return agents, nil
}

func (c *Client) ListIssues() ([]Issue, error) {
	raw, err := c.do("GET", "/api/issues")
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Issues []Issue `json:"issues"`
		HasMore bool   `json:"has_more"`
	}
	// Try wrapped first; fallback to bare array
	if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Issues == nil {
		var issues []Issue
		if err := json.Unmarshal(raw, &issues); err != nil {
			return nil, fmt.Errorf("parse issues: %w", err)
		}
		return issues, nil
	}
	return wrapper.Issues, nil
}

func (c *Client) GetAgentTasks(agentID string) ([]Task, error) {
	raw, err := c.do("GET", fmt.Sprintf("/api/agents/%s/tasks", agentID))
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		return nil, fmt.Errorf("parse tasks: %w", err)
	}
	return tasks, nil
}

func (c *Client) ListAgentRuntimes() ([]Runtime, error) {
	raw, err := c.do("GET", "/api/runtimes")
	if err != nil {
		return nil, err
	}
	var runtimes []Runtime
	if err := json.Unmarshal(raw, &runtimes); err != nil {
		return nil, fmt.Errorf("parse runtimes: %w", err)
	}
	return runtimes, nil
}

func (c *Client) GetDashboardAgentRunTime() ([]DashboardAgentRunTime, error) {
	raw, err := c.do("GET", "/api/dashboard/agent-runtime")
	if err != nil {
		return nil, err
	}
	var data []DashboardAgentRunTime
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse dashboard: %w", err)
	}
	return data, nil
}
