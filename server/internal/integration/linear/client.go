package linear

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const (
	linearAPIEndpoint = "https://api.linear.app/graphql"
	requestTimeout    = 30 * time.Second
)

// Client is a thin Linear GraphQL client for syncing status back.
type Client struct {
	apiKey   string
	endpoint string
	http     *http.Client
}

// NewClient creates a Linear API client using the LINEAR_API_KEY env var.
func NewClient() *Client {
	return &Client{
		apiKey:   os.Getenv("LINEAR_API_KEY"),
		endpoint: linearAPIEndpoint,
		http:     &http.Client{Timeout: requestTimeout},
	}
}

// NewClientWithKey creates a Linear API client with an explicit API key.
func NewClientWithKey(apiKey string) *Client {
	return &Client{
		apiKey:   apiKey,
		endpoint: linearAPIEndpoint,
		http:     &http.Client{Timeout: requestTimeout},
	}
}

// Available returns true if the client has credentials configured.
func (c *Client) Available() bool {
	return c.apiKey != ""
}

// graphqlRequest executes a GraphQL query against Linear.
func (c *Client) graphqlRequest(query string, variables map[string]any) (map[string]any, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY not configured")
	}

	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linear api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("linear api status %d: %s", resp.StatusCode, truncateBytes(respBody, 200))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if errs, ok := result["errors"]; ok {
		return nil, fmt.Errorf("linear graphql errors: %v", errs)
	}

	return result, nil
}

const resolveStateIDQuery = `
query ResolveStateId($issueId: String!, $stateName: String!) {
  issue(id: $issueId) {
    team {
      states(filter: {name: {eq: $stateName}}, first: 1) {
        nodes { id }
      }
    }
  }
}`

// ResolveStateID looks up the workflow state ID by name for an issue's team.
func (c *Client) ResolveStateID(issueID, stateName string) (string, error) {
	result, err := c.graphqlRequest(resolveStateIDQuery, map[string]any{
		"issueId":   issueID,
		"stateName": stateName,
	})
	if err != nil {
		return "", err
	}

	data, _ := result["data"].(map[string]any)
	issue, _ := data["issue"].(map[string]any)
	team, _ := issue["team"].(map[string]any)
	states, _ := team["states"].(map[string]any)
	nodes, _ := states["nodes"].([]any)
	if len(nodes) == 0 {
		return "", fmt.Errorf("state %q not found for issue %s", stateName, issueID)
	}
	node, _ := nodes[0].(map[string]any)
	stateID, _ := node["id"].(string)
	if stateID == "" {
		return "", fmt.Errorf("state %q resolved to empty ID", stateName)
	}
	return stateID, nil
}

const updateIssueStateMutation = `
mutation UpdateIssueState($issueId: String!, $stateId: String!) {
  issueUpdate(id: $issueId, input: {stateId: $stateId}) {
    success
  }
}`

// UpdateIssueState moves a Linear issue to the given state name.
func (c *Client) UpdateIssueState(issueID, stateName string) error {
	stateID, err := c.ResolveStateID(issueID, stateName)
	if err != nil {
		return fmt.Errorf("resolve state: %w", err)
	}

	result, err := c.graphqlRequest(updateIssueStateMutation, map[string]any{
		"issueId": issueID,
		"stateId": stateID,
	})
	if err != nil {
		return err
	}

	data, _ := result["data"].(map[string]any)
	update, _ := data["issueUpdate"].(map[string]any)
	success, _ := update["success"].(bool)
	if !success {
		return fmt.Errorf("issueUpdate returned success=false")
	}

	slog.Info("linear: updated issue state", "issue_id", issueID, "state", stateName)
	return nil
}

const createCommentMutation = `
mutation CreateComment($issueId: String!, $body: String!) {
  commentCreate(input: {issueId: $issueId, body: $body}) {
    success
  }
}`

// CreateComment posts a comment on a Linear issue.
func (c *Client) CreateComment(issueID, body string) error {
	result, err := c.graphqlRequest(createCommentMutation, map[string]any{
		"issueId": issueID,
		"body":    body,
	})
	if err != nil {
		return err
	}

	data, _ := result["data"].(map[string]any)
	create, _ := data["commentCreate"].(map[string]any)
	success, _ := create["success"].(bool)
	if !success {
		return fmt.Errorf("commentCreate returned success=false")
	}

	return nil
}

func truncateBytes(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "..."
}
