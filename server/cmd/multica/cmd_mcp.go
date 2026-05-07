package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp): cerebro modification of upstream file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// persistSessionFromStartup saves the ambient session discovered during startup.
func persistSessionFromStartup(gitRoot string, session *mcpSessionState) {
	if gitRoot == "" {
		return
	}
	wsID, issueID := session.ambient()
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
	var sessionState mcpSessionState
	if gitRoot != "" {
		bindings, _ := mcp.LoadRepoBindings()
		if b, ok := bindings[gitRoot]; ok && b.ActiveSession != nil && !b.ActiveSession.IsStale() {
			sessionState.setAmbient(b.ActiveSession.WorkSessionID, b.ActiveSession.IssueID)
			sessionState.track(b.ActiveSession.WorkSessionID, b.ActiveSession.IssueID, 0)
		}
	}
	// Fallback: check server for active session if none found locally.
	if sessionState.ambientWorkSessionID() == "" {
		var activeResp struct {
			Session *struct {
				ID      string `json:"id"`
				IssueID string `json:"issue_id"`
			} `json:"session"`
		}
		if err := client.GetJSON(context.Background(), "/api/work-sessions/active", &activeResp); err == nil && activeResp.Session != nil {
			sessionState.setAmbient(activeResp.Session.ID, activeResp.Session.IssueID)
			sessionState.track(activeResp.Session.ID, activeResp.Session.IssueID, 0)
			// Persist locally so next restart is instant.
			persistSessionFromStartup(gitRoot, &sessionState)
		}
	}

	registerTools(srv, client, &sessionState, workspaceID, projectID, gitRoot)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return srv.Run(ctx)
}

// sessionEntry holds per-session counters tracked locally so that parallel
// agents reporting on different work_session_ids cannot clobber each other's
// seq counter or auto-naming flag.
type sessionEntry struct {
	issueID string
	seq     int
	named   bool
}

// mcpSessionState tracks all known work sessions in this MCP process.
//
// The "ambient" session is what get_me reports and what restart-resume
// restores — it exists for single-agent convenience only. All routing of
// activity (report_activity, complete_work, ...) MUST be done via an
// explicit work_session_id parameter, otherwise parallel subagents sharing
// this MCP process will clobber each other.
type mcpSessionState struct {
	mu sync.Mutex

	ambientID    string
	ambientIssue string

	// sessions is keyed by work_session_id.
	sessions map[string]*sessionEntry
}

func (s *mcpSessionState) ambient() (workSessionID, issueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ambientID, s.ambientIssue
}

func (s *mcpSessionState) ambientWorkSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ambientID
}

func (s *mcpSessionState) setAmbient(workSessionID, issueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ambientID = workSessionID
	s.ambientIssue = issueID
}

func (s *mcpSessionState) clearAmbientIfMatches(workSessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ambientID == workSessionID {
		s.ambientID = ""
		s.ambientIssue = ""
		return true
	}
	return false
}

// track records a work session in the per-session map. Existing entries are
// replaced (e.g. to reset seq on attach).
func (s *mcpSessionState) track(workSessionID, issueID string, seq int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sessionEntry)
	}
	s.sessions[workSessionID] = &sessionEntry{issueID: issueID, seq: seq}
}

// nextSeq returns and increments the seq counter for the given work session.
// If the session is not yet tracked, it is created with seq starting at 1.
func (s *mcpSessionState) nextSeq(workSessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sessionEntry)
	}
	e, ok := s.sessions[workSessionID]
	if !ok {
		e = &sessionEntry{}
		s.sessions[workSessionID] = e
	}
	e.seq++
	return e.seq
}

// markNamed sets the named flag and returns true if the call was the first
// to set it. Used by report_activity to auto-name the session on first
// activity.
func (s *mcpSessionState) markNamed(workSessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sessionEntry)
	}
	e, ok := s.sessions[workSessionID]
	if !ok {
		e = &sessionEntry{}
		s.sessions[workSessionID] = e
	}
	if e.named {
		return false
	}
	e.named = true
	return true
}

// forget removes a tracked session. Called by complete_work.
func (s *mcpSessionState) forget(workSessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, workSessionID)
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
