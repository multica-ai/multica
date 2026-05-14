package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v2"

	"github.com/multica-ai/multica/server/internal/cli"
)

const a2aConfigFilename = "a2a-agents.yaml"
const a2aCardPath = "/.well-known/agent-card.json"
const a2aRequestTimeout = 5 * time.Minute

// ---------------------------------------------------------------------------
// YAML config types
// ---------------------------------------------------------------------------

// a2aConfigFile represents the top-level structure of a2a-agents.yaml.
type a2aConfigFile struct {
	Agents []a2aConfigEntry `yaml:"agents"`
}

// a2aConfigEntry represents a single agent entry in the config file.
type a2aConfigEntry struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	TokenEnv string `yaml:"token_env,omitempty"`
}

// ---------------------------------------------------------------------------
// A2A Agent Card (minimal subset we need)
// ---------------------------------------------------------------------------

// A2AAgentCard holds the subset of the A2A Agent Card we use.
type A2AAgentCard struct {
	Name               string   `json:"name"`
	Version            string   `json:"version"`
	Description        string   `json:"description"`
	DefaultInputModes  []string `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string `json:"defaultOutputModes,omitempty"`
}

// ---------------------------------------------------------------------------
// JSON-RPC types for SendMessage
// ---------------------------------------------------------------------------

type a2aJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type a2aSendParams struct {
	Message a2aMessage `json:"message"`
}

type a2aMessage struct {
	Role  string    `json:"role"`
	Parts []a2aPart `json:"parts"`
}

type a2aPart struct {
	Text string `json:"text,omitempty"`
}

// The response result is a discriminated union: either "task" or "message".
type a2aSendResult struct {
	Task    *a2aTaskResponse `json:"task,omitempty"`
	Message *a2aMessage      `json:"message,omitempty"`
}

type a2aTaskResponse struct {
	ID     string    `json:"id"`
	Status a2aStatus `json:"status"`
}

type a2aStatus struct {
	State   string      `json:"state"`
	Message *a2aMessage `json:"message,omitempty"`
}

type a2aJSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *a2aJSONRPCError `json:"error,omitempty"`
}

type a2aJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Config loading
// ---------------------------------------------------------------------------

// loadA2AConfig reads and parses the A2A config file. Returns nil if the file
// does not exist — A2A agents are opt-in.
func loadA2AConfig(profile string) (*a2aConfigFile, error) {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		return nil, nil
	}
	path := filepath.Join(dir, a2aConfigFilename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read A2A config %s: %w", path, err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var cfg a2aConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse A2A config %s: %w", path, err)
	}
	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Agent Card fetching
// ---------------------------------------------------------------------------

// fetchAgentCard retrieves and parses the A2A Agent Card from the given base URL.
func fetchAgentCard(ctx context.Context, baseURL string) (*A2AAgentCard, error) {
	cardURL := strings.TrimRight(baseURL, "/") + a2aCardPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build card request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card from %s: %w", cardURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card returned %d from %s", resp.StatusCode, cardURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("read agent card body: %w", err)
	}

	var card A2AAgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, fmt.Errorf("parse agent card JSON: %w", err)
	}
	if card.Name == "" {
		return nil, fmt.Errorf("agent card missing required field 'name'")
	}
	return &card, nil
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

// discoverA2AAgents loads the A2A config, fetches Agent Cards for each entry,
// and returns a map of provider-keyed AgentEntry values. Failures to fetch
// individual cards are logged but do not prevent other agents from loading.
func discoverA2AAgents(ctx context.Context, profile string, logger *slog.Logger) map[string]AgentEntry {
	cfg, err := loadA2AConfig(profile)
	if err != nil {
		logger.Warn("a2a: config load failed", "error", err)
		return nil
	}
	if cfg == nil || len(cfg.Agents) == 0 {
		return nil
	}

	agents := make(map[string]AgentEntry, len(cfg.Agents))
	for _, entry := range cfg.Agents {
		if entry.Name == "" || entry.URL == "" {
			logger.Warn("a2a: skipping entry with empty name or url")
			continue
		}

		// Resolve auth token.
		var token string
		if entry.TokenEnv != "" {
			token = strings.TrimSpace(os.Getenv(entry.TokenEnv))
		}

		// Fetch Agent Card.
		card, cardErr := fetchAgentCard(ctx, entry.URL)
		if cardErr != nil {
			logger.Warn("a2a: failed to fetch agent card, skipping agent",
				"name", entry.Name,
				"url", entry.URL,
				"error", cardErr,
			)
			continue
		}

		logger.Info("a2a: discovered agent",
			"name", card.Name,
			"version", card.Version,
			"url", entry.URL,
		)

		agents[entry.Name] = AgentEntry{
			Mode:   "a2a",
			A2AURL: strings.TrimRight(entry.URL, "/"),
			Card:   card,
			Token:  token,
		}
	}
	return agents
}

// ---------------------------------------------------------------------------
// Task dispatch
// ---------------------------------------------------------------------------

// dispatchA2ATask sends the task prompt to the A2A agent via JSON-RPC
// SendMessage and maps the response to a TaskResult.
func (d *Daemon) dispatchA2ATask(ctx context.Context, task Task, entry AgentEntry, taskLog *slog.Logger) (TaskResult, error) {
	if task.WorkspaceID == "" {
		return TaskResult{}, fmt.Errorf("refusing to dispatch a2a task: task has no workspace_id (task_id=%s)", task.ID)
	}

	prompt := BuildPrompt(task, entry.Card.Name)
	taskLog.Info("dispatching a2a task", "url", entry.A2AURL, "agent", entry.Card.Name)

	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Connecting to %s", entry.Card.Name), 1, 2)

	// Build JSON-RPC SendMessage request.
	reqBody := a2aJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      task.ID,
		Method:  "SendMessage",
		Params: a2aSendParams{
			Message: a2aMessage{
				Role: "ROLE_USER",
				Parts: []a2aPart{
					{Text: prompt},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return TaskResult{}, fmt.Errorf("marshal a2a request: %w", err)
	}

	endpoint := entry.A2AURL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TaskResult{}, fmt.Errorf("build a2a request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if entry.Token != "" {
		req.Header.Set("Authorization", "Bearer "+entry.Token)
	}

	client := &http.Client{Timeout: a2aRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("a2a request failed: %s", err)}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB max
	if err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("read a2a response: %s", err)}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent returned HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	// Parse JSON-RPC response.
	var rpcResp a2aJSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("parse a2a response: %s", err)}, nil
	}
	if rpcResp.Error != nil {
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a JSON-RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message),
		}, nil
	}

	_ = d.client.ReportProgress(ctx, task.ID, "A2A agent responded", 2, 2)

	return mapA2AResult(rpcResp.Result, taskLog)
}

// mapA2AResult converts the A2A SendMessage response into a TaskResult.
func mapA2AResult(raw json.RawMessage, taskLog *slog.Logger) (TaskResult, error) {
	if len(raw) == 0 {
		return TaskResult{Status: "blocked", Comment: "a2a agent returned empty result"}, nil
	}

	var result a2aSendResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("parse a2a result: %s", err)}, nil
	}

	// If the agent returned a direct message (pre-task clarification), treat as blocked.
	if result.Message != nil && result.Task == nil {
		text := extractTextFromMessage(result.Message)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent responded with message (no task): %s", text),
		}, nil
	}

	if result.Task == nil {
		return TaskResult{Status: "blocked", Comment: "a2a response contains neither task nor message"}, nil
	}

	state := result.Task.Status.State
	taskLog.Info("a2a task state", "state", state)

	switch state {
	case "TASK_STATE_COMPLETED":
		output := extractStatusMessageText(&result.Task.Status)
		if output == "" {
			output = "a2a agent completed task"
		}
		return TaskResult{Status: "completed", Comment: output}, nil
	case "TASK_STATE_FAILED":
		msg := extractStatusMessageText(&result.Task.Status)
		if msg == "" {
			msg = "a2a agent reported task failure"
		}
		return TaskResult{Status: "blocked", Comment: msg}, nil
	case "TASK_STATE_REJECTED":
		msg := extractStatusMessageText(&result.Task.Status)
		if msg == "" {
			msg = "a2a agent rejected task"
		}
		return TaskResult{Status: "blocked", Comment: msg}, nil
	case "TASK_STATE_CANCELED":
		return TaskResult{Status: "cancelled", Comment: "a2a agent canceled task"}, nil
	case "TASK_STATE_INPUT_REQUIRED":
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent requires input: %s", msg),
		}, nil
	case "TASK_STATE_AUTH_REQUIRED":
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent requires authentication: %s", msg),
		}, nil
	case "TASK_STATE_WORKING":
		// Working is a non-terminal state; in blocking mode we shouldn't
		// normally see this, but handle it gracefully.
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent still working (non-terminal): %s", msg),
		}, nil
	default:
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent returned unknown state %s: %s", state, msg),
		}, nil
	}
}

// extractTextFromMessage joins all text parts from an A2A message.
func extractTextFromMessage(msg *a2aMessage) string {
	if msg == nil {
		return ""
	}
	var parts []string
	for _, p := range msg.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// extractStatusMessageText extracts text from the status message if present.
func extractStatusMessageText(status *a2aStatus) string {
	if status == nil || status.Message == nil {
		return ""
	}
	return extractTextFromMessage(status.Message)
}

