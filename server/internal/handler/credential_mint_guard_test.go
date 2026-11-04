package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCredentialMintHandlers_RejectMachineCredentials pins the handler-level
// backstop for MUL-8154: even if a route refactor drops RequireHumanActor,
// IssueCliToken and CreatePersonalAccessToken must refuse machine actors and
// must not write a PAT row.
func TestCredentialMintHandlers_RejectMachineCredentials(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	countPATs := func() int {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM personal_access_token WHERE user_id = $1`, parseUUID(testUserID)).Scan(&n); err != nil {
			t.Fatalf("count pats: %v", err)
		}
		return n
	}

	for _, source := range []string{"task_token", "cloud_pat"} {
		t.Run(source, func(t *testing.T) {
			before := countPATs()

			req := newRequest("POST", "/api/tokens", map[string]any{"name": "escalation"})
			req.Header.Set("X-Actor-Source", source)
			w := httptest.NewRecorder()
			testHandler.CreatePersonalAccessToken(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("CreatePersonalAccessToken status = %d, want 403 (body: %s)", w.Code, w.Body.String())
			}
			if after := countPATs(); after != before {
				t.Fatalf("PAT rows changed %d -> %d; rejected request must have no side effect", before, after)
			}

			req = newRequest("POST", "/api/cli-token", nil)
			req.Header.Set("X-Actor-Source", source)
			w = httptest.NewRecorder()
			testHandler.IssueCliToken(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("IssueCliToken status = %d, want 403 (body: %s)", w.Code, w.Body.String())
			}
			if bytes.Contains(w.Body.Bytes(), []byte(`"token"`)) {
				t.Fatalf("IssueCliToken leaked a token on rejection: %s", w.Body.String())
			}
		})
	}
}

// TestCredentialMintHandlers_AllowHumanActors guards against the backstop
// over-blocking: a request with no machine actor source (JWT / mul_ PAT)
// still mints credentials.
func TestCredentialMintHandlers_AllowHumanActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database connection")
	}

	req := newRequest("POST", "/api/tokens", map[string]any{"name": "human-pat"})
	w := httptest.NewRecorder()
	testHandler.CreatePersonalAccessToken(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreatePersonalAccessToken status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE user_id = $1 AND name = 'human-pat'`, parseUUID(testUserID))
	})

	req = newRequest("POST", "/api/cli-token", nil)
	w = httptest.NewRecorder()
	testHandler.IssueCliToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("IssueCliToken status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}
