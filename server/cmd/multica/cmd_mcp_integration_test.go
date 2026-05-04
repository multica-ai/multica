package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// TestJEH420_EndToEnd reproduces the bug described in the issue end-to-end:
// parent attaches, subagent attaches with set_active=false and then
// completes its own session — parent's ambient pointer must be intact.
//
// Unlike the unit tests in cmd_mcp_test.go that drive mcpSessionState
// directly, this test exercises the full registered-tool dispatch path:
// it creates a real *mcp.Server, calls registerTools with a real cli.APIClient
// pointing at an httptest mock of the Multica API, and invokes attach_session
// / complete_work / get_me through Server.Call exactly as Claude would.
func TestJEH420_EndToEnd(t *testing.T) {
	mock := newMultiicaMock(t)
	defer mock.Close()

	srv := mcp.NewServer("multica", "test")
	client := cli.NewAPIClient(mock.URL, "ws-test", "token-test")
	var sessionState mcpSessionState
	registerTools(srv, client, &sessionState, "ws-test", "proj-test", "" /* gitRoot */)

	ctx := context.Background()

	// Step 1: Parent attaches (set_active default = true).
	parentRes := callTool(t, srv, ctx, "attach_session", map[string]any{
		"issue_id": "issue-parent",
	})
	parentWS := mock.LastCreatedSessionID()
	if !strings.Contains(parentRes, parentWS) {
		t.Fatalf("parent attach result missing work_session_id: %s", parentRes)
	}

	// Step 2: get_me reflects parent as ambient.
	if got := ambientFromGetMe(t, srv, ctx); got != parentWS {
		t.Fatalf("after parent attach: get_me ambient = %q, want %q", got, parentWS)
	}

	// Step 3: Subagent attaches with set_active=false — must NOT take over ambient.
	childRes := callTool(t, srv, ctx, "attach_session", map[string]any{
		"issue_id":   "issue-child",
		"set_active": false,
	})
	childWS := mock.LastCreatedSessionID()
	if childWS == parentWS {
		t.Fatalf("mock returned same session id for parent and child — fix the mock")
	}
	if !strings.Contains(childRes, "ambient unchanged") {
		t.Fatalf("subagent attach should advertise ambient unchanged, got: %s", childRes)
	}

	// THIS IS THE JEH-420 INVARIANT: parent's ambient must survive.
	if got := ambientFromGetMe(t, srv, ctx); got != parentWS {
		t.Fatalf("after subagent attach: get_me ambient = %q, want %q (subagent clobbered parent — JEH-429 regression)", got, parentWS)
	}

	// Step 4: Subagent completes its own session.
	if _, err := srv.Call(ctx, "complete_work", map[string]any{
		"work_session_id": childWS,
		"summary":         "child done",
	}); err != nil {
		t.Fatalf("subagent complete_work: %v", err)
	}

	// THIS IS THE OTHER JEH-420 INVARIANT: parent's ambient still there.
	if got := ambientFromGetMe(t, srv, ctx); got != parentWS {
		t.Fatalf("after subagent complete: get_me ambient = %q, want %q (subagent's complete erased parent — JEH-429 regression)", got, parentWS)
	}

	// Step 5: Parent reports activity using its captured work_session_id.
	if _, err := srv.Call(ctx, "report_activity", map[string]any{
		"work_session_id": parentWS,
		"type":            "decision",
		"summary":         "parent decision after subagent finished",
	}); err != nil {
		t.Fatalf("parent report_activity: %v", err)
	}
	// The activity must land on parent's session, not child's.
	if !mock.LastMessageOnSession(parentWS) {
		t.Fatalf("parent's report_activity did not land on parent's session")
	}

	// Step 6: Parent completes — now ambient should clear.
	if _, err := srv.Call(ctx, "complete_work", map[string]any{
		"work_session_id": parentWS,
		"summary":         "parent done",
	}); err != nil {
		t.Fatalf("parent complete_work: %v", err)
	}
	if got := ambientFromGetMe(t, srv, ctx); got != "" {
		t.Fatalf("after parent complete: get_me ambient = %q, want empty", got)
	}
}

// TestExplicitWorkSessionID_Required ensures the schema actually rejects calls
// missing work_session_id — the contract Claude reads from tool descriptions
// is also enforced by the server.
func TestExplicitWorkSessionID_Required(t *testing.T) {
	mock := newMultiicaMock(t)
	defer mock.Close()

	srv := mcp.NewServer("multica", "test")
	client := cli.NewAPIClient(mock.URL, "ws-test", "token-test")
	var sessionState mcpSessionState
	registerTools(srv, client, &sessionState, "ws-test", "proj-test", "")

	ctx := context.Background()

	// report_activity without work_session_id must error.
	res, err := srv.Call(ctx, "report_activity", map[string]any{
		"type":    "note",
		"summary": "should fail",
	})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("report_activity without work_session_id must return IsError=true, got %#v", res)
	}

	// complete_work without work_session_id must error.
	res, err = srv.Call(ctx, "complete_work", map[string]any{
		"summary": "should fail",
	})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("complete_work without work_session_id must return IsError=true, got %#v", res)
	}
}

// callTool invokes a tool by name and returns the concatenated text content
// of the result (or fails the test on error / IsError).
func callTool(t *testing.T, srv *mcp.Server, ctx context.Context, name string, args map[string]any) string {
	t.Helper()
	res, err := srv.Call(ctx, name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool reported error: %s", name, textOf(res))
	}
	return textOf(res)
}

func textOf(res mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// ambientFromGetMe parses get_me's JSON output and returns
// active_session.work_session_id (or "" if none).
func ambientFromGetMe(t *testing.T, srv *mcp.Server, ctx context.Context) string {
	t.Helper()
	res, err := srv.Call(ctx, "get_me", map[string]any{})
	if err != nil {
		t.Fatalf("get_me: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_me: tool reported error: %s", textOf(res))
	}
	var payload struct {
		ActiveSession *struct {
			WorkSessionID string `json:"work_session_id"`
		} `json:"active_session"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &payload); err != nil {
		t.Fatalf("get_me JSON: %v\n%s", err, textOf(res))
	}
	if payload.ActiveSession == nil {
		return ""
	}
	return payload.ActiveSession.WorkSessionID
}

// multicaMock is a minimal httptest server that fakes the subset of the
// Multica API the MCP tools call.
type multicaMock struct {
	*httptest.Server

	mu              sync.Mutex
	sessionCounter  atomic.Int64
	lastCreatedID   string
	messagesBySess  map[string][]map[string]any
	completedBySess map[string]bool
}

func newMultiicaMock(t *testing.T) *multicaMock {
	t.Helper()
	m := &multicaMock{
		messagesBySess:  make(map[string][]map[string]any),
		completedBySess: make(map[string]bool),
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *multicaMock) LastCreatedSessionID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCreatedID
}

// LastMessageOnSession returns true if the most recent /messages POST landed
// on the given session id.
func (m *multicaMock) LastMessageOnSession(sessID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := m.messagesBySess[sessID]
	return len(msgs) > 0
}

func (m *multicaMock) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/me":
		writeJSON(w, map[string]any{"id": "user-test", "email": "test@example.com"})

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/projects/"):
		writeJSON(w, map[string]any{"title": "Test Project"})

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/"):
		// e.g. /api/issues/issue-parent
		id := strings.TrimPrefix(r.URL.Path, "/api/issues/")
		writeJSON(w, map[string]any{"id": id, "identifier": "TEST-1", "title": "Test issue"})

	case r.Method == http.MethodGet && r.URL.Path == "/api/work-sessions/active":
		writeJSON(w, map[string]any{"session": nil})

	case r.Method == http.MethodPost && r.URL.Path == "/api/work-sessions":
		n := m.sessionCounter.Add(1)
		id := fmt.Sprintf("ws-%d", n)
		m.mu.Lock()
		m.lastCreatedID = id
		m.mu.Unlock()
		writeJSON(w, map[string]any{"id": id})

	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/work-sessions/") && strings.HasSuffix(r.URL.Path, "/messages"):
		sessID := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/messages"), "/api/work-sessions/")
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.messagesBySess[sessID] = append(m.messagesBySess[sessID], body.Messages...)
		m.mu.Unlock()
		writeJSON(w, map[string]any{})

	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/work-sessions/") && strings.HasSuffix(r.URL.Path, "/complete"):
		sessID := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/complete"), "/api/work-sessions/")
		m.mu.Lock()
		m.completedBySess[sessID] = true
		m.mu.Unlock()
		writeJSON(w, map[string]any{})

	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/work-sessions/") && strings.HasSuffix(r.URL.Path, "/name"):
		writeJSON(w, map[string]any{})

	default:
		http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
