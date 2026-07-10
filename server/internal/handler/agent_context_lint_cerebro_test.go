package handler

// Cerebro fork tests (FIR-1775 Phase 3): the context-lint endpoints must
// surface dead skill refs, duplicated rules, governance gaps, and repo-file
// drift against real workspace data.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/cerebro/agentoffice"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newAgentOfficeLintHandler() *agentoffice.Handler {
	return agentoffice.NewHandler(agentoffice.New(testHandler.CerebroQueries, testPool, nil))
}

func lintMemberRequest(method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	member := db.Member{
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(testUserID),
		Role:        "owner",
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}

func createLintTestSkill(t *testing.T, name, content string) string {
	t.Helper()
	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content)
		VALUES ($1, $2, '', $3)
		RETURNING id
	`, testWorkspaceID, name, content).Scan(&skillID); err != nil {
		t.Fatalf("create test skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

func TestAgentContextLintEndpoint(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	office := newAgentOfficeLintHandler()

	suffix := uuid.NewString()[:8]
	rule := "Never claim work is done without a green run of the full verification pipeline."
	boundSkillID := createLintTestSkill(t, "lint-bound-"+suffix, "1. "+rule+"\n")
	createLintTestSkill(t, "lint-unbound-"+suffix, "Some other content for the unbound skill test case.\n")

	agentID := createHandlerTestAgent(t, "lint-agent-"+suffix, []byte(`{}`))
	instructions := "- " + rule + "\n" +
		"Follow the `lint-unbound-" + suffix + "` skill before posting.\n" +
		"Also use the `lint-missing-" + suffix + "` skill for edge cases.\n"
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET instructions = $2 WHERE id = $1`, agentID, instructions); err != nil {
		t.Fatalf("set instructions: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, boundSkillID); err != nil {
		t.Fatalf("bind skill: %v", err)
	}

	w := httptest.NewRecorder()
	req := lintMemberRequest("GET", "/api/agents/"+agentID+"/context/lint", nil)
	req = withURLParam(req, "id", agentID)
	office.LintAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LintAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var report agentoffice.LintReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.AgentID != agentID {
		t.Fatalf("report agent_id = %q, want %q", report.AgentID, agentID)
	}
	codes := map[string]int{}
	for _, f := range report.Findings {
		codes[f.Code]++
	}
	for _, want := range []string{
		"missing_context_owner", // created without a context owner
		"dead_skill_ref",        // lint-missing-… does not exist
		"unbound_skill_ref",     // lint-unbound-… exists but is not bound
		"duplicated_rule",       // rule in instructions AND the bound skill
	} {
		if codes[want] == 0 {
			t.Fatalf("expected a %s finding, got %+v", want, report.Findings)
		}
	}

	// Workspace sweep includes this agent's report.
	w = httptest.NewRecorder()
	office.LintWorkspace(w, lintMemberRequest("GET", "/api/agents/context/lint", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("LintWorkspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sweep agentoffice.WorkspaceLintResponse
	if err := json.Unmarshal(w.Body.Bytes(), &sweep); err != nil {
		t.Fatalf("decode sweep: %v", err)
	}
	found := false
	for _, r := range sweep.Agents {
		if r.AgentID == agentID {
			found = len(r.Findings) > 0
		}
	}
	if !found {
		t.Fatalf("sweep must include the lint test agent with findings, got %d agents, %d total findings",
			len(sweep.Agents), sweep.TotalFindings)
	}
}

func TestAgentContextLintRepoFileEndpoint(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	office := newAgentOfficeLintHandler()

	suffix := uuid.NewString()[:8]
	rule := "Always attach proof from a green CI run before claiming anything is live at all."
	agentID := createHandlerTestAgent(t, "lint-repo-agent-"+suffix, []byte(`{}`))
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET instructions = $2 WHERE id = $1`, agentID, "- "+rule+"\n"); err != nil {
		t.Fatalf("set instructions: %v", err)
	}

	content := "# Project\nRun make dev to start.\n" + rule + "\nNever mention another agent in a closing comment.\n"
	w := httptest.NewRecorder()
	req := lintMemberRequest("POST", "/api/agents/context/lint/repo-file",
		map[string]any{"filename": "CLAUDE.md", "content": content})
	office.LintRepoFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LintRepoFile: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Filename string                    `json:"filename"`
		Findings []agentoffice.LintFinding `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	codes := map[string]int{}
	for _, f := range resp.Findings {
		codes[f.Code]++
	}
	if codes["duplicated_from_harness"] == 0 {
		t.Fatalf("expected duplicated_from_harness (rule copied from the agent), got %+v", resp.Findings)
	}
	if codes["agent_behavior_in_repo_file"] == 0 {
		t.Fatalf("expected agent_behavior_in_repo_file (mention rule), got %+v", resp.Findings)
	}

	// Missing content is a 400, not a silent empty report.
	w = httptest.NewRecorder()
	req = lintMemberRequest("POST", "/api/agents/context/lint/repo-file",
		map[string]any{"filename": "CLAUDE.md"})
	office.LintRepoFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("LintRepoFile without content: expected 400, got %d", w.Code)
	}
}
