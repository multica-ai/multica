package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools-grants-test): tests for the Persona grant MCP tools (JEH-1181) — backend swapped via factory, mock still used here.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/persona"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func setupGrantsTestServer(t *testing.T) (*mcp.Server, *persona.MockBackend) {
	t.Helper()
	srv := mcp.NewServer("test", "0")
	mock := persona.NewMockBackend("ws-test")
	prev := grantsBackendFactory
	grantsBackendFactory = func(_ *cli.APIClient, _ string) persona.Backend { return mock }
	t.Cleanup(func() { grantsBackendFactory = prev })
	registerGrantTools(srv, nil, "ws-test", "user-sara", "member")
	return srv, mock
}

// callGrantTool calls a registered tool and returns the textual result body, the
// IsError flag, and an error from the dispatch path.
func callGrantTool(t *testing.T, srv *mcp.Server, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := srv.Call(context.Background(), name, args)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("call %s: empty content", name)
	}
	return res.Content[0].Text, res.IsError
}

func TestRegisterGrantToolsExposesAllNames(t *testing.T) {
	srv, _ := setupGrantsTestServer(t)
	want := []string{"list_grants", "get_grant", "create_grant", "update_grant", "delete_grant", "evaluate_policy", "list_grant_audit"}
	got := map[string]bool{}
	for _, tool := range srvTools(srv) {
		got[tool.Name] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("missing tool: %s", n)
		}
	}
}

func TestCreateGrantToolReturnsJSON(t *testing.T) {
	srv, _ := setupGrantsTestServer(t)
	body, isErr := callGrantTool(t, srv, "create_grant", map[string]any{
		"subject_type":     "agent",
		"subject_id":       "agent-1",
		"resource_pattern": "issue:*",
		"capability":       "issue.read",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", body)
	}
	var g persona.Grant
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatalf("decode grant: %v\nbody=%s", err, body)
	}
	if g.SubjectType != "agent" || g.Capability != "issue.read" {
		t.Errorf("unexpected grant: %+v", g)
	}
}

func TestCreateGrantValidatesRequiredFields(t *testing.T) {
	srv, _ := setupGrantsTestServer(t)
	body, isErr := callGrantTool(t, srv, "create_grant", map[string]any{
		"subject_type": "agent",
	})
	if !isErr {
		t.Fatalf("expected validation error, got body=%s", body)
	}
}

func TestUpdateGrantViaTool(t *testing.T) {
	srv, mock := setupGrantsTestServer(t)
	g, err := mock.Create(context.Background(), persona.GrantInput{
		SubjectType:     strp("agent"),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, persona.AuditContext{Actor: "seed"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	body, isErr := callGrantTool(t, srv, "update_grant", map[string]any{
		"grant_id":   g.ID,
		"capability": "issue.write",
	})
	if isErr {
		t.Fatalf("update error: %s", body)
	}
	var updated persona.Grant
	if err := json.Unmarshal([]byte(body), &updated); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, body)
	}
	if updated.Capability != "issue.write" {
		t.Errorf("capability not applied: %s", updated.Capability)
	}
}

func TestDeleteGrantSoftRevokes(t *testing.T) {
	srv, mock := setupGrantsTestServer(t)
	g, _ := mock.Create(context.Background(), persona.GrantInput{
		SubjectType:     strp("agent"),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, persona.AuditContext{})
	body, isErr := callGrantTool(t, srv, "delete_grant", map[string]any{"grant_id": g.ID})
	if isErr {
		t.Fatalf("delete error: %s", body)
	}
	got, _ := mock.Get(context.Background(), g.ID, persona.AuditContext{})
	if got.Status != persona.StatusRevoked {
		t.Errorf("status not revoked: %q", got.Status)
	}
}

func TestEvaluatePolicyTool(t *testing.T) {
	srv, mock := setupGrantsTestServer(t)
	_, _ = mock.Create(context.Background(), persona.GrantInput{
		SubjectType:     strp("agent"),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, persona.AuditContext{})

	body, isErr := callGrantTool(t, srv, "evaluate_policy", map[string]any{
		"actor_type": "agent",
		"actor_id":   "rasp",
		"resource":   "issue:JEH-1181",
		"capability": "issue.read",
	})
	if isErr {
		t.Fatalf("evaluate error: %s", body)
	}
	var res persona.EvaluationResult
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, body)
	}
	if res.Decision != persona.EffectAllow {
		t.Errorf("expected allow, got %q (%s)", res.Decision, res.Reason)
	}
}

func TestListGrantsFilters(t *testing.T) {
	srv, mock := setupGrantsTestServer(t)
	_, _ = mock.Create(context.Background(), persona.GrantInput{
		SubjectType: strp("agent"), SubjectID: strp("a1"),
		ResourcePattern: strp("issue:*"), Capability: strp("issue.read"),
	}, persona.AuditContext{})
	_, _ = mock.Create(context.Background(), persona.GrantInput{
		SubjectType: strp("member"), SubjectID: strp("m1"),
		ResourcePattern: strp("agent:*"), Capability: strp("agent.run"),
	}, persona.AuditContext{})

	body, _ := callGrantTool(t, srv, "list_grants", map[string]any{"subject_type": "agent"})
	var res struct {
		Grants []persona.Grant `json:"grants"`
		Count  int             `json:"count"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if res.Count != 1 || res.Grants[0].SubjectType != "agent" {
		t.Errorf("filter broken: %+v", res)
	}
}

func TestAuditLogToolSurfacesEntries(t *testing.T) {
	srv, _ := setupGrantsTestServer(t)
	_, _ = callGrantTool(t, srv, "create_grant", map[string]any{
		"subject_type":     "agent",
		"subject_id":       "agent-1",
		"resource_pattern": "issue:*",
		"capability":       "issue.read",
	})
	body, _ := callGrantTool(t, srv, "list_grant_audit", map[string]any{})
	if !strings.Contains(body, "grant.create") {
		t.Errorf("audit log should include create entry; body=%s", body)
	}
	if !strings.Contains(body, "user-sara") {
		t.Errorf("audit log should record actor user-sara; body=%s", body)
	}
	if !strings.Contains(body, mcpProcessSessionID) {
		t.Errorf("audit log should record session id %s", mcpProcessSessionID)
	}
}

func TestParseGrantInputRejectsBadTimestamps(t *testing.T) {
	srv, _ := setupGrantsTestServer(t)
	body, isErr := callGrantTool(t, srv, "create_grant", map[string]any{
		"subject_type":     "agent",
		"subject_id":       "agent-1",
		"resource_pattern": "issue:*",
		"capability":       "issue.read",
		"starts_at":        "not-a-time",
	})
	if !isErr || !strings.Contains(body, "starts_at") {
		t.Errorf("expected timestamp error, got isErr=%v body=%s", isErr, body)
	}
}

// srvTools returns the registered tools — uses an unexported field via a
// helper in this same package's MCP package for stable test introspection.
// We only need the names, so we re-discover them by replaying tools/list.
func srvTools(srv *mcp.Server) []mcp.Tool {
	// Hack: srv exposes no public accessor, but we can list-via-call by
	// reflecting on the registered handlers indirectly. Simplest path:
	// re-register zero-arg probes is overkill — instead trust the
	// dispatch behaviour: try each expected name with empty args and see
	// if it returns "unknown tool".
	expected := []string{"list_grants", "get_grant", "create_grant", "update_grant", "delete_grant", "evaluate_policy", "list_grant_audit"}
	out := make([]mcp.Tool, 0, len(expected))
	for _, name := range expected {
		res, _ := srv.Call(context.Background(), name, map[string]any{})
		if len(res.Content) == 0 {
			continue
		}
		// "unknown tool: X" is the only response from Call when not registered.
		if strings.HasPrefix(res.Content[0].Text, "unknown tool:") {
			continue
		}
		out = append(out, mcp.Tool{Name: name})
	}
	return out
}

// strp is duplicated from grants_test.go to keep this file self-contained
// since it lives in a different package (`main` here vs `persona` there).
func strp(s string) *string { return &s }
