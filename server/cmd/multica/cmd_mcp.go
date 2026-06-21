package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp): cerebro modification of upstream file

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cerebro/clitools"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// persistSessionFromStartup saves the ambient session discovered during startup.
func persistSessionFromStartup(gitRoot string, session *clitools.SessionState) {
	if gitRoot == "" {
		return
	}
	wsID, issueID := session.Ambient()
	if wsID == "" {
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
		WorkSessionID: wsID,
		IssueID:       issueID,
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

	// Fallback: if no project from local binding, try matching git remote to a project's repo_url.
	if projectID == "" && gitRoot != "" {
		if repoURL := mcp.DetectGitRemote(gitRoot); repoURL != "" {
			var resp struct {
				Project *struct {
					ID string `json:"id"`
				} `json:"project"`
			}
			if err := client.GetJSON(context.Background(), "/api/projects/by-repo?url="+repoURL, &resp); err == nil && resp.Project != nil {
				projectID = resp.Project.ID
			}
		}
	}

	// Build MCP server.
	srv := mcp.NewServer("multica", version)

	// Session state for attach/complete/progress — restore from persisted binding or server.
	var sessionState clitools.SessionState
	if gitRoot != "" {
		bindings, _ := mcp.LoadRepoBindings()
		if b, ok := bindings[gitRoot]; ok && b.ActiveSession != nil && !b.ActiveSession.IsStale() {
			sessionState.SetAmbient(b.ActiveSession.WorkSessionID, b.ActiveSession.IssueID)
			sessionState.Track(b.ActiveSession.WorkSessionID, b.ActiveSession.IssueID, 0)
		}
	}
	// Fallback: check server for active session if none found locally.
	if sessionState.AmbientWorkSessionID() == "" {
		var activeResp struct {
			Session *struct {
				ID      string `json:"id"`
				IssueID string `json:"issue_id"`
			} `json:"session"`
		}
		if err := client.GetJSON(context.Background(), "/api/work-sessions/active", &activeResp); err == nil && activeResp.Session != nil {
			sessionState.SetAmbient(activeResp.Session.ID, activeResp.Session.IssueID)
			sessionState.Track(activeResp.Session.ID, activeResp.Session.IssueID, 0)
			// Persist locally so next restart is instant.
			persistSessionFromStartup(gitRoot, &sessionState)
		}
	}

	// CEREBRO-PATCH(mcp-cli-clitools-extract): FIR-1449 tool registry lifted to importable clitools package.
	clitools.RegisterTools(srv, client, &sessionState, workspaceID, projectID, gitRoot)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return srv.Run(ctx)
}
