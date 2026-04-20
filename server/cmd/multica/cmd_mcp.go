package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// persistSessionFromStartup saves session state discovered during startup.
func persistSessionFromStartup(gitRoot string, session *mcpSessionState) {
	if gitRoot == "" || session.WorkSessionID == "" {
		return
	}
	bindings, err := mcp.LoadRepoBindings()
	if err != nil {
		return
	}
	binding, ok := bindings[gitRoot]
	if !ok {
		return
	}
	binding.ActiveSession = &mcp.ActiveSession{
		WorkSessionID: session.WorkSessionID,
		IssueID:       session.IssueID,
		AttachedAt:    time.Now(),
	}
	bindings[gitRoot] = binding
	mcp.SaveRepoBindings(bindings)
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol server",
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP stdio server for Claude Code integration",
	RunE:  runMCPServe,
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)
}

func runMCPServe(cmd *cobra.Command, _ []string) error {
	// Load config.
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated. Run 'multica login' first")
	}

	serverURL := resolveServerURL(cmd)
	workspaceID := resolveWorkspaceID(cmd)

	// Detect git repo and apply repo binding.
	var projectID string
	gitRoot := mcp.DetectGitRoot()
	if gitRoot != "" {
		bindings, err := mcp.LoadRepoBindings()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load repo bindings: %v\n", err)
		} else if binding, ok := bindings[gitRoot]; ok {
			if binding.WorkspaceID != "" {
				workspaceID = binding.WorkspaceID
			}
			projectID = binding.ProjectID
		}
	}

	_ = cfg // used for profile resolution above

	client := cli.NewAPIClient(serverURL, workspaceID, token)

	// Build MCP server.
	srv := mcp.NewServer("multica", version)

	// Session state for attach/complete/progress — restore from persisted binding or server.
	var sessionState mcpSessionState
	if gitRoot != "" {
		bindings, _ := mcp.LoadRepoBindings()
		if b, ok := bindings[gitRoot]; ok && b.ActiveSession != nil && !b.ActiveSession.IsStale() {
			sessionState.WorkSessionID = b.ActiveSession.WorkSessionID
			sessionState.IssueID = b.ActiveSession.IssueID
		}
	}
	// Fallback: check server for active session if none found locally.
	if sessionState.WorkSessionID == "" {
		var activeResp struct {
			Session *struct {
				ID      string `json:"id"`
				IssueID string `json:"issue_id"`
			} `json:"session"`
		}
		if err := client.GetJSON(context.Background(), "/api/work-sessions/active", &activeResp); err == nil && activeResp.Session != nil {
			sessionState.WorkSessionID = activeResp.Session.ID
			sessionState.IssueID = activeResp.Session.IssueID
			// Persist locally so next restart is instant.
			persistSessionFromStartup(gitRoot, &sessionState)
		}
	}

	registerTools(srv, client, &sessionState, workspaceID, projectID, gitRoot)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return srv.Run(ctx)
}

// mcpSessionState tracks the active work session for attach/complete/progress tools.
type mcpSessionState struct {
	WorkSessionID string
	IssueID       string
	Seq           int
}

// jsonText marshals v to JSON and returns a TextResult.
func jsonText(v any) (mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.ErrorResult("json encode error"), err
	}
	return mcp.TextResult(string(data)), nil
}

// requireString extracts a required string argument.
func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("parameter %s must be a non-empty string", key)
	}
	return s, nil
}

// optString extracts an optional string argument.
func optString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// optInt extracts an optional int argument (JSON numbers come as float64).
func optInt(args map[string]any, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return defaultVal
}
