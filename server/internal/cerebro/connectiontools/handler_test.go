package connectiontools

// FIR-2273 — gate tests for the agent-only connection-tools entry point. This is
// the surface that fronts the secrets box, so the security-critical behaviour is
// verified here directly: only a server-stamped task token is admitted, the
// agent's workspace must match the token workspace, every lookup failure fails
// CLOSED, and a non-agent caller is rejected before any store is touched.
//
// allowedTools / Call's dispatch resolve the per-agent gate and dispatch the
// HTTP call through pgx-backed stores; those need a database and are covered by
// the toolpolicy ConnectionEndpointEffective tests + an integration check, not
// here. These tests pin the parts reachable without a DB — which is precisely
// the auth/identity gate.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

const (
	testAgentID = "11111111-1111-1111-1111-111111111111"
	testWsID    = "22222222-2222-2222-2222-222222222222"
	testRunID   = "33333333-3333-3333-3333-333333333333"
	testOwnerID = "44444444-4444-4444-4444-444444444444"
	otherWsID   = "55555555-5555-5555-5555-555555555555"
)

// fakeResolver is a test AgentResolver: it returns a fixed identity, or an error
// when err is set (to exercise the fail-closed path).
type fakeResolver struct {
	wsID, runID, ownerID pgtype.UUID
	err                  error
	calledWith           pgtype.UUID
}

func (f *fakeResolver) ResolveAgent(ctx context.Context, agentID pgtype.UUID) (pgtype.UUID, pgtype.UUID, pgtype.UUID, error) {
	f.calledWith = agentID
	if f.err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, f.err
	}
	return f.wsID, f.runID, f.ownerID, nil
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

// agentToken sets the headers the auth middleware stamps for a real mat_ task
// token. A client can never set X-Actor-Source itself — the middleware strips it
// — so a request carrying "task_token" here models an authenticated agent.
func agentToken(r *http.Request, agentID, wsID string) {
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Workspace-ID", wsID)
	r.Header.Set("X-User-ID", testOwnerID)
}

func newGateHandler(t *testing.T, res AgentResolver) *Handler {
	t.Helper()
	// pool/conns/policy/flags stay nil: every test here returns before those are
	// touched. If a path ever reaches a nil store the test panics — a useful
	// tripwire that the gate let something through it should have rejected.
	return &Handler{agents: res}
}

func decodeErr(t *testing.T, body string) string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("response is not a JSON error object: %q", body)
	}
	return m["error"]
}

func TestRequireAgent_RejectsNonAgent(t *testing.T) {
	cases := []struct {
		name      string
		actorSrc  string // "" = header absent
	}{
		{"no actor source (human PAT / public)", ""},
		{"member actor source", "member"},
		{"cloud_pat actor source", "cloud_pat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newGateHandler(t, &fakeResolver{})
			r := httptest.NewRequest(http.MethodGet, "/api/cerebro/connection-tools", nil)
			if tc.actorSrc != "" {
				r.Header.Set("X-Actor-Source", tc.actorSrc)
			}
			r.Header.Set("X-Agent-ID", testAgentID)
			r.Header.Set("X-Workspace-ID", testWsID)
			w := httptest.NewRecorder()

			h.List(w, r)

			if w.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d (%s)", w.Code, w.Body.String())
			}
			if msg := decodeErr(t, w.Body.String()); !strings.Contains(msg, "agent-only") {
				t.Fatalf("want agent-only error, got %q", msg)
			}
		})
	}
}

func TestRequireAgent_BadIdentityHeaders(t *testing.T) {
	cases := []struct {
		name        string
		agentID, ws string
		wantField   string
	}{
		{"garbage agent id", "not-a-uuid", testWsID, "agent_id"},
		{"empty agent id", "", testWsID, "agent_id"},
		{"garbage workspace id", testAgentID, "nope", "workspace_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newGateHandler(t, &fakeResolver{})
			r := httptest.NewRequest(http.MethodGet, "/api/cerebro/connection-tools", nil)
			r.Header.Set("X-Actor-Source", "task_token")
			r.Header.Set("X-Agent-ID", tc.agentID)
			r.Header.Set("X-Workspace-ID", tc.ws)
			w := httptest.NewRecorder()

			h.List(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
			}
			if msg := decodeErr(t, w.Body.String()); !strings.Contains(msg, tc.wantField) {
				t.Fatalf("want error mentioning %q, got %q", tc.wantField, msg)
			}
		})
	}
}

func TestRequireAgent_FailsClosedOnResolveError(t *testing.T) {
	h := newGateHandler(t, &fakeResolver{err: errors.New("agent not found")})
	r := httptest.NewRequest(http.MethodGet, "/api/cerebro/connection-tools", nil)
	agentToken(r, testAgentID, testWsID)
	w := httptest.NewRecorder()

	h.List(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 on resolve error (fail closed), got %d (%s)", w.Code, w.Body.String())
	}
}

func TestRequireAgent_RejectsWorkspaceMismatch(t *testing.T) {
	// Token says workspace A; the agent row resolves to workspace B. A cross-
	// workspace token must never reach another workspace's connections.
	res := &fakeResolver{
		wsID:    mustUUID(t, otherWsID),
		runID:   mustUUID(t, testRunID),
		ownerID: mustUUID(t, testOwnerID),
	}
	h := newGateHandler(t, res)
	r := httptest.NewRequest(http.MethodGet, "/api/cerebro/connection-tools", nil)
	agentToken(r, testAgentID, testWsID) // token workspace = testWsID
	w := httptest.NewRecorder()

	h.List(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 on workspace mismatch, got %d (%s)", w.Code, w.Body.String())
	}
	if msg := decodeErr(t, w.Body.String()); !strings.Contains(msg, "workspace") {
		t.Fatalf("want workspace error, got %q", msg)
	}
}

// A well-formed agent token whose workspace matches passes the gate; with no
// resolver wired (feature off) it must return an empty tool list and 200, never
// break the caller's MCP loop.
func TestList_AllowedAgent_FlagOff_ReturnsEmpty(t *testing.T) {
	res := &fakeResolver{
		wsID:    mustUUID(t, testWsID),
		runID:   mustUUID(t, testRunID),
		ownerID: mustUUID(t, testOwnerID),
	}
	h := newGateHandler(t, res)
	r := httptest.NewRequest(http.MethodGet, "/api/cerebro/connection-tools", nil)
	agentToken(r, testAgentID, testWsID)
	w := httptest.NewRecorder()

	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(resp.Tools) != 0 {
		t.Fatalf("want empty tool list with flag off, got %d tools", len(resp.Tools))
	}
	if !res.calledWith.Valid {
		t.Fatalf("resolver should have been called with the agent id")
	}
}

func TestCall_NonAgent_Forbidden(t *testing.T) {
	h := newGateHandler(t, &fakeResolver{})
	r := httptest.NewRequest(http.MethodPost, "/api/cerebro/connection-tools/call",
		strings.NewReader(`{"tool":"infisical_admin__get_secrets","arguments":{}}`))
	// no task_token header
	w := httptest.NewRecorder()

	h.Call(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for non-agent call, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestCall_EmptyTool_BadRequest(t *testing.T) {
	res := &fakeResolver{
		wsID:    mustUUID(t, testWsID),
		runID:   mustUUID(t, testRunID),
		ownerID: mustUUID(t, testOwnerID),
	}
	h := newGateHandler(t, res)
	r := httptest.NewRequest(http.MethodPost, "/api/cerebro/connection-tools/call",
		strings.NewReader(`{"tool":"   ","arguments":{}}`))
	agentToken(r, testAgentID, testWsID)
	w := httptest.NewRecorder()

	h.Call(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty tool, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestCall_BadBody_BadRequest(t *testing.T) {
	res := &fakeResolver{
		wsID:    mustUUID(t, testWsID),
		runID:   mustUUID(t, testRunID),
		ownerID: mustUUID(t, testOwnerID),
	}
	h := newGateHandler(t, res)
	r := httptest.NewRequest(http.MethodPost, "/api/cerebro/connection-tools/call",
		strings.NewReader(`{not json`))
	agentToken(r, testAgentID, testWsID)
	w := httptest.NewRecorder()

	h.Call(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad body, got %d (%s)", w.Code, w.Body.String())
	}
}

// A Handler with no resolver wired (feature off) resolves to an empty allowed set
// rather than panicking — the default-OFF, fail-open-to-empty contract.
func TestAllowedTools_NilResolverIsEmpty(t *testing.T) {
	h := &Handler{} // resolver nil
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := h.allowedTools(r, agentIdentity{workspaceID: mustUUID(t, testWsID), agentID: mustUUID(t, testAgentID)}); len(got) != 0 {
		t.Fatalf("allowedTools must be empty when the resolver is nil (default OFF), got %d", len(got))
	}
}
