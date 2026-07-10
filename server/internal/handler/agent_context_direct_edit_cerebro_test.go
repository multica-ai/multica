package handler

// Cerebro fork tests (FIR-1775 Phase 2): a direct edit through UpdateAgent
// must land in the agent context version history via the
// AgentContextDirectEdit seam, and an edit that touches no versioned field
// must NOT append a version.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/cerebro/agentoffice"
)

func TestUpdateAgentDirectEditRecordsContextVersion(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "direct-edit-agent-"+uuid.NewString()[:8], []byte(`{}`))

	// Wire the Agent Office direct-edit recorder the way router.go does.
	testHandler.AgentContextDirectEdit = agentoffice.New(testHandler.CerebroQueries, testPool, nil)
	t.Cleanup(func() { testHandler.AgentContextDirectEdit = nil })

	updateAgent := func(t *testing.T, body map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/agents/"+agentID, body)
		req = withURLParam(req, "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	countVersions := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM agent_context_version WHERE agent_id = $1`, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count versions: %v", err)
		}
		return n
	}

	// 1. Editing a versioned field (instructions) appends a snapshot and bumps
	// the pointer from the 1.0.0 default.
	updateAgent(t, map[string]any{"instructions": "Always verify before claiming done."})

	var contextVersion string
	if err := testPool.QueryRow(ctx,
		`SELECT context_version FROM agent WHERE id = $1`, agentID,
	).Scan(&contextVersion); err != nil {
		t.Fatalf("load context_version: %v", err)
	}
	if contextVersion != "1.0.1" {
		t.Fatalf("expected context_version 1.0.1 after direct edit, got %q", contextVersion)
	}
	var snapInstructions, description, createdBy string
	if err := testPool.QueryRow(ctx, `
		SELECT snapshot->>'instructions', description, COALESCE(created_by::text, '')
		FROM agent_context_version WHERE agent_id = $1 AND version = '1.0.1'
	`, agentID).Scan(&snapInstructions, &description, &createdBy); err != nil {
		t.Fatalf("load version 1.0.1: %v", err)
	}
	if snapInstructions != "Always verify before claiming done." {
		t.Fatalf("snapshot instructions = %q, want the edited value", snapInstructions)
	}
	if description != "Direct edit via agent update API" {
		t.Fatalf("version description = %q", description)
	}
	if createdBy != testUserID {
		t.Fatalf("created_by = %q, want editor %q", createdBy, testUserID)
	}
	if got := countVersions(t); got != 1 {
		t.Fatalf("expected 1 version row, got %d", got)
	}

	// 2. An edit that touches no versioned field (rename) must not append.
	updateAgent(t, map[string]any{"name": "direct-edit-renamed-" + uuid.NewString()[:8]})
	if got := countVersions(t); got != 1 {
		t.Fatalf("rename must not record a version; got %d rows", got)
	}

	// 3. A second versioned-field edit (model) appends again and keeps the
	// pointer monotonic.
	updateAgent(t, map[string]any{"model": "claude-fable-5"})
	if got := countVersions(t); got != 2 {
		t.Fatalf("expected 2 version rows after model edit, got %d", got)
	}
	var snapModel string
	if err := testPool.QueryRow(ctx, `
		SELECT snapshot->>'model' FROM agent_context_version
		WHERE agent_id = $1 AND version = '1.0.2'
	`, agentID).Scan(&snapModel); err != nil {
		t.Fatalf("load version 1.0.2: %v", err)
	}
	if snapModel != "claude-fable-5" {
		t.Fatalf("snapshot model = %q, want claude-fable-5", snapModel)
	}
}

// TestUpdateAgentDirectEditSeamNil verifies UpdateAgent still works on a
// server without Agent Office wired (seam nil → no recording, no panic).
func TestUpdateAgentDirectEditSeamNil(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "direct-edit-nil-seam-"+uuid.NewString()[:8], []byte(`{}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{"description": "no recorder wired"})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent with nil seam: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_context_version WHERE agent_id = $1`, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if n != 0 {
		t.Fatalf("nil seam must not record versions, got %d rows", n)
	}
}
