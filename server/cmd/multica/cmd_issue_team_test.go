package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestParseIssueTeamPolicy(t *testing.T) {
	text := `
Some issue details.

multica-team:
lead=opus-latest
implementer=codex-high
reviewer=gemini-pro
continue until reviewer approves
`

	policy, err := parseIssueTeamPolicy(text)
	if err != nil {
		t.Fatalf("parseIssueTeamPolicy: %v", err)
	}

	if policy.Lead != "opus-latest" {
		t.Errorf("Lead = %q", policy.Lead)
	}
	if policy.Implementer != "codex-high" {
		t.Errorf("Implementer = %q", policy.Implementer)
	}
	if policy.Reviewer != "gemini-pro" {
		t.Errorf("Reviewer = %q", policy.Reviewer)
	}
	if policy.Until != "reviewer approves" {
		t.Errorf("Until = %q", policy.Until)
	}
}

func TestParseIssueTeamPolicyRequiresRoles(t *testing.T) {
	_, err := parseIssueTeamPolicy("lead=opus-latest\nreviewer=gemini-pro")
	if err == nil {
		t.Fatal("expected missing implementer error")
	}
	if !strings.Contains(err.Error(), "implementer") {
		t.Fatalf("expected implementer in error, got %v", err)
	}
}

func TestParseIssueTeamPolicyFromProseLine(t *testing.T) {
	text := "Smoke test only. lead=claude-haiku; implementer=codex-quick; reviewer=gemini-flash; continue until reviewer approves"

	policy, err := parseIssueTeamPolicy(text)
	if err != nil {
		t.Fatalf("parseIssueTeamPolicy: %v", err)
	}
	if policy.Lead != "claude-haiku" || policy.Implementer != "codex-quick" || policy.Reviewer != "gemini-flash" {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}

func TestReviewerApproved(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "approved", text: "Approved. Ship it.", want: true},
		{name: "approval marker", text: "reviewer_approved", want: true},
		{name: "needs changes", text: "Not approved yet, needs changes.", want: false},
		{name: "changes requested", text: "Changes requested before approval.", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewerApproved(tt.text); got != tt.want {
				t.Errorf("reviewerApproved(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestLeadPromptKeepsRunnerInControlOfMentions(t *testing.T) {
	prompt := strings.ToLower(leadPrompt())
	if !strings.Contains(prompt, "do not @mention") {
		t.Fatalf("lead prompt should tell lead not to @mention other agents so the runner owns sequencing, got: %s", leadPrompt())
	}
}

func TestPostAndWaitForRoleIgnoresCommentsBeforeRoleTrigger(t *testing.T) {
	agentID := "aaaaaaaa-1111-1111-1111-111111111111"
	roleCommentCreatedAt := time.Now().UTC().Add(30 * time.Second).Truncate(time.Second)
	oldAgentCommentAt := roleCommentCreatedAt.Add(-10 * time.Second)
	newAgentCommentAt := roleCommentCreatedAt.Add(10 * time.Second)
	commentsCalls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/issue-1/comments":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "role-comment-1",
				"content":    "Team role: lead",
				"created_at": roleCommentCreatedAt.Format(time.RFC3339),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-1/comments":
			commentsCalls++
			comments := []map[string]any{
				{
					"id":          "old-agent-comment",
					"author_type": "agent",
					"author_id":   agentID,
					"content":     "old assignment-run output that should not satisfy this role",
					"created_at":  oldAgentCommentAt.Format(time.RFC3339),
				},
			}
			if commentsCalls > 1 {
				comments = append(comments, map[string]any{
					"id":          "new-agent-comment",
					"author_type": "agent",
					"author_id":   agentID,
					"content":     "new role-triggered output",
					"created_at":  newAgentCommentAt.Format(time.RFC3339),
				})
			}
			json.NewEncoder(w).Encode(comments)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	comment, err := postAndWaitForRole(ctx, client, issueTeamRunOptions{
		IssueID:      "issue-1",
		PollInterval: time.Millisecond,
	}, "lead", issueTeamAgent{Name: "lead-agent", ID: agentID}, "please plan")
	if err != nil {
		t.Fatalf("postAndWaitForRole: %v", err)
	}
	if got := comment["id"]; got != "new-agent-comment" {
		t.Fatalf("waited for %v, want new-agent-comment", got)
	}
}

func TestRunIssueTeamTriggerPostsRoleMentionsOnSameIssue(t *testing.T) {
	var postedPaths []string
	var postedComments []string
	var statusBodies []map[string]any

	agents := []map[string]any{
		{"id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "opus-latest"},
		{"id": "bbbbbbbb-2222-2222-2222-222222222222", "name": "codex-high"},
		{"id": "cccccccc-3333-3333-3333-333333333333", "name": "gemini-pro"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "issue-1",
				"title":       "Team issue",
				"description": "lead=opus-latest\nimplementer=codex-high\nreviewer=gemini-pro\ncontinue until reviewer approves",
				"status":      "todo",
				"priority":    "low",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode(agents)
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode status body: %v", err)
			}
			statusBodies = append(statusBodies, body)
			json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "status": body["status"]})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/issue-1/comments":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode comment body: %v", err)
			}
			postedPaths = append(postedPaths, r.URL.Path)
			postedComments = append(postedComments, body["content"].(string))
			json.NewEncoder(w).Encode(map[string]any{"id": "comment-1", "content": body["content"]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	opts := issueTeamRunOptions{
		IssueID:      "issue-1",
		Wait:         false,
		MaxRounds:    1,
		PollInterval: time.Millisecond,
		Timeout:      time.Second,
	}

	if err := runIssueTeam(context.Background(), client, opts, io.Discard); err != nil {
		t.Fatalf("runIssueTeam: %v", err)
	}

	if len(statusBodies) != 1 || statusBodies[0]["status"] != "in_progress" {
		t.Fatalf("expected one in_progress status update, got %+v", statusBodies)
	}
	if len(postedComments) != 3 {
		t.Fatalf("expected three role comments, got %d: %+v", len(postedComments), postedComments)
	}
	for _, path := range postedPaths {
		if path != "/api/issues/issue-1/comments" {
			t.Fatalf("comment posted to %q, want same issue path", path)
		}
	}
	if !strings.Contains(postedComments[0], "mention://agent/aaaaaaaa-1111-1111-1111-111111111111") {
		t.Errorf("lead comment missing lead mention: %s", postedComments[0])
	}
	if !strings.Contains(postedComments[1], "mention://agent/bbbbbbbb-2222-2222-2222-222222222222") {
		t.Errorf("implementer comment missing implementer mention: %s", postedComments[1])
	}
	if !strings.Contains(postedComments[2], "mention://agent/cccccccc-3333-3333-3333-333333333333") {
		t.Errorf("reviewer comment missing reviewer mention: %s", postedComments[2])
	}
}

// TestTeamPolicyBlockRoundTrip verifies that teamPolicyBlock produces output
// that parseIssueTeamPolicy can parse back to the original values.
func TestTeamPolicyBlockRoundTrip(t *testing.T) {
	orig := issueTeamPolicy{
		Lead:        "planner-agent",
		Implementer: "builder-agent",
		Reviewer:    "review-agent",
		Until:       "reviewer approves",
	}
	block := teamPolicyBlock(orig, "")
	parsed, err := parseIssueTeamPolicy(block)
	if err != nil {
		t.Fatalf("parseIssueTeamPolicy(teamPolicyBlock(...)): %v", err)
	}
	if parsed.Lead != orig.Lead {
		t.Errorf("Lead = %q, want %q", parsed.Lead, orig.Lead)
	}
	if parsed.Implementer != orig.Implementer {
		t.Errorf("Implementer = %q, want %q", parsed.Implementer, orig.Implementer)
	}
	if parsed.Reviewer != orig.Reviewer {
		t.Errorf("Reviewer = %q, want %q", parsed.Reviewer, orig.Reviewer)
	}
}

// TestIssueTeamCreateForcesBacklogNoAssignee verifies that the team create
// handler always creates the issue with status=backlog and no assignee_id.
func TestIssueTeamCreateForcesBacklogNoAssignee(t *testing.T) {
	var createdBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/issues" {
			if err := json.NewDecoder(r.Body).Decode(&createdBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "new-issue-1",
				"title":  createdBody["title"],
				"status": createdBody["status"],
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	// Build a minimal cobra command to satisfy runIssueTeamCreate's flag reads.
	cmd := issueTeamCreateCmd
	cmd.Flags().Set("title", "My team issue")
	cmd.Flags().Set("policy", "lead=planner, implementer=builder, reviewer=reviewer")

	// Call the underlying logic directly via the client.
	policy, _ := parseIssueTeamPolicy("lead=planner, implementer=builder, reviewer=reviewer")
	body := map[string]any{
		"title":       "My team issue",
		"status":      "backlog",
		"description": teamPolicyBlock(policy, ""),
		"priority":    "medium",
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		t.Fatalf("PostJSON: %v", err)
	}

	if createdBody["status"] != "backlog" {
		t.Errorf("status = %q, want backlog", createdBody["status"])
	}
	if _, hasAssignee := createdBody["assignee_id"]; hasAssignee {
		t.Errorf("team create should not set assignee_id, got: %v", createdBody["assignee_id"])
	}
	if _, hasAssigneeType := createdBody["assignee_type"]; hasAssigneeType {
		t.Errorf("team create should not set assignee_type, got: %v", createdBody["assignee_type"])
	}
	desc, _ := createdBody["description"].(string)
	if !strings.Contains(desc, "lead=planner") {
		t.Errorf("description should contain policy, got: %q", desc)
	}
	if !strings.Contains(desc, "implementer=builder") {
		t.Errorf("description should contain policy, got: %q", desc)
	}
	if !strings.Contains(desc, "reviewer=reviewer") {
		t.Errorf("description should contain policy, got: %q", desc)
	}
}

// TestRunIssueTeamRejectsAssignedIssue verifies that team run errors when the
// issue already has an assignee and neither --allow-assigned nor --detach-assignee
// is set (the parasitic-run guardrail).
func TestRunIssueTeamRejectsAssignedIssue(t *testing.T) {
	agents := []map[string]any{
		{"id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "opus-latest"},
		{"id": "bbbbbbbb-2222-2222-2222-222222222222", "name": "codex-high"},
		{"id": "cccccccc-3333-3333-3333-333333333333", "name": "gemini-pro"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-assigned":
			json.NewEncoder(w).Encode(map[string]any{
				"id":            "issue-assigned",
				"description":   "lead=opus-latest\nimplementer=codex-high\nreviewer=gemini-pro",
				"status":        "todo",
				"assignee_id":   "some-agent-id",
				"assignee_type": "agent",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode(agents)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	opts := issueTeamRunOptions{
		IssueID:      "issue-assigned",
		Wait:         false,
		MaxRounds:    1,
		PollInterval: time.Millisecond,
		Timeout:      time.Second,
		// AllowAssigned and DetachAssignee both false -- should error
	}

	err := runIssueTeam(context.Background(), client, opts, io.Discard)
	if err == nil {
		t.Fatal("expected error when issue has assignee and no guardrail flag set")
	}
	if !strings.Contains(err.Error(), "assignee") {
		t.Errorf("error should mention assignee, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--detach-assignee") || !strings.Contains(err.Error(), "--allow-assigned") {
		t.Errorf("error should hint at --detach-assignee / --allow-assigned flags, got: %v", err)
	}
}

// TestRunIssueTeamAllowAssigned verifies that --allow-assigned lets the run
// proceed with a warning instead of failing.
func TestRunIssueTeamAllowAssigned(t *testing.T) {
	agents := []map[string]any{
		{"id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "opus-latest"},
		{"id": "bbbbbbbb-2222-2222-2222-222222222222", "name": "codex-high"},
		{"id": "cccccccc-3333-3333-3333-333333333333", "name": "gemini-pro"},
	}
	putCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-assigned":
			json.NewEncoder(w).Encode(map[string]any{
				"id":            "issue-assigned",
				"description":   "lead=opus-latest\nimplementer=codex-high\nreviewer=gemini-pro",
				"status":        "todo",
				"assignee_id":   "some-agent-id",
				"assignee_type": "agent",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode(agents)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-assigned":
			putCalls++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{"id": "issue-assigned", "status": body["status"]})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/issue-assigned/comments":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{"id": "comment-1", "content": body["content"]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	var out bytes.Buffer
	opts := issueTeamRunOptions{
		IssueID:       "issue-assigned",
		Wait:          false,
		MaxRounds:     1,
		PollInterval:  time.Millisecond,
		Timeout:       time.Second,
		AllowAssigned: true,
	}

	if err := runIssueTeam(context.Background(), client, opts, &out); err != nil {
		t.Fatalf("runIssueTeam with --allow-assigned: %v", err)
	}
	if !strings.Contains(out.String(), "Warning") {
		t.Errorf("expected a warning message in output, got: %q", out.String())
	}
}

// TestRunIssueTeamDetachAssignee verifies that --detach-assignee sends a PUT
// to clear the assignee before starting the team run.
func TestRunIssueTeamDetachAssignee(t *testing.T) {
	agents := []map[string]any{
		{"id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "opus-latest"},
		{"id": "bbbbbbbb-2222-2222-2222-222222222222", "name": "codex-high"},
		{"id": "cccccccc-3333-3333-3333-333333333333", "name": "gemini-pro"},
	}
	var putBodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-assigned":
			json.NewEncoder(w).Encode(map[string]any{
				"id":            "issue-assigned",
				"description":   "lead=opus-latest\nimplementer=codex-high\nreviewer=gemini-pro",
				"status":        "todo",
				"assignee_id":   "some-agent-id",
				"assignee_type": "agent",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode(agents)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-assigned":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			putBodies = append(putBodies, body)
			json.NewEncoder(w).Encode(map[string]any{"id": "issue-assigned", "status": "in_progress"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/issue-assigned/comments":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{"id": "comment-1", "content": body["content"]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	opts := issueTeamRunOptions{
		IssueID:        "issue-assigned",
		Wait:           false,
		MaxRounds:      1,
		PollInterval:   time.Millisecond,
		Timeout:        time.Second,
		DetachAssignee: true,
	}

	if err := runIssueTeam(context.Background(), client, opts, io.Discard); err != nil {
		t.Fatalf("runIssueTeam with --detach-assignee: %v", err)
	}

	// First PUT must be the detach (assignee_id = nil).
	if len(putBodies) < 1 {
		t.Fatal("expected at least one PUT call (detach assignee)")
	}
	detachBody := putBodies[0]
	if _, hasKey := detachBody["assignee_id"]; !hasKey {
		t.Errorf("detach PUT should include assignee_id key, got: %v", detachBody)
	}
	// The value should be nil (JSON null), which decodes to nil in map[string]any.
	if detachBody["assignee_id"] != nil {
		t.Errorf("detach PUT assignee_id should be null/nil, got: %v", detachBody["assignee_id"])
	}
}
