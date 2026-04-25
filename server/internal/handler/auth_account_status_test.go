package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
)

func TestFindOrCreateUser_Suspended(t *testing.T) {
	ctx := context.Background()
	email := "suspended-findorcreate@multica.ai"
	var uid string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, account_status)
		VALUES ('Susp Test', $1, 'suspended')
		RETURNING id::text
	`, email).Scan(&uid)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
	})

	_, _, err = testHandler.findOrCreateUser(ctx, email)
	if err == nil {
		t.Fatal("expected error for suspended user")
	}
	if err != auth.ErrAccountSuspended {
		t.Fatalf("expected ErrAccountSuspended, got %v", err)
	}
}

func TestSendCode_SuspendedUser(t *testing.T) {
	ctx := context.Background()
	email := "suspended-sendcode@multica.ai"
	var uid string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, account_status)
		VALUES ('Susp', $1, 'suspended')
		RETURNING id::text
	`, email).Scan(&uid)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
	})

	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPost, "/auth/send-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.SendCode(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["code"] != auth.AccountSuspendedCode {
		t.Fatalf("code: got %q want %q", resp["code"], auth.AccountSuspendedCode)
	}
}

func TestGetMe_SuspendedUser(t *testing.T) {
	ctx := context.Background()
	email := "suspended-getme@multica.ai"
	var uid string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, account_status)
		VALUES ('Susp', $1, 'suspended')
		RETURNING id::text
	`, email).Scan(&uid)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-User-ID", uid)
	w := httptest.NewRecorder()
	testHandler.GetMe(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestIssueCliToken_SuspendedUser(t *testing.T) {
	ctx := context.Background()
	email := "suspended-cli@multica.ai"
	var uid string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, account_status)
		VALUES ('Susp', $1, 'suspended')
		RETURNING id::text
	`, email).Scan(&uid)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/cli-token", nil)
	req.Header.Set("X-User-ID", uid)
	w := httptest.NewRecorder()
	testHandler.IssueCliToken(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestMiddlewareAuth_SuspendedJWT(t *testing.T) {
	ctx := context.Background()
	email := "suspended-jwt@multica.ai"
	var uid string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, account_status)
		VALUES ('Susp', $1, 'suspended')
		RETURNING id::text
	`, email).Scan(&uid)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
	})

	u, err := testHandler.Queries.GetUser(ctx, parseUUID(uid))
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	token, err := testHandler.issueJWT(u)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}

	var saw403 bool
	h := middleware.Auth(testHandler.Queries)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw403 = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	if saw403 {
		t.Fatal("inner handler should not run")
	}
}
