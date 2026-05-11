package references

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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testWorkspaceSlug = "ref-tests"
	testOtherWSSlug   = "ref-tests-other"
	testUserEmail     = "ref-tests@multica.ai"
	testOtherEmail    = "ref-other@multica.ai"
)

var (
	testPool       *pgxpool.Pool
	testWorkspace  string
	testOtherWS    string
	testUser       string
	testMember     db.Member
	testOtherUser  string
	testOtherMembr db.Member
	testIssue      db.Issue
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica_firtal_cerebro_630?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("references: skipping tests, no DB: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("references: skipping tests, DB unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool

	if err := setupFixture(ctx, pool); err != nil {
		fmt.Printf("references: setup failed: %v\n", err)
		_ = teardownFixture(context.Background(), pool)
		pool.Close()
		os.Exit(1)
	}
	code := m.Run()
	if err := teardownFixture(context.Background(), pool); err != nil {
		fmt.Printf("references: teardown failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func mustUUID(t testing.TB, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

func parseUUIDOrPanic(s string) pgtype.UUID {
	u, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

func setupFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := teardownFixture(ctx, pool); err != nil {
		return err
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reference Tests", testUserEmail,
	).Scan(&testUser); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reference Other", testOtherEmail,
	).Scan(&testOtherUser); err != nil {
		return fmt.Errorf("create other user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		"Reference Tests", testWorkspaceSlug, "References test workspace", "REF",
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
		Title:       "Reference test issue",
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

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		"Reference Other WS", testOtherWSSlug, "other", "ROT",
	).Scan(&testOtherWS); err != nil {
		return fmt.Errorf("create other workspace: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
		RETURNING id, workspace_id, user_id, role, created_at, budget_enforcement_enabled`,
		testOtherWS, testOtherUser,
	).Scan(&testOtherMembr.ID, &testOtherMembr.WorkspaceID, &testOtherMembr.UserID, &testOtherMembr.Role, &testOtherMembr.CreatedAt, &testOtherMembr.BudgetEnforcementEnabled); err != nil {
		return fmt.Errorf("create other member: %w", err)
	}
	return nil
}

func teardownFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug IN ($1, $2)`, testWorkspaceSlug, testOtherWSSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email IN ($1, $2)`, testUserEmail, testOtherEmail); err != nil {
		return err
	}
	return nil
}

// newHandler builds a fresh Handler against the live DB. Each test gets its
// own bus so WS-event assertions don't bleed between tests.
func newHandler() (*Handler, *events.Bus) {
	bus := events.New()
	h := New(cerebrodb.New(testPool), db.New(testPool), bus)
	return h, bus
}

// requestAs builds a request scoped to the testWorkspace + testUser member,
// matching the context shape that RequireWorkspaceMember would set up in
// production. Returns *http.Request ready for handler invocation.
func requestAs(t testing.TB, member db.Member, workspaceID, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", util.UUIDToString(member.UserID))
	ctx := middleware.SetMemberContext(req.Context(), workspaceID, member)
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

func newCollector(bus *events.Bus) *eventCollector {
	c := &eventCollector{}
	bus.SubscribeAll(func(ev events.Event) {
		c.add(ev)
	})
	return c
}

type eventCollector struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *eventCollector) add(ev events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *eventCollector) snapshot() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.events))
	copy(out, c.events)
	return out
}

// cleanupReferences wipes the table so tests don't leak rows into one
// another. Called at the start of every test that exercises mutations.
func cleanupReferences(t testing.TB) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM cerebro_issue_reference WHERE issue_id = $1`, testIssue.ID); err != nil {
		t.Fatalf("cleanup references: %v", err)
	}
}

func decodeReference(t testing.TB, body []byte) ReferenceResponse {
	t.Helper()
	var resp ReferenceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode reference: %v: %s", err, string(body))
	}
	return resp
}

func TestCreateReference_PersistsAndEmitsEvent(t *testing.T) {
	cleanupReferences(t)
	h, bus := newHandler()
	collector := newCollector(bus)

	body := map[string]any{
		"object":   "stripe.customer",
		"type":     "billing",
		"ref_id":   "cus_001",
		"label":    "Acme",
		"url":      "https://dashboard.stripe.com/customers/cus_001",
		"metadata": map[string]any{"plan": "pro"},
	}
	req := requestAs(t, testMember, testWorkspace, http.MethodPost,
		"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", body)
	req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
	rr := httptest.NewRecorder()
	h.Create(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := decodeReference(t, rr.Body.Bytes())
	if resp.Object != "stripe.customer" || resp.RefID != "cus_001" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.CreatedByType != "member" || resp.CreatedByID != util.UUIDToString(testMember.UserID) {
		t.Fatalf("creator not stamped: %+v", resp)
	}

	got := collector.snapshot()
	if len(got) != 1 || got[0].Type != EventReferenceCreated {
		t.Fatalf("expected single created event, got %+v", got)
	}
	if got[0].WorkspaceID != testWorkspace {
		t.Fatalf("event workspace mismatch: %s", got[0].WorkspaceID)
	}
}

func TestCreateReference_UpsertOnConflict(t *testing.T) {
	cleanupReferences(t)
	h, bus := newHandler()
	collector := newCollector(bus)

	post := func(label string, meta map[string]any) ReferenceResponse {
		body := map[string]any{
			"object":   "github.pr",
			"type":     "review",
			"ref_id":   "pr-42",
			"label":    label,
			"metadata": meta,
		}
		req := requestAs(t, testMember, testWorkspace, http.MethodPost,
			"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", body)
		req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
		rr := httptest.NewRecorder()
		h.Create(rr, req)
		if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
			t.Fatalf("post: unexpected %d: %s", rr.Code, rr.Body.String())
		}
		return decodeReference(t, rr.Body.Bytes())
	}

	first := post("first", map[string]any{"v": 1})
	// Force a small clock gap so we can detect updated_at advancing without
	// needing sub-millisecond precision.
	time.Sleep(5 * time.Millisecond)
	second := post("second", map[string]any{"v": 2})

	if first.ID != second.ID {
		t.Fatalf("UPSERT created two rows: %s != %s", first.ID, second.ID)
	}
	if second.Label == nil || *second.Label != "second" {
		t.Fatalf("UPSERT did not refresh label: %+v", second.Label)
	}
	count := 0
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM cerebro_issue_reference WHERE issue_id = $1`,
		testIssue.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after UPSERT, got %d", count)
	}

	events := collector.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventReferenceCreated || events[1].Type != EventReferenceUpdated {
		t.Fatalf("event order wrong: %s, %s", events[0].Type, events[1].Type)
	}
}

func TestListReferencesByIssue(t *testing.T) {
	cleanupReferences(t)
	h, _ := newHandler()
	for i, refID := range []string{"a", "b", "c"} {
		body := map[string]any{
			"object": "test.thing",
			"type":   "kind",
			"ref_id": refID,
			"label":  fmt.Sprintf("row-%d", i),
		}
		req := requestAs(t, testMember, testWorkspace, http.MethodPost,
			"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", body)
		req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
		rr := httptest.NewRecorder()
		h.Create(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed %d: %d %s", i, rr.Code, rr.Body.String())
		}
	}

	req := requestAs(t, testMember, testWorkspace, http.MethodGet,
		"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", nil)
	req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
	rr := httptest.NewRecorder()
	h.ListByIssue(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		References []ReferenceResponse `json:"references"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(out.References) != 3 {
		t.Fatalf("expected 3 references, got %d", len(out.References))
	}
}

func TestUpdateReference_PatchOnlyMutableFields(t *testing.T) {
	cleanupReferences(t)
	h, bus := newHandler()
	collector := newCollector(bus)

	created := seedReference(t, h, "linear.issue", "tracker", "TKR-1",
		ptr("Old"), ptr("https://linear.app/old"), map[string]any{"x": 1})

	patch := map[string]any{
		"label":    "New",
		"metadata": map[string]any{"x": 2, "y": 3},
	}
	req := requestAs(t, testMember, testWorkspace, http.MethodPatch,
		"/api/cerebro/references/"+created.ID, patch)
	req = withChiParam(req, "refId", created.ID)
	rr := httptest.NewRecorder()
	h.Update(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rr.Code, rr.Body.String())
	}
	got := decodeReference(t, rr.Body.Bytes())
	if got.Label == nil || *got.Label != "New" {
		t.Fatalf("label not updated: %+v", got.Label)
	}
	if got.URL == nil || *got.URL != "https://linear.app/old" {
		t.Fatalf("url should remain unchanged when omitted: %+v", got.URL)
	}
	var meta map[string]any
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta["y"] != float64(3) {
		t.Fatalf("metadata not refreshed: %+v", meta)
	}

	events := collector.snapshot()
	if len(events) == 0 || events[len(events)-1].Type != EventReferenceUpdated {
		t.Fatalf("expected updated event, got %+v", events)
	}
}

func TestDeleteReference_EmitsEvent(t *testing.T) {
	cleanupReferences(t)
	h, bus := newHandler()
	collector := newCollector(bus)

	created := seedReference(t, h, "obj.del", "k", "1", nil, nil, nil)

	req := requestAs(t, testMember, testWorkspace, http.MethodDelete,
		"/api/cerebro/references/"+created.ID, nil)
	req = withChiParam(req, "refId", created.ID)
	rr := httptest.NewRecorder()
	h.Delete(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}
	count := 0
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM cerebro_issue_reference WHERE id = $1`,
		mustUUID(t, created.ID),
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("row not deleted: %d remain", count)
	}
	events := collector.snapshot()
	if len(events) == 0 || events[len(events)-1].Type != EventReferenceDeleted {
		t.Fatalf("expected deleted event, got %+v", events)
	}
}

func TestFKCascadeOnIssueDelete(t *testing.T) {
	cleanupReferences(t)
	q := db.New(testPool)
	scratch, err := q.CreateIssue(context.Background(), db.CreateIssueParams{
		WorkspaceID: parseUUIDOrPanic(testWorkspace),
		Title:       "Scratch issue for FK test",
		Status:      "todo",
		Priority:    "medium",
		Number:      99,
		CreatorType: "member",
		CreatorID:   parseUUIDOrPanic(testUser),
	})
	if err != nil {
		t.Fatalf("create scratch: %v", err)
	}
	cerebro := cerebrodb.New(testPool)
	if _, err := cerebro.InsertReference(context.Background(), cerebrodb.InsertReferenceParams{
		IssueID:       scratch.ID,
		Object:        "fk.target",
		RefID:         "victim",
		Metadata:      []byte(`{}`),
		CreatedByType: "member",
		CreatedByID:   parseUUIDOrPanic(testUser),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, scratch.ID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	count := 0
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM cerebro_issue_reference WHERE issue_id = $1`,
		scratch.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected FK cascade to wipe references, got %d remaining", count)
	}
}

func TestNonMemberCannotAccessIssueReferences(t *testing.T) {
	cleanupReferences(t)
	h, _ := newHandler()
	// Other user is a member of testOtherWS, NOT testWorkspace. Even with
	// SetMemberContext pinning their context to testOtherWS, asking for an
	// issue from testWorkspace must 404.
	req := requestAs(t, testOtherMembr, testOtherWS, http.MethodGet,
		"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", nil)
	req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
	rr := httptest.NewRecorder()
	h.ListByIssue(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 cross-workspace, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestListByObject_FiltersToWorkspace(t *testing.T) {
	cleanupReferences(t)
	h, _ := newHandler()
	// Seed a reference in testWorkspace.
	seedReference(t, h, "shared.kind", "x", "shared-1", ptr("mine"), nil, nil)
	// Seed an unrelated row in testOtherWS via direct insert so the reverse
	// lookup must filter it out.
	otherIssue, err := db.New(testPool).CreateIssue(context.Background(), db.CreateIssueParams{
		WorkspaceID: parseUUIDOrPanic(testOtherWS),
		Title:       "Other ws issue",
		Status:      "todo",
		Priority:    "medium",
		Number:      1,
		CreatorType: "member",
		CreatorID:   parseUUIDOrPanic(testOtherUser),
	})
	if err != nil {
		t.Fatalf("create other issue: %v", err)
	}
	if _, err := cerebrodb.New(testPool).InsertReference(context.Background(), cerebrodb.InsertReferenceParams{
		IssueID:       otherIssue.ID,
		Object:        "shared.kind",
		RefID:         "shared-1",
		Metadata:      []byte(`{}`),
		CreatedByType: "member",
		CreatedByID:   parseUUIDOrPanic(testOtherUser),
	}); err != nil {
		t.Fatalf("seed cross-ws: %v", err)
	}

	req := requestAs(t, testMember, testWorkspace, http.MethodGet,
		"/api/cerebro/references?object=shared.kind&ref_id=shared-1", nil)
	rr := httptest.NewRecorder()
	h.ListByObject(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list-by-object: %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		References []ReferenceResponse `json:"references"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.References) != 1 {
		t.Fatalf("expected 1 reference filtered to workspace, got %d", len(out.References))
	}
	if out.References[0].IssueID != util.UUIDToString(testIssue.ID) {
		t.Fatalf("returned wrong issue: %s", out.References[0].IssueID)
	}
}

func TestRejectsBadInput(t *testing.T) {
	cleanupReferences(t)
	h, _ := newHandler()
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing object", map[string]any{"ref_id": "x"}, http.StatusBadRequest},
		{"missing ref_id", map[string]any{"object": "x"}, http.StatusBadRequest},
		{"non-object metadata", map[string]any{"object": "x", "ref_id": "y", "metadata": []any{1, 2}}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := requestAs(t, testMember, testWorkspace, http.MethodPost,
				"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", tc.body)
			req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
			rr := httptest.NewRecorder()
			h.Create(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rr.Code, rr.Body.String())
			}
		})
	}
}

// seedReference is a small helper that POSTs through the handler so the row
// goes through the same validation path as production. Used by tests that
// then act on the resulting reference.
func seedReference(t testing.TB, h *Handler, object, refType, refID string, label, url *string, metadata map[string]any) ReferenceResponse {
	t.Helper()
	body := map[string]any{
		"object": object,
		"ref_id": refID,
	}
	if refType != "" {
		body["type"] = refType
	}
	if label != nil {
		body["label"] = *label
	}
	if url != nil {
		body["url"] = *url
	}
	if metadata != nil {
		body["metadata"] = metadata
	}
	req := requestAs(t, testMember, testWorkspace, http.MethodPost,
		"/api/issues/"+util.UUIDToString(testIssue.ID)+"/references", body)
	req = withChiParam(req, "id", util.UUIDToString(testIssue.ID))
	rr := httptest.NewRecorder()
	h.Create(rr, req)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", rr.Code, rr.Body.String())
	}
	return decodeReference(t, rr.Body.Bytes())
}

func ptr(s string) *string { return &s }
