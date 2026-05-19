package agentpass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Integration tests for the agent-pass admin API. Requires a reachable
// Postgres at $DATABASE_URL — the suite skips cleanly when no DB is
// available, matching the cerebro/sharetoken precedent.
//
// The key assertion for JEH-1731 is that a child pass that widens its
// parent's scope is rejected with HTTP 422 (the production callsite for
// ValidateChildScope), not just at the unit level. The unit-level proof
// already lives in scope_test.go.

const (
	agentPassTestWorkspaceSlug = "agent-pass-handler-tests"
	agentPassTestUserEmail     = "agent-pass-handler-tests@multica.ai"
)

var (
	apHandlerPool      *pgxpool.Pool
	apHandlerWorkspace string
	apHandlerUser      string
	apHandlerMember    db.Member
	apHandlerAgent     db.Agent
	apHandlerIssue     db.Issue
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica_firtal_cerebro_630?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("agentpass handler: skipping tests, no DB: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("agentpass handler: skipping tests, DB unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	apHandlerPool = pool
	if err := setupHandlerFixture(ctx, pool); err != nil {
		fmt.Printf("agentpass handler: setup failed: %v\n", err)
		_ = teardownHandlerFixture(context.Background(), pool)
		pool.Close()
		os.Exit(1)
	}
	code := m.Run()
	_ = teardownHandlerFixture(context.Background(), pool)
	pool.Close()
	os.Exit(code)
}

func setupHandlerFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := teardownHandlerFixture(ctx, pool); err != nil {
		return err
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"AgentPass Handler Tests", agentPassTestUserEmail,
	).Scan(&apHandlerUser); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		"AgentPass Handler Tests", agentPassTestWorkspaceSlug, "agent-pass handler tests", "APH",
	).Scan(&apHandlerWorkspace); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
		RETURNING id, workspace_id, user_id, role, created_at, budget_enforcement_enabled`,
		apHandlerWorkspace, apHandlerUser,
	).Scan(&apHandlerMember.ID, &apHandlerMember.WorkspaceID, &apHandlerMember.UserID, &apHandlerMember.Role, &apHandlerMember.CreatedAt, &apHandlerMember.BudgetEnforcementEnabled); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	q := db.New(pool)
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: parseUUIDOrPanic(apHandlerWorkspace),
		Title:       "Agent-pass handler test issue",
		Status:      "todo",
		Priority:    "medium",
		Number:      1,
		CreatorType: "member",
		CreatorID:   parseUUIDOrPanic(apHandlerUser),
	})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}
	apHandlerIssue = issue

	// An agent row is needed because cerebro_agent_pass.agent_id has an
	// FK to agent(id). The agent table requires a runtime row. Both are
	// inserted via raw SQL to avoid coupling the test to upstream sqlc
	// signatures that grow new fields each sync.
	var runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata)
		VALUES ($1, $2, 'local', 'claude', 'offline', '', '{}'::jsonb)
		RETURNING id`,
		apHandlerWorkspace, "Agent-pass test runtime",
	).Scan(&runtimeID); err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}

	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, visibility, status, max_concurrent_tasks, description, instructions, runtime_id)
		VALUES ($1, $2, 'local', '{}'::jsonb, 'workspace', 'idle', 1, '', '', $3)
		RETURNING id`,
		apHandlerWorkspace, "Agent-pass test agent", runtimeID,
	).Scan(&agentID); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	apHandlerAgent = db.Agent{ID: agentID, WorkspaceID: apHandlerMember.WorkspaceID, Name: "Agent-pass test agent"}

	return nil
}

func teardownHandlerFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, agentPassTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, agentPassTestUserEmail); err != nil {
		return err
	}
	return nil
}

func parseUUIDOrPanic(s string) pgtype.UUID {
	u, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

func newHandlerForTest() *Handler {
	q := cerebrodb.New(apHandlerPool)
	svc, err := New(q, nil)
	if err != nil {
		panic(err)
	}
	return NewHandler(q, svc)
}

func memberRequest(t testing.TB, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", util.UUIDToString(apHandlerMember.UserID))
	ctx := middleware.SetMemberContext(req.Context(), apHandlerWorkspace, apHandlerMember)
	return req.WithContext(ctx)
}

func withChiParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestHandler_Create_ScopeWidening_Returns422 is the headline acceptance
// test for JEH-1731: a child pass that widens its parent's scope must
// fail at the HTTP boundary with 422, not just at the unit level.
//
// Builds a parent pass with allowed_tools=["read"], then POSTs a child
// pointing at the parent with allowed_tools=["read","write"]. The handler
// must reject this BEFORE inserting the child.
func TestHandler_Create_ScopeWidening_Returns422(t *testing.T) {
	h := newHandlerForTest()

	// Seed a parent pass directly so the test does not depend on a
	// successful first POST. Scope restricts the parent to read.
	q := cerebrodb.New(apHandlerPool)
	parent, err := q.CreateCerebroAgentPass(context.Background(), cerebrodb.CreateCerebroAgentPassParams{
		WorkspaceID: apHandlerMember.WorkspaceID,
		IssuerType:  IssuerMember,
		IssuerID:    apHandlerMember.UserID,
		AgentID:     apHandlerAgent.ID,
		IssueID:     apHandlerIssue.ID,
		Scope:       []byte(`{"allowed_tools":["read"]}`),
	})
	if err != nil {
		t.Fatalf("seed parent pass: %v", err)
	}
	// Clean up the parent at the end so other tests can re-issue against
	// the same (agent, issue) without colliding with the unique partial
	// index.
	t.Cleanup(func() {
		_, _ = apHandlerPool.Exec(context.Background(), `DELETE FROM cerebro_agent_pass WHERE id = $1`, parent.ID)
	})

	body := createRequest{
		AgentID:      util.UUIDToString(apHandlerAgent.ID),
		IssueID:      util.UUIDToString(apHandlerIssue.ID),
		ParentPassID: util.UUIDToString(parent.ID),
		Scope:        json.RawMessage(`{"allowed_tools":["read","write"]}`),
	}

	req := memberRequest(t, http.MethodPost, "/api/cerebro/agent-passes", body)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for scope widening, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "scope_widening") {
		t.Fatalf("response body should mention scope_widening, got %s", w.Body.String())
	}

	// The widened child must NOT have been persisted.
	rows, err := q.ListCerebroAgentPassesForWorkspace(context.Background(), apHandlerMember.WorkspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range rows {
		if row.ParentPassID.Valid && uuidEqual(row.ParentPassID, parent.ID) {
			t.Fatalf("widened child pass leaked into the database: id=%s", util.UUIDToString(row.ID))
		}
	}
}

// TestHandler_Create_ValidNarrowing_Returns201 proves the inverse path:
// a child that narrows the parent is accepted and persisted. Without
// this complement, the 422 test alone could be satisfied by a handler
// that rejects every parent_pass_id.
func TestHandler_Create_ValidNarrowing_Returns201(t *testing.T) {
	h := newHandlerForTest()
	q := cerebrodb.New(apHandlerPool)

	parent, err := q.CreateCerebroAgentPass(context.Background(), cerebrodb.CreateCerebroAgentPassParams{
		WorkspaceID: apHandlerMember.WorkspaceID,
		IssuerType:  IssuerMember,
		IssuerID:    apHandlerMember.UserID,
		AgentID:     apHandlerAgent.ID,
		IssueID:     apHandlerIssue.ID,
		Scope:       []byte(`{"allowed_tools":["read","write"]}`),
	})
	if err != nil {
		t.Fatalf("seed parent pass: %v", err)
	}
	t.Cleanup(func() {
		_, _ = apHandlerPool.Exec(context.Background(), `DELETE FROM cerebro_agent_pass WHERE id = $1`, parent.ID)
	})

	// Use a second issue so the unique partial index does not flag the
	// child against the existing active parent on the same (agent, issue).
	upstreamQ := db.New(apHandlerPool)
	issue2, err := upstreamQ.CreateIssue(context.Background(), db.CreateIssueParams{
		WorkspaceID: apHandlerMember.WorkspaceID,
		Title:       "Agent-pass narrowing test child issue",
		Status:      "todo",
		Priority:    "medium",
		Number:      2,
		CreatorType: "member",
		CreatorID:   apHandlerMember.UserID,
	})
	if err != nil {
		t.Fatalf("create child issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = apHandlerPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue2.ID)
	})

	body := createRequest{
		AgentID:      util.UUIDToString(apHandlerAgent.ID),
		IssueID:      util.UUIDToString(issue2.ID),
		ParentPassID: util.UUIDToString(parent.ID),
		Scope:        json.RawMessage(`{"allowed_tools":["read"]}`),
	}

	req := memberRequest(t, http.MethodPost, "/api/cerebro/agent-passes", body)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid narrowing, got %d body=%s", w.Code, w.Body.String())
	}

	var resp passResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = apHandlerPool.Exec(context.Background(), `DELETE FROM cerebro_agent_pass WHERE id = $1`, parseUUIDOrPanic(resp.ID))
	})

	if resp.Status != StatusActive {
		t.Fatalf("expected active status, got %s", resp.Status)
	}
	if resp.ParentPassID != util.UUIDToString(parent.ID) {
		t.Fatalf("expected parent_pass_id=%s, got %s", util.UUIDToString(parent.ID), resp.ParentPassID)
	}
}

// TestHandler_Revoke_FlipsStatusToRevoked confirms the lifecycle path
// the admin UI relies on: revoke marks the row revoked and records the
// caller's identity.
func TestHandler_Revoke_FlipsStatusToRevoked(t *testing.T) {
	h := newHandlerForTest()
	q := cerebrodb.New(apHandlerPool)

	pass, err := q.CreateCerebroAgentPass(context.Background(), cerebrodb.CreateCerebroAgentPassParams{
		WorkspaceID: apHandlerMember.WorkspaceID,
		IssuerType:  IssuerMember,
		IssuerID:    apHandlerMember.UserID,
		AgentID:     apHandlerAgent.ID,
		IssueID:     apHandlerIssue.ID,
		Scope:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed pass: %v", err)
	}
	t.Cleanup(func() {
		_, _ = apHandlerPool.Exec(context.Background(), `DELETE FROM cerebro_agent_pass WHERE id = $1`, pass.ID)
	})

	req := memberRequest(t, http.MethodDelete, "/api/cerebro/agent-passes/"+util.UUIDToString(pass.ID), revokeRequest{Reason: "test cleanup"})
	req = withChiParam(req, "passId", util.UUIDToString(pass.ID))
	w := httptest.NewRecorder()
	h.Revoke(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from revoke, got %d body=%s", w.Code, w.Body.String())
	}

	var resp passResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != StatusRevoked {
		t.Fatalf("expected revoked status, got %s", resp.Status)
	}
	if resp.RevokeReason != "test cleanup" {
		t.Fatalf("expected revoke reason to round-trip, got %q", resp.RevokeReason)
	}
}

// TestHandler_List_NonAdmin_Returns403 protects the threat model: only
// owner/admin should see the workspace's pass roster.
func TestHandler_List_NonAdmin_Returns403(t *testing.T) {
	h := newHandlerForTest()

	// Create a request with the same member but role downgraded.
	memberCopy := apHandlerMember
	memberCopy.Role = "member"
	var buf bytes.Buffer
	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/agent-passes", &buf)
	req.Header.Set("X-User-ID", util.UUIDToString(memberCopy.UserID))
	ctx := middleware.SetMemberContext(req.Context(), apHandlerWorkspace, memberCopy)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d body=%s", w.Code, w.Body.String())
	}
}
