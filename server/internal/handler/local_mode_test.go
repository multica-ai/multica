package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLocalLoginDisabledIsNotFound(t *testing.T) {
	h := &Handler{cfg: Config{LocalMode: false}}
	recorder := httptest.NewRecorder()
	h.LocalLogin(recorder, httptest.NewRequest(http.MethodPost, "/auth/local", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("LocalLogin() status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestLocalSetupAndLoginBootstrapOneIdentity(t *testing.T) {
	if testPool == nil {
		t.Skip("database fixture is unavailable")
	}
	ctx := context.Background()
	cleanup := func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM lifeos_local_credential`)
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, localWorkspaceSlug)
		_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, localUserEmail)
	}
	cleanup()
	t.Cleanup(cleanup)

	t.Setenv("JWT_SECRET", "local-mode-test-secret")
	h := &Handler{
		Queries: db.New(testPool),
		DB:      testPool,
		cfg: Config{
			LocalMode:            true,
			LocalAutomationToken: "test-automation-token-12345678901234567890",
		},
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			map[bool]string{true: "/auth/local/setup", false: "/auth/local"}[i == 0],
			strings.NewReader(`{"username":"xingyao","password":"Strong!LifeOS2026"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		if i == 0 {
			h.LocalSetup(recorder, request)
		} else {
			h.LocalLogin(recorder, request)
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("local auth status = %d, want 200: %s", recorder.Code, recorder.Body.String())
		}

		var response LocalLoginResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode LocalLogin response: %v", err)
		}
		if response.User.Email != localUserEmail || response.User.OnboardedAt == nil {
			t.Fatalf("unexpected local user: %+v", response.User)
		}
		if response.Workspace.Slug != localWorkspaceSlug {
			t.Fatalf("workspace slug = %q, want %q", response.Workspace.Slug, localWorkspaceSlug)
		}

		cookies := recorder.Result().Cookies()
		seenAuth := false
		seenCSRF := false
		for _, cookie := range cookies {
			seenAuth = seenAuth || cookie.Name == auth.AuthCookieName
			seenCSRF = seenCSRF || cookie.Name == auth.CSRFCookieName
		}
		if !seenAuth || !seenCSRF {
			t.Fatalf("local login cookies: auth=%v csrf=%v", seenAuth, seenCSRF)
		}
	}

	var users, workspaces, memberships int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM "user" WHERE email = $1`, localUserEmail).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, localWorkspaceSlug).Scan(&workspaces); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM member m
		JOIN "user" u ON u.id = m.user_id
		JOIN workspace w ON w.id = m.workspace_id
		WHERE u.email = $1 AND w.slug = $2
	`, localUserEmail, localWorkspaceSlug).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if users != 1 || workspaces != 1 || memberships != 1 {
		t.Fatalf("local bootstrap counts: users=%d workspaces=%d memberships=%d", users, workspaces, memberships)
	}
}

func TestLocalCredentialValidation(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		valid    bool
	}{
		{name: "strong", username: "xingyao", password: "Strong!LifeOS2026", valid: true},
		{name: "short", username: "xingyao", password: "Aa1!short", valid: false},
		{name: "weak classes", username: "xingyao", password: "alllowercasepassword", valid: false},
		{name: "contains username", username: "xingyao", password: "Xingyao!2026Strong", valid: false},
		{name: "invalid username", username: "星耀", password: "Strong!LifeOS2026", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, message := validateLocalCredential(tt.username, tt.password)
			if (message == "") != tt.valid {
				t.Fatalf("validateLocalCredential() message = %q, valid = %v", message, tt.valid)
			}
		})
	}
}

func TestDeriveLocalPasswordIsSaltedAndDeterministic(t *testing.T) {
	one := deriveLocalPassword("Strong!LifeOS2026", []byte("01234567890123456789012345678901"), 1000)
	two := deriveLocalPassword("Strong!LifeOS2026", []byte("01234567890123456789012345678901"), 1000)
	other := deriveLocalPassword("Strong!LifeOS2026", []byte("11234567890123456789012345678901"), 1000)
	if subtle.ConstantTimeCompare(one, two) != 1 {
		t.Fatal("same credential must derive the same digest")
	}
	if subtle.ConstantTimeCompare(one, other) == 1 {
		t.Fatal("different salts must derive different digests")
	}
}
