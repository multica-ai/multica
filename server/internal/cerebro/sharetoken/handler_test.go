package sharetoken

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testWorkspaceSlug = "sharetoken-tests"
	testUserEmail     = "sharetoken-tests@multica.ai"
)

var (
	testPool      *pgxpool.Pool
	testWorkspace string
	testUser      string
	testMember    db.Member
	testIssue     db.Issue
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica_firtal_cerebro_630?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("sharetoken: skipping tests, no DB: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("sharetoken: skipping tests, DB unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	if err := setupFixture(ctx, pool); err != nil {
		fmt.Printf("sharetoken: setup failed: %v\n", err)
		_ = teardownFixture(context.Background(), pool)
		pool.Close()
		os.Exit(1)
	}
	code := m.Run()
	_ = teardownFixture(context.Background(), pool)
	pool.Close()
	os.Exit(code)
}

func setupFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := teardownFixture(ctx, pool); err != nil {
		return err
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"ShareToken Tests", testUserEmail,
	).Scan(&testUser); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		"ShareToken Tests", testWorkspaceSlug, "share-token tests", "STT",
	).Scan(&testWorkspace); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
		RETURNING id, workspace_id, user_id, role, created_at, budget_enforcement_enabled`,
		testWorkspace, testUser,
	).Scan(&testMember.ID, &testMember.WorkspaceID, &testMember.UserID, &testMember.Role, &testMember.CreatedAt, &testMember.BudgetEnforcementEnabled); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	q := db.New(pool)
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: parseUUIDOrPanic(testWorkspace),
		Title:       "Share-token test issue",
		Status:      "todo",
		Priority:    "medium",
		Number:      1,
		CreatorType: "member",
		CreatorID:   parseUUIDOrPanic(testUser),
	})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}
	testIssue = issue
	return nil
}

func teardownFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, testUserEmail); err != nil {
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

// stubPersona is a PersonaClient that records every call. Lets tests
// verify the share-token handler synchronises grants without standing
// up a real persona service.
type stubPersona struct {
	mu              sync.Mutex
	added           []addCall
	deleted         []string
	nextGrantID     string
	failAdd, failDel bool
}

type addCall struct {
	Action, Kind, Pattern string
}

func (s *stubPersona) AddAnonymousGrant(_ context.Context, action, kind, pattern string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAdd {
		return "", fmt.Errorf("stub: forced add failure")
	}
	s.added = append(s.added, addCall{Action: action, Kind: kind, Pattern: pattern})
	if s.nextGrantID == "" {
		return "grant-stub", nil
	}
	return s.nextGrantID, nil
}

func (s *stubPersona) DeleteAnonymousGrant(_ context.Context, grantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDel {
		return fmt.Errorf("stub: forced delete failure")
	}
	s.deleted = append(s.deleted, grantID)
	return nil
}

func newTestHandler(stub PersonaClient) *Handler {
	return NewHandlerWithPersona(cerebrodb.New(testPool), db.New(testPool), stub)
}

func memberRequest(t testing.TB, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", util.UUIDToString(testMember.UserID))
	ctx := middleware.SetMemberContext(req.Context(), testWorkspace, testMember)
	return req.WithContext(ctx)
}

func withChi(req *http.Request, key, value string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestShareToken_MintAndPublicGet is the cerebro integration test required
// by JEH-1076 AC #4 + #6: a workspace member mints a public link, the
// handler synchronises an anonymous grant on the persona side, and an
// unauthenticated visitor reaches the gated resource via the public
// route. Other resources stay invisible.
func TestShareToken_MintAndPublicGet(t *testing.T) {
	// Stub persona for the mint call. Returns a deterministic grant id.
	stub := &stubPersona{nextGrantID: "grant-test-1"}
	h := newTestHandler(stub)

	// 1. Mint the share token.
	mintReq := memberRequest(t, http.MethodPost,
		"/api/cerebro/issues/"+util.UUIDToString(testIssue.ID)+"/share-tokens",
		map[string]any{"action": "read"})
	mintReq = withChi(mintReq, "id", util.UUIDToString(testIssue.ID))
	mintRR := httptest.NewRecorder()
	h.Create(mintRR, mintReq)
	if mintRR.Code != http.StatusCreated {
		t.Fatalf("mint status = %d, want 201; body = %s", mintRR.Code, mintRR.Body.String())
	}
	var mint CreateResponse
	if err := json.Unmarshal(mintRR.Body.Bytes(), &mint); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}
	if mint.Token == "" || mint.PersonaGrantID != "grant-test-1" {
		t.Fatalf("unexpected mint response: %+v", mint)
	}

	// Persona contract: action / kind / pattern must match what the
	// public-route /v1/check will ask. Persona's grant resolver matches
	// resource_pattern against resource_id exactly, so a mis-shaped
	// pattern silently denies forever.
	stub.mu.Lock()
	if len(stub.added) != 1 {
		t.Fatalf("expected 1 anonymous-grant add, got %d", len(stub.added))
	}
	got := stub.added[0]
	stub.mu.Unlock()
	wantPattern := util.UUIDToString(testIssue.ID)
	if got.Action != "read" || got.Kind != IssueResourceKind || got.Pattern != wantPattern {
		t.Fatalf("anonymous-grant args = %+v, want read/%s/%s", got, IssueResourceKind, wantPattern)
	}

	// 2. Mock persona's /v1/check for the unauth path. Allow only the
	// granted resource; reject everything else fail-closed.
	personaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/check" {
			http.NotFound(w, r)
			return
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("public path sent Authorization header = %q (must be empty for anonymous)", auth)
		}
		var body struct {
			Action           string `json:"action"`
			Resource         struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"resource"`
			AnonymousContext string `json:"anonymous_context"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		allowed := body.Action == "read" &&
			body.Resource.Kind == IssueResourceKind &&
			body.Resource.ID == wantPattern
		if allowed && body.AnonymousContext == "" {
			t.Errorf("anonymous_context missing on allow path")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"allowed":     allowed,
			"decision_id": "decision-test",
		})
	}))
	t.Cleanup(personaSrv.Close)
	t.Setenv("MULTICA_PERSONA_URL", personaSrv.URL)

	// 3. Allow path — the mint's token reaches the issue.
	allowReq := httptest.NewRequest(http.MethodGet, "/api/cerebro/public/share/"+mint.Token, nil)
	allowReq = withChi(allowReq, "token", mint.Token)
	allowRR := httptest.NewRecorder()
	h.PublicGet(allowRR, allowReq)
	if allowRR.Code != http.StatusOK {
		t.Fatalf("public GET status = %d, want 200; body = %s", allowRR.Code, allowRR.Body.String())
	}
	var view publicIssueView
	if err := json.Unmarshal(allowRR.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode public view: %v", err)
	}
	if view.Title != testIssue.Title {
		t.Errorf("public view title = %q, want %q", view.Title, testIssue.Title)
	}

	// 4. Unknown / revoked tokens always 404 — never leak existence.
	missingReq := httptest.NewRequest(http.MethodGet, "/api/cerebro/public/share/bogus", nil)
	missingReq = withChi(missingReq, "token", "bogus")
	missingRR := httptest.NewRecorder()
	h.PublicGet(missingRR, missingReq)
	if missingRR.Code != http.StatusNotFound {
		t.Errorf("missing token status = %d, want 404", missingRR.Code)
	}

	// 5. Revoke. The persona delete call must fire so the anonymous
	// allow is torn down in lockstep with the cerebro-side revoke.
	revokeReq := memberRequest(t, http.MethodDelete, "/api/cerebro/share-tokens/"+mint.ID, nil)
	revokeReq = withChi(revokeReq, "tokenId", mint.ID)
	revokeRR := httptest.NewRecorder()
	h.Revoke(revokeRR, revokeReq)
	if revokeRR.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204; body = %s", revokeRR.Code, revokeRR.Body.String())
	}
	stub.mu.Lock()
	if len(stub.deleted) != 1 || stub.deleted[0] != "grant-test-1" {
		t.Fatalf("expected one delete for grant-test-1, got %+v", stub.deleted)
	}
	stub.mu.Unlock()

	// 6. Public route on the revoked token: GetShareTokenByToken filters
	// revoked rows, so this is a clean 404 with no persona call.
	postRevokeReq := httptest.NewRequest(http.MethodGet, "/api/cerebro/public/share/"+mint.Token, nil)
	postRevokeReq = withChi(postRevokeReq, "token", mint.Token)
	postRevokeRR := httptest.NewRecorder()
	h.PublicGet(postRevokeRR, postRevokeReq)
	if postRevokeRR.Code != http.StatusNotFound {
		t.Errorf("revoked token status = %d, want 404", postRevokeRR.Code)
	}
}
