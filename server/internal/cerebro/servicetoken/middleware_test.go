package servicetoken

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/auth"
)

// fakeStore is an in-memory Store so these tests need no database.
type fakeStore struct {
	byHash map[string]Token
	audits []AuditEvent
}

func newFakeStore() *fakeStore { return &fakeStore{byHash: map[string]Token{}} }

func (f *fakeStore) Create(_ context.Context, p CreateParams) (Token, error) {
	t := Token{ID: "tok-" + p.Name, WorkspaceID: p.WorkspaceID, Name: p.Name, TokenPrefix: p.TokenPrefix, Scopes: p.Scopes, CreatedBy: p.CreatedBy}
	f.byHash[p.TokenHash] = t
	return t, nil
}
func (f *fakeStore) GetByHash(_ context.Context, hash string) (Token, error) {
	t, ok := f.byHash[hash]
	if !ok {
		return Token{}, errNoRows
	}
	return t, nil
}
func (f *fakeStore) Touch(_ context.Context, _ string) error { return nil }
func (f *fakeStore) ListByWorkspace(_ context.Context, ws string) ([]Token, error) {
	var out []Token
	for _, t := range f.byHash {
		if t.WorkspaceID == ws {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakeStore) Revoke(_ context.Context, id, ws string) (Token, error) {
	for h, t := range f.byHash {
		if t.ID == id && t.WorkspaceID == ws {
			delete(f.byHash, h)
			return t, nil
		}
	}
	return Token{}, errNoRows
}
func (f *fakeStore) AppendAudit(_ context.Context, e AuditEvent) error {
	f.audits = append(f.audits, e)
	return nil
}

// seedToken inserts a live token with the given scopes and returns the raw
// "msv_" secret to present in the Authorization header.
func seedToken(t *testing.T, f *fakeStore, workspaceID string, scopes ...string) string {
	t.Helper()
	raw, err := GenerateServiceToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	f.byHash[auth.HashToken(raw)] = Token{
		ID:          "tok-1",
		WorkspaceID: workspaceID,
		Name:        "test",
		TokenPrefix: tokenPrefix(raw),
		Scopes:      scopes,
	}
	return raw
}

// buildRouter mounts a minimal router that mirrors production wiring: the
// auth branch runs for msv_ tokens, then the scoped machine routes plus one
// non-service path (to prove the fail-closed boundary).
func buildRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Match the upstream Auth contract: X-Actor-Source is server-set
			// only, so strip any client value before the branch stamps it.
			req.Header.Del("X-Actor-Source")
			tok := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
			if strings.HasPrefix(tok, Prefix) {
				AuthBranch(w, req, next, tok)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	r.With(RequireScope(ScopeSkillsRead)).Get("/api/service/skills", ok)
	r.With(RequireScope(ScopeIssuesRead)).Get("/api/service/issues", ok)
	r.With(RequireScope(ScopeIssuesWrite)).Post("/api/service/issues", ok)
	r.Get("/api/issues", ok) // a non-service (human) route
	return r
}

func do(t *testing.T, h http.Handler, method, path, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestServiceToken_ReadOnly_ReadSucceeds_WriteRejected is the Definition of
// Done: a skills:read token succeeds on a read route and is rejected (403) on
// a write route.
func TestServiceToken_ReadOnly_ReadSucceeds_WriteRejected(t *testing.T) {
	f := newFakeStore()
	SetAuthenticator(NewTokenService(f))
	t.Cleanup(func() { SetAuthenticator(nil) })

	raw := seedToken(t, f, "ws-1", ScopeSkillsRead)
	h := buildRouter()

	if code := do(t, h, http.MethodGet, "/api/service/skills", raw); code != http.StatusOK {
		t.Fatalf("skills:read token on read route: got %d, want 200", code)
	}
	if code := do(t, h, http.MethodPost, "/api/service/issues", raw); code != http.StatusForbidden {
		t.Fatalf("skills:read token on write route: got %d, want 403", code)
	}
	// It must also be rejected on a read route for a DIFFERENT resource it was
	// not scoped to — scopes isolate per host, not just read-vs-write.
	if code := do(t, h, http.MethodGet, "/api/service/issues", raw); code != http.StatusForbidden {
		t.Fatalf("skills:read token on issues:read route: got %d, want 403", code)
	}
}

func TestServiceToken_FailsClosedOutsideServicePrefix(t *testing.T) {
	f := newFakeStore()
	SetAuthenticator(NewTokenService(f))
	t.Cleanup(func() { SetAuthenticator(nil) })

	raw := seedToken(t, f, "ws-1", ScopeIssuesRead)
	h := buildRouter()

	// A valid service token may never reach a non-/api/service/ route.
	if code := do(t, h, http.MethodGet, "/api/issues", raw); code != http.StatusForbidden {
		t.Fatalf("service token on human route: got %d, want 403", code)
	}
}

func TestServiceToken_WriteScopeGrantsWrite(t *testing.T) {
	f := newFakeStore()
	SetAuthenticator(NewTokenService(f))
	t.Cleanup(func() { SetAuthenticator(nil) })

	raw := seedToken(t, f, "ws-1", ScopeIssuesWrite)
	h := buildRouter()

	if code := do(t, h, http.MethodPost, "/api/service/issues", raw); code != http.StatusOK {
		t.Fatalf("issues:write token on write route: got %d, want 200", code)
	}
}

func TestServiceToken_UnknownTokenRejected(t *testing.T) {
	f := newFakeStore()
	SetAuthenticator(NewTokenService(f))
	t.Cleanup(func() { SetAuthenticator(nil) })

	h := buildRouter()
	// A well-formed msv_ prefix with no matching row → 401.
	if code := do(t, h, http.MethodGet, "/api/service/skills", "msv_deadbeef"); code != http.StatusUnauthorized {
		t.Fatalf("unknown token: got %d, want 401", code)
	}
}

func TestServiceToken_NonServiceCallerRejectedOnMachineRoute(t *testing.T) {
	f := newFakeStore()
	SetAuthenticator(NewTokenService(f))
	t.Cleanup(func() { SetAuthenticator(nil) })

	h := buildRouter()
	// No Authorization header at all: the request never becomes a service
	// token, so RequireScope must 403 the machine route.
	if code := do(t, h, http.MethodGet, "/api/service/skills", ""); code != http.StatusForbidden {
		t.Fatalf("anonymous caller on machine route: got %d, want 403", code)
	}
}

// TestServiceToken_RevokeThenAuthenticate proves a revoked token no longer
// authenticates (the fake mirrors the SQL query's revoked/expired filter by
// deleting the row on revoke).
func TestServiceToken_RevokeThenAuthenticate(t *testing.T) {
	f := newFakeStore()
	svc := NewTokenService(f)
	SetAuthenticator(svc)
	t.Cleanup(func() { SetAuthenticator(nil) })

	raw := seedToken(t, f, "ws-1", ScopeSkillsRead)
	h := buildRouter()
	if code := do(t, h, http.MethodGet, "/api/service/skills", raw); code != http.StatusOK {
		t.Fatalf("pre-revoke: got %d, want 200", code)
	}
	if _, err := svc.Revoke(context.Background(), "tok-1", "ws-1", "admin-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if code := do(t, h, http.MethodGet, "/api/service/skills", raw); code != http.StatusUnauthorized {
		t.Fatalf("post-revoke: got %d, want 401", code)
	}
}
