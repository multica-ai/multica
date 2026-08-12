package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createMika(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := withChatTestWorkspaceCtx(t, newRequest("POST", "/api/agents/mika", body))
	w := httptest.NewRecorder()
	testHandler.CreateMikaAgent(w, req)
	return w
}

func decodeAgent(t *testing.T, w *httptest.ResponseRecorder) AgentResponse {
	t.Helper()
	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode agent response: %v (body %s)", err, w.Body.String())
	}
	return resp
}

func cleanupMika(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND system_key = $2`,
			testWorkspaceID, service.MikaSystemKey)
	})
}

// TestCreateMikaAgent_ServerOwnsTheDefinition is the point of moving creation
// server-side: the caller sends only a runtime and a language, and everything
// that makes Mika Mika is decided here.
func TestCreateMikaAgent_ServerOwnsTheDefinition(t *testing.T) {
	cleanupMika(t)

	w := createMika(t, map[string]any{
		"runtime_id": handlerTestRuntimeID(t),
		"language":   "en",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeAgent(t, w)

	if resp.SystemKey != service.MikaSystemKey {
		t.Fatalf("system_key = %q, want %q", resp.SystemKey, service.MikaSystemKey)
	}
	if resp.Name != service.MikaDefaultName {
		t.Fatalf("name = %q, want %q", resp.Name, service.MikaDefaultName)
	}
	if resp.PermissionMode != mikaAgentPermissionMode {
		t.Fatalf("permission_mode = %q, want %q", resp.PermissionMode, mikaAgentPermissionMode)
	}
	// The workspace half starts empty — the product half is never written to
	// the row, which is what keeps a release from overwriting workspace notes.
	if resp.Instructions != "" {
		t.Fatalf("instructions must start empty, got %q", resp.Instructions)
	}
	if !strings.Contains(resp.SystemInstructions, "You are Mika") {
		t.Fatalf("system_instructions should carry the product prompt, got %q", resp.SystemInstructions)
	}

	// kind stays 'user' so Mika keeps appearing in agent lists and assignment
	// surfaces, and survives runtime teardown.
	var kind string
	if err := testPool.QueryRow(context.Background(),
		`SELECT kind FROM agent WHERE id = $1`, resp.ID).Scan(&kind); err != nil {
		t.Fatalf("load agent kind: %v", err)
	}
	if kind != "user" {
		t.Fatalf("kind = %q, want \"user\" — 'system' hides the row and deletes it with its runtime", kind)
	}
}

func TestCreateMikaAgent_IsIdempotentPerWorkspace(t *testing.T) {
	cleanupMika(t)
	runtimeID := handlerTestRuntimeID(t)

	first := createMika(t, map[string]any{"runtime_id": runtimeID, "language": "en"})
	if first.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	second := createMika(t, map[string]any{"runtime_id": runtimeID, "language": "zh"})
	if second.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", second.Code, second.Body.String())
	}
	if a, b := decodeAgent(t, first).ID, decodeAgent(t, second).ID; a != b {
		t.Fatalf("expected the same agent back, got %s then %s", a, b)
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent WHERE workspace_id = $1 AND system_key = $2`,
		testWorkspaceID, service.MikaSystemKey).Scan(&count); err != nil {
		t.Fatalf("count mika agents: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 Mika in the workspace, got %d", count)
	}
}

func TestCreateMikaAgent_RejectsUnsupportedLanguage(t *testing.T) {
	cleanupMika(t)
	w := createMika(t, map[string]any{"runtime_id": handlerTestRuntimeID(t), "language": "fr"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestComposeMikaInstructions covers the layering contract: the product half
// always leads, workspace notes are labelled with their provenance, and an
// empty second layer adds nothing — including the section heading that
// describes it.
func TestComposeMikaInstructions(t *testing.T) {
	system := service.MikaSystemInstructions(service.MikaDefaultName)

	if got := service.ComposeMikaInstructions(service.MikaDefaultName, ""); got != system {
		t.Fatal("an empty workspace layer must compose to exactly the system layer")
	}
	if got := service.ComposeMikaInstructions(service.MikaDefaultName, "   \n  "); got != system {
		t.Fatal("a blank workspace layer must compose to exactly the system layer")
	}
	// Without notes the prompt must not end by announcing a section that has
	// nothing under it.
	if strings.Contains(system, "Workspace notes below add") {
		t.Fatalf("the notes rule must not appear when there are no notes:\n%s", system)
	}

	composed := service.ComposeMikaInstructions(service.MikaDefaultName, "Our main repo is acme/platform.")
	if !strings.HasPrefix(composed, system) {
		t.Fatal("the system layer must lead the composed prompt")
	}
	for _, want := range []string{
		"## Workspace notes",
		"Workspace notes below add",
		"Added by this workspace's admins",
		"Our main repo is acme/platform.",
	} {
		if !strings.Contains(composed, want) {
			t.Fatalf("composed prompt missing %q:\n%s", want, composed)
		}
	}
}

// The runtime brief announces "**You are: <name>**" from the agent row, so a
// hardcoded "You are Mika" would contradict it the moment an owner renames the
// agent.
func TestMikaSystemInstructionsUsesTheCurrentDisplayName(t *testing.T) {
	renamed := service.MikaSystemInstructions("Jarvis")
	if !strings.HasPrefix(renamed, "You are Jarvis,") {
		t.Fatalf("prompt should open as the current name:\n%s", renamed[:120])
	}
	if strings.Contains(renamed, "{{AGENT_NAME}}") {
		t.Fatal("the name placeholder must be substituted")
	}
	// The product identity is still stated, just not as the display name.
	if !strings.Contains(renamed, "built-in system agent (Mika)") {
		t.Fatal("prompt should still identify itself as Multica's built-in agent")
	}

	if blank := service.MikaSystemInstructions("   "); !strings.HasPrefix(blank, "You are Mika,") {
		t.Fatalf("a blank name should fall back to the default:\n%s", blank[:120])
	}
}

// TestArchiveMikaIsRejected: archiving would hide the workspace's entry point
// while leaving the row in place, which also strands the bootstrap endpoint —
// its lookup skips archived rows but the unique index does not.
func TestArchiveMikaIsRejected(t *testing.T) {
	cleanupMika(t)
	w := createMika(t, map[string]any{
		"runtime_id": handlerTestRuntimeID(t),
		"language":   "en",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	agentID := decodeAgent(t, w).ID

	req := withChatTestWorkspaceCtx(t, withURLParam(
		newRequest("POST", "/api/agents/"+agentID+"/archive", nil), "id", agentID,
	))
	archiveW := httptest.NewRecorder()
	testHandler.ArchiveAgent(archiveW, req)
	if archiveW.Code != http.StatusBadRequest {
		t.Fatalf("archiving a system agent: expected 400, got %d: %s", archiveW.Code, archiveW.Body.String())
	}

	var archivedAt *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT archived_at::text FROM agent WHERE id = $1`, agentID).Scan(&archivedAt); err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if archivedAt != nil {
		t.Fatalf("agent must remain active, got archived_at = %v", *archivedAt)
	}
}

// TestSystemInstructionsFor_OnlySystemAgents guards the blast radius: an
// ordinary agent's payload must be byte-identical to before this feature.
func TestSystemInstructionsFor_OnlySystemAgents(t *testing.T) {
	mika := db.Agent{SystemKey: pgtype.Text{String: service.MikaSystemKey, Valid: true}}
	if systemInstructionsFor(mika) == "" {
		t.Fatal("Mika should expose the product prompt")
	}
	for _, ordinary := range []db.Agent{
		{},
		{SystemKey: pgtype.Text{String: "", Valid: true}},
		{SystemKey: pgtype.Text{String: "agent_builder:abc", Valid: true}},
	} {
		if got := systemInstructionsFor(ordinary); got != "" {
			t.Fatalf("non-Mika agent must expose no system instructions, got %q", got)
		}
	}
}
