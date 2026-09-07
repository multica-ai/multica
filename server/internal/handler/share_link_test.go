package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var testSeq atomic.Int64

type shareJoinCapacityStub struct {
	mu          sync.Mutex
	claimTokens []uuid.UUID
	claimGate   *sync.WaitGroup
}

func (*shareJoinCapacityStub) RecoveryAvailable() bool { return true }

func (s *shareJoinCapacityStub) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) ClaimShareJoin(_ context.Context, _ uuid.UUID, token uuid.UUID) (seatcapacity.Decision, error) {
	s.mu.Lock()
	s.claimTokens = append(s.claimTokens, token)
	s.mu.Unlock()
	if s.claimGate != nil {
		s.claimGate.Done()
		s.claimGate.Wait()
	}
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) Consume(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) Release(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) GetOperation(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{}, nil
}

func (s *shareJoinCapacityStub) tokens() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uuid.UUID(nil), s.claimTokens...)
}

func pgInt32(v int32) *int32 { return &v }

func clearShareLinksForTestWorkspace(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`DELETE FROM workspace_share_link WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID),
	); err != nil {
		t.Fatalf("clear share links: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_share_link WHERE workspace_id = $1`,
			parseUUID(testWorkspaceID),
		)
	})
}

// createTestUserAndMember inserts a fresh user and (optionally) a member row in
// the test workspace with the given role. Returns the new user id.
func createTestUserAndMember(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	seq := testSeq.Add(1)
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("sharelink-test-%d", seq),
		fmt.Sprintf("sharelink-test-%d@multica.ai", seq),
	).Scan(&userID); err != nil {
		t.Fatalf("create share-link test user: %v", err)
	}
	if role != "" {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
			parseUUID(testWorkspaceID), parseUUID(userID), role,
		); err != nil {
			t.Fatalf("create share-link test member: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, parseUUID(userID))
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, parseUUID(userID))
	})
	return userID
}

// createTestShareLink inserts a share link directly and returns its code.
func createTestShareLink(t *testing.T, workspaceID, role string, maxUses int32, expireHours int32) db.WorkspaceShareLink {
	t.Helper()
	ctx := context.Background()
	var link db.WorkspaceShareLink
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_share_link (workspace_id, code, created_by, role, max_uses, expires_at)
		VALUES ($1, $2, $3, $4,
		        CASE WHEN $5 = 0 THEN NULL ELSE $5 END,
		        CASE WHEN $6 = 0 THEN NULL ELSE now() + ($6 * interval '1 hour') END)
		RETURNING id, workspace_id, code, created_by, role, expires_at, max_uses, use_count, is_active, created_at
	`,
		parseUUID(workspaceID),
		fmt.Sprintf("testcode-%d", testSeq.Add(1)),
		parseUUID(testUserID),
		role,
		pgInt32(maxUses),
		pgInt32(expireHours),
	).Scan(&link.ID, &link.WorkspaceID, &link.Code, &link.CreatedBy, &link.Role, &link.ExpiresAt, &link.MaxUses, &link.UseCount, &link.IsActive, &link.CreatedAt)
	if err != nil {
		t.Fatalf("create test share link: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_share_link WHERE id = $1`, link.ID)
	})
	return link
}

// --- Tests ---

func TestCreateShareLink_RequiresOwnerAdmin(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
		r.Post("/share-links", testHandler.CreateShareLink)
	})

	// Owner succeeds.
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member is denied.
	memberID := createTestUserAndMember(t, "member")
	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req2.Header.Set("X-User-ID", memberID)
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("member create: expected 403, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestListShareLinks_RequiresOwnerAdmin(t *testing.T) {
	clearShareLinksForTestWorkspace(t)
	createTestShareLink(t, testWorkspaceID, "member", 0, 0)

	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
		r.Get("/share-links", testHandler.ListShareLinks)
	})

	// Owner can list.
	req := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/share-links", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner list: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member denied.
	memberID := createTestUserAndMember(t, "member")
	req2 := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/share-links", nil)
	req2.Header.Set("X-User-ID", memberID)
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("member list: expected 403, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateShareLink_InvalidatesOldLink(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	first := createTestShareLink(t, testWorkspaceID, "member", 0, 0)

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateShareLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var active bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT is_active FROM workspace_share_link WHERE id = $1`, first.ID).Scan(&active); err != nil {
		t.Fatalf("reload old link: %v", err)
	}
	if active {
		t.Fatal("old share link must be deactivated after creating a new one")
	}
}

func TestCreateShareLink_RejectsInvalidInput(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// Negative expires_in is rejected.
	neg := -1
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", ExpiresIn: &neg})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateShareLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative expires_in: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Zero max_uses is rejected.
	zero := 0
	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", MaxUses: &zero})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateShareLink(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("zero max_uses: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateShareLink_RejectsOverflowingInput(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// max_uses above math.MaxInt32 wraps in int32(...) — must be rejected.
	overMax := 2147483648 // MaxInt32 + 1
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", MaxUses: &overMax})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateShareLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("max_uses over MaxInt32: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// expires_in above the duration-safe bound overflows time.Duration * time.Hour.
	overExp := int(expiresInMaxHours) + 1
	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", ExpiresIn: &overExp})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateShareLink(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expires_in over duration-safe bound: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}

	// Boundary: expires_in == expiresInMaxHours must be accepted, and the
	// stored expires_at must be strictly in the future (not wrapped negative).
	maxExp := int(expiresInMaxHours)
	req3 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", ExpiresIn: &maxExp})
	req3 = withURLParam(req3, "id", testWorkspaceID)
	w3 := httptest.NewRecorder()
	testHandler.CreateShareLink(w3, req3)
	if w3.Code != http.StatusCreated {
		t.Fatalf("expires_in = duration-safe max: expected 201, got %d: %s", w3.Code, w3.Body.String())
	}
	var expiresAt pgtype.Timestamptz
	if err := testPool.QueryRow(context.Background(),
		`SELECT expires_at FROM workspace_share_link WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 1`,
		parseUUID(testWorkspaceID)).Scan(&expiresAt); err != nil {
		t.Fatalf("load created link expiry: %v", err)
	}
	if !expiresAt.Valid || !expiresAt.Time.After(time.Now()) {
		t.Fatalf("accepted max expires_in must persist a future expires_at, got %v", expiresAt.Time)
	}

	// Deactivate so a fresh active link can be created for the max_uses check.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace_share_link SET is_active = false WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("deactivate for max_uses boundary: %v", err)
	}
	maxUse := 2147483647
	req4 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", MaxUses: &maxUse})
	req4 = withURLParam(req4, "id", testWorkspaceID)
	w4 := httptest.NewRecorder()
	testHandler.CreateShareLink(w4, req4)
	if w4.Code != http.StatusCreated {
		t.Fatalf("max_uses = MaxInt32: expected 201, got %d: %s", w4.Code, w4.Body.String())
	}
}

func TestJoinShareLink_ExpiredAndRevoked(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// Expired link.
	expired := createTestShareLink(t, testWorkspaceID, "member", 0, -1)
	userID := createTestUserAndMember(t, "")

	req := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: expired.Code})
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.JoinByShareLink(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expired join: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Deactivate the expired link so the workspace has room for a new active one.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace_share_link SET is_active = false WHERE id = $1`, expired.ID); err != nil {
		t.Fatalf("deactivate expired link: %v", err)
	}

	// Revoked link.
	revoked := createTestShareLink(t, testWorkspaceID, "member", 0, 0)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace_share_link SET is_active = false WHERE id = $1`, revoked.ID); err != nil {
		t.Fatalf("revoke link: %v", err)
	}
	req2 := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: revoked.Code})
	req2.Header.Set("X-User-ID", userID)
	w2 := httptest.NewRecorder()
	testHandler.JoinByShareLink(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("revoked join: expected 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestJoinShareLink_ConcurrentLastUse(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// max_uses = 1: only one concurrent join may succeed.
	link := createTestShareLink(t, testWorkspaceID, "member", 1, 0)

	const n = 4
	results := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			userID := createTestUserAndMember(t, "")
			req := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: link.Code})
			req.Header.Set("X-User-ID", userID)
			w := httptest.NewRecorder()
			testHandler.JoinByShareLink(w, req)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)

	ok := 0
	conflict := 0
	for code := range results {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusNotFound, http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected join status: %d", code)
		}
	}
	if ok != 1 {
		t.Fatalf("max_uses=1: expected exactly 1 success, got %d", ok)
	}
}

func TestJoinShareLink_DuplicateJoinDoesNotConsumeUse(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	link := createTestShareLink(t, testWorkspaceID, "member", 5, 0)
	userID := createTestUserAndMember(t, "")

	// First join succeeds.
	req := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: link.Code})
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.JoinByShareLink(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first join: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Duplicate join is rejected and must NOT consume another use.
	req2 := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: link.Code})
	req2.Header.Set("X-User-ID", userID)
	w2 := httptest.NewRecorder()
	testHandler.JoinByShareLink(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate join: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}

	var useCount int32
	if err := testPool.QueryRow(context.Background(),
		`SELECT use_count FROM workspace_share_link WHERE id = $1`, link.ID).Scan(&useCount); err != nil {
		t.Fatalf("reload use count: %v", err)
	}
	if useCount != 1 {
		t.Fatalf("duplicate join must not consume a use: expected use_count=1, got %d", useCount)
	}
}

func TestJoinShareLink_ReactivatesDeadLetterWithSameCapacityToken(t *testing.T) {
	clearShareLinksForTestWorkspace(t)
	link := createTestShareLink(t, testWorkspaceID, "member", 5, 0)
	userID := createTestUserAndMember(t, "")
	stub := &shareJoinCapacityStub{}
	useSeatCapacity(t, stub)

	queries := db.New(testPool)
	originalToken := uuid.New()
	intent, err := queries.CreateOrReactivateShareJoinCapacityIntent(context.Background(), db.CreateOrReactivateShareJoinCapacityIntentParams{
		WorkspaceID: link.WorkspaceID, OperationToken: uuidToPG(originalToken),
		ShareLinkID: link.ID, UserID: parseUUID(userID),
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM seat_capacity_outbox WHERE operation_token = $1`, intent.OperationToken)
	})
	if _, err := testPool.Exec(context.Background(), `
		UPDATE seat_capacity_outbox
		SET attempt_count = 10, dead_lettered_at = now(), last_error = 'stuck'
		WHERE operation_token = $1
	`, intent.OperationToken); err != nil {
		t.Fatal(err)
	}

	req := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: link.Code})
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.JoinByShareLink(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("join: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	tokens := stub.tokens()
	if len(tokens) != 1 || tokens[0] != originalToken {
		t.Fatalf("claim tokens=%v, want original %s", tokens, originalToken)
	}
}

func TestJoinShareLink_ConcurrentSameUserUsesOneCapacityOperation(t *testing.T) {
	clearShareLinksForTestWorkspace(t)
	link := createTestShareLink(t, testWorkspaceID, "member", 5, 0)
	userID := createTestUserAndMember(t, "")
	gate := &sync.WaitGroup{}
	gate.Add(2)
	stub := &shareJoinCapacityStub{claimGate: gate}
	useSeatCapacity(t, stub)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM seat_capacity_outbox
			WHERE workspace_id = $1 AND share_link_id = $2 AND user_id = $3
		`, link.WorkspaceID, link.ID, parseUUID(userID))
	})

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: link.Code})
			req.Header.Set("X-User-ID", userID)
			w := httptest.NewRecorder()
			testHandler.JoinByShareLink(w, req)
			statuses <- w.Code
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	var successes, conflicts int
	for status := range statuses {
		switch status {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected concurrent join status %d", status)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
	tokens := stub.tokens()
	if len(tokens) != 2 || tokens[0] != tokens[1] {
		t.Fatalf("concurrent claim tokens=%v, want one logical operation", tokens)
	}
	var members int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2
	`, link.WorkspaceID, parseUUID(userID)).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 1 {
		t.Fatalf("member rows=%d, want 1", members)
	}
}

func TestGetShareLinkInfo_PublicResponseSlim(t *testing.T) {
	clearShareLinksForTestWorkspace(t)
	link := createTestShareLink(t, testWorkspaceID, "admin", 0, 0)

	req := newRequest("GET", "/api/share-links/"+link.Code, nil)
	req = withURLParam(req, "code", link.Code)
	w := httptest.NewRecorder()
	testHandler.GetShareLinkInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("public preview: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("parse preview: %v", err)
	}
	if _, ok := raw["creator_email"]; ok {
		t.Fatal("public preview must not expose the inviter's email")
	}
	if _, ok := raw["workspace_id"]; ok {
		t.Fatal("public preview must not expose the internal workspace id")
	}
	if _, ok := raw["code"]; ok {
		t.Fatal("public preview must not expose the invite code")
	}
}

func TestShareLink_CrossWorkspaceDenied(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// A user who is a member of another workspace must not list/create links
	// in the test workspace. Create a member in a throwaway workspace.
	ctx := context.Background()
	var otherWS string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"ShareLink Other", fmt.Sprintf("sharelink-other-%d", testSeq.Add(1)),
	).Scan(&otherWS); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, parseUUID(otherWS))
	})

	otherUser := createTestUserAndMember(t, "")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		parseUUID(otherWS), parseUUID(otherUser),
	); err != nil {
		t.Fatalf("add owner to other workspace: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
		r.Post("/share-links", testHandler.CreateShareLink)
	})

	// otherUser is not a member of the test workspace -> must be denied.
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req.Header.Set("X-User-ID", otherUser)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace create: expected 403 or 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateShareLink_ConcurrentCreatesSingleActive(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// Concurrent creates on the same workspace: the partial unique index
	// (workspace_id WHERE is_active) serializes them so that at most one active
	// link exists at any time — later creates deactivate the earlier one rather
	// than both succeeding. Assert the invariant: exactly one active row.
	const n = 3
	results := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
			req = withURLParam(req, "id", testWorkspaceID)
			w := httptest.NewRecorder()
			testHandler.CreateShareLink(w, req)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)

	for code := range results {
		if code != http.StatusCreated && code != http.StatusConflict && code != http.StatusInternalServerError {
			t.Fatalf("unexpected create status: %d", code)
		}
	}

	var active int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_share_link WHERE workspace_id = $1 AND is_active = true`,
		parseUUID(testWorkspaceID),
	).Scan(&active); err != nil {
		t.Fatalf("count active links: %v", err)
	}
	if active != 1 {
		t.Fatalf("concurrent creates: expected exactly 1 active link, got %d", active)
	}
}
