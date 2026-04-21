package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newBootstrapTestHandler(t *testing.T, cfg Config) (*Handler, context.Context) {
	t.Helper()

	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
	})

	return &Handler{
		Queries:      db.New(tx),
		DB:           tx,
		TxStarter:    tx,
		EmailService: service.NewEmailService(),
		Analytics:    analytics.NoopClient{},
		cfg:          cfg,
	}, ctx
}

func resetBootstrapState(t *testing.T, ctx context.Context, h *Handler) {
	t.Helper()

	statements := []string{
		`DELETE FROM agent`,
		`DELETE FROM agent_runtime`,
		`DELETE FROM member`,
		`DELETE FROM workspace`,
		`DELETE FROM verification_code`,
		`DELETE FROM "user"`,
	}
	for _, stmt := range statements {
		if _, err := h.DB.Exec(ctx, stmt); err != nil {
			t.Fatalf("reset bootstrap state failed for %q: %v", stmt, err)
		}
	}
}

func insertBootstrapUser(t *testing.T, ctx context.Context, h *Handler, name, email string) string {
	t.Helper()

	var userID string
	if err := h.DB.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return userID
}

func insertBootstrapWorkspace(t *testing.T, ctx context.Context, h *Handler, userID string, slug string) {
	t.Helper()

	var workspaceID string
	if err := h.DB.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Bootstrap Workspace", slug, "bootstrap test workspace", "BTS").Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	if _, err := h.DB.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
}

func TestBootstrapCreatesTrustedOwnerWhenDatabaseIsEmpty(t *testing.T) {
	h, ctx := newBootstrapTestHandler(t, Config{
		AllowSignup:         false,
		AllowedEmails:       []string{"nobody@example.com"},
		AllowedEmailDomains: []string{"blocked.example.com"},
	})
	resetBootstrapState(t, ctx, h)

	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap", nil)
	rec := httptest.NewRecorder()

	h.Bootstrap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Bootstrap: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp BootstrapResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Mode != trustedBootstrapMode {
		t.Fatalf("expected mode %q, got %q", trustedBootstrapMode, resp.Mode)
	}
	if resp.OwnerResolution != trustedBootstrapOwnerResolutionNew {
		t.Fatalf("expected owner_resolution %q, got %q", trustedBootstrapOwnerResolutionNew, resp.OwnerResolution)
	}
	if resp.BootstrapState != trustedBootstrapStateReady {
		t.Fatalf("expected bootstrap_state %q, got %q", trustedBootstrapStateReady, resp.BootstrapState)
	}
	if resp.User.Name != trustedBootstrapOwnerName || resp.User.Email != trustedBootstrapOwnerEmail {
		t.Fatalf("expected trusted owner %q <%s>, got %q <%s>", trustedBootstrapOwnerName, trustedBootstrapOwnerEmail, resp.User.Name, resp.User.Email)
	}
	if len(resp.Workspaces) != 0 {
		t.Fatalf("expected no workspaces, got %d", len(resp.Workspaces))
	}

	cookies := rec.Result().Cookies()
	seenAuth := false
	seenCSRF := false
	for _, cookie := range cookies {
		switch cookie.Name {
		case auth.AuthCookieName:
			seenAuth = true
		case auth.CSRFCookieName:
			seenCSRF = true
		}
	}
	if !seenAuth || !seenCSRF {
		t.Fatalf("expected auth and csrf cookies, got %+v", cookies)
	}
}

func TestBootstrapResumesSoleOwnerAndReturnsWorkspaces(t *testing.T) {
	h, ctx := newBootstrapTestHandler(t, Config{AllowSignup: true})
	resetBootstrapState(t, ctx, h)

	userID := insertBootstrapUser(t, ctx, h, "Sole Owner", "owner@example.com")
	insertBootstrapWorkspace(t, ctx, h, userID, "bootstrap-workspace")

	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap", nil)
	rec := httptest.NewRecorder()

	h.Bootstrap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Bootstrap: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp BootstrapResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OwnerResolution != trustedBootstrapOwnerResolutionOld {
		t.Fatalf("expected owner_resolution %q, got %q", trustedBootstrapOwnerResolutionOld, resp.OwnerResolution)
	}
	if resp.User.Email != "owner@example.com" {
		t.Fatalf("expected resumed owner email, got %q", resp.User.Email)
	}
	if len(resp.Workspaces) != 1 || resp.Workspaces[0].Slug != "bootstrap-workspace" {
		t.Fatalf("expected bootstrap workspace in response, got %+v", resp.Workspaces)
	}
}

func TestBootstrapFailsClosedForMultiUserDatabase(t *testing.T) {
	h, ctx := newBootstrapTestHandler(t, Config{AllowSignup: true})
	resetBootstrapState(t, ctx, h)

	insertBootstrapUser(t, ctx, h, "First User", "first@example.com")
	insertBootstrapUser(t, ctx, h, "Second User", "second@example.com")

	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap", nil)
	rec := httptest.NewRecorder()

	h.Bootstrap(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("Bootstrap: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp BootstrapConflictResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Mode != trustedBootstrapMode {
		t.Fatalf("expected mode %q, got %q", trustedBootstrapMode, resp.Mode)
	}
	if resp.BootstrapState != trustedBootstrapStateMultiUserDB {
		t.Fatalf("expected bootstrap_state %q, got %q", trustedBootstrapStateMultiUserDB, resp.BootstrapState)
	}
}

func TestBootstrapTokenReturnsBearerWithoutSettingBrowserCookies(t *testing.T) {
	h, ctx := newBootstrapTestHandler(t, Config{AllowSignup: false})
	resetBootstrapState(t, ctx, h)

	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap/token", nil)
	rec := httptest.NewRecorder()

	h.BootstrapToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("BootstrapToken: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp BootstrapResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected bearer token in bootstrap token response")
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == auth.AuthCookieName || cookie.Name == auth.CSRFCookieName {
			t.Fatalf("did not expect browser auth cookies from bootstrap token endpoint, got %+v", rec.Result().Cookies())
		}
	}
}

func TestVerifyCodeDoesNotCreateAdditionalUsersInTrustedBootstrapMode(t *testing.T) {
	h, ctx := newBootstrapTestHandler(t, Config{AllowSignup: true})
	resetBootstrapState(t, ctx, h)

	insertBootstrapUser(t, ctx, h, trustedBootstrapOwnerName, trustedBootstrapOwnerEmail)
	if _, err := h.Queries.CreateVerificationCode(ctx, db.CreateVerificationCodeParams{
		Email:     "outsider@example.com",
		Code:      "123456",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("CreateVerificationCode: %v", err)
	}

	body := map[string]string{"email": "outsider@example.com", "code": "123456"}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.VerifyCode(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("VerifyCode: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	var userCount int
	if err := h.DB.QueryRow(ctx, `SELECT count(*) FROM "user"`).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected exactly one trusted owner user, got %d", userCount)
	}
}
