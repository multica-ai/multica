// Package hindsight provides long-term memory for Multica agents via the Hindsight API.
//
// When MULTICA_HINDSIGHT_URL is set, the daemon recalls relevant memories before
// each task and retains the task outcome after completion. Memory is silently
// disabled when the env var is absent — existing behaviour is unchanged.
package hindsight

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config holds the Hindsight connection settings read from environment variables.
//
//	MULTICA_HINDSIGHT_URL     – base URL of the Hindsight API (required to enable memory)
//	MULTICA_HINDSIGHT_API_KEY – Bearer token for Hindsight Cloud (optional for self-hosted)
//	MULTICA_HINDSIGHT_BANK_ID – memory bank to use (default: "multica")
type Config struct {
	APIURL string
	APIKey string
	BankID string
}

// ConfigFromEnv reads MULTICA_HINDSIGHT_* env vars and returns a Config.
// Returns nil when MULTICA_HINDSIGHT_URL is not set (memory is disabled).
func ConfigFromEnv() *Config {
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_HINDSIGHT_URL")), "/")
	if apiURL == "" {
		return nil
	}
	bankID := strings.TrimSpace(os.Getenv("MULTICA_HINDSIGHT_BANK_ID"))
	if bankID == "" {
		bankID = "multica"
	}
	return &Config{
		APIURL: apiURL,
		APIKey: strings.TrimSpace(os.Getenv("MULTICA_HINDSIGHT_API_KEY")),
		BankID: bankID,
	}
}

// recallRequest is the body sent to POST /v1/default/banks/{bank_id}/memories/recall.
type recallRequest struct {
	Query     string `json:"query"`
	Budget    string `json:"budget"`
	MaxTokens int    `json:"max_tokens"`
}

// recallResult is a single item in the recall response.
type recallResult struct {
	Text string `json:"text"`
}

// recallResponse is the JSON body returned by the recall endpoint.
type recallResponse struct {
	Results []recallResult `json:"results"`
}

// Recall retrieves memories relevant to query from the Hindsight bank.
// Returns a formatted numbered list string, or "" when there are no results
// or when cfg is nil. Errors are logged but never propagated — a recall
// failure must never block task execution.
func Recall(ctx context.Context, cfg *Config, query string, logger *slog.Logger) string {
	if cfg == nil || query == "" {
		return ""
	}

	reqBody, _ := json.Marshal(recallRequest{
		Query:     query,
		Budget:    "mid",
		MaxTokens: 4096,
	})

	url := fmt.Sprintf("%s/v1/default/banks/%s/memories/recall", cfg.APIURL, cfg.BankID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		logger.Debug("hindsight: recall request build failed", "error", err)
		return ""
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		logger.Debug("hindsight: recall request failed", "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debug("hindsight: recall returned non-200", "status", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debug("hindsight: recall read body failed", "error", err)
		return ""
	}

	var result recallResponse
	if err := json.Unmarshal(body, &result); err != nil {
		logger.Debug("hindsight: recall unmarshal failed", "error", err)
		return ""
	}

	if len(result.Results) == 0 {
		return ""
	}

	var b strings.Builder
	for i, r := range result.Results {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(r.Text))
	}
	return strings.TrimSpace(b.String())
}

// retainRequest is the body sent to POST /v1/default/banks/{bank_id}/memories.
type retainRequest struct {
	Items []retainItem `json:"items"`
	Async bool         `json:"async"`
}

// retainItem is a single memory item to store.
type retainItem struct {
	Content string `json:"content"`
}

// Retain stores content in the Hindsight bank asynchronously (fire-and-forget).
// It is designed to be called as a goroutine after task completion.
// Errors are logged but never propagated.
func Retain(ctx context.Context, cfg *Config, content string, logger *slog.Logger) {
	if cfg == nil || strings.TrimSpace(content) == "" {
		return
	}

	reqBody, _ := json.Marshal(retainRequest{
		Items: []retainItem{{Content: content}},
		Async: true,
	})

	url := fmt.Sprintf("%s/v1/default/banks/%s/memories", cfg.APIURL, cfg.BankID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		logger.Debug("hindsight: retain request build failed", "error", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		logger.Debug("hindsight: retain request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		logger.Debug("hindsight: retain returned non-2xx", "status", resp.StatusCode)
	}
}

// FormatMemoriesBlock wraps a numbered memory list in a <hindsight_memories> XML block.
// Returns "" when memories is empty.
func FormatMemoriesBlock(memories string) string {
	if memories == "" {
		return ""
	}
	return "<hindsight_memories>\nRelevant memories from past tasks:\n" + memories + "\n</hindsight_memories>"
}

// BuildRetainContent formats a task outcome into a string suitable for retention.
func BuildRetainContent(workspaceID, issueID, agentName, result, comment string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace: %s\n", workspaceID)
	fmt.Fprintf(&b, "Issue: %s\n", issueID)
	if agentName != "" {
		fmt.Fprintf(&b, "Agent: %s\n", agentName)
	}
	fmt.Fprintf(&b, "Status: %s\n", result)
	if comment != "" {
		b.WriteString("\nOutcome:\n")
		b.WriteString(comment)
	}
	return b.String()
}
