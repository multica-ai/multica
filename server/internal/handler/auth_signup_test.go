package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTestHandler(cfg Config) *Handler {
	return &Handler{
		cfg: cfg,
	}
}

func TestSignupGating(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		email string
		isNew bool
		want  error
	}{
		{"allow_signup_true_new", Config{AllowSignup: true}, "a@x.com", true, nil},
		{"allow_signup_false_new", Config{AllowSignup: false}, "a@x.com", true, ErrSignupProhibited},
		{"allow_signup_false_existing", Config{AllowSignup: false}, "a@x.com", false, nil},
		{"domain_allowlist_match", Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}}, "user@company.com", true, nil},
		{"domain_allowlist_mismatch_signup_disabled", Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}}, "user@other.com", true, ErrSignupProhibited},
		{"domain_allowlist_mismatch_signup_enabled", Config{AllowSignup: true, AllowedEmailDomains: []string{"company.com"}}, "user@other.com", true, ErrEmailNotAllowed},
		{"email_allowlist_match", Config{AllowSignup: false, AllowedEmails: []string{"boss@x.com"}}, "boss@x.com", true, nil},
		{"email_allowlist_mismatch_signup_enabled", Config{AllowSignup: true, AllowedEmails: []string{"boss@x.com"}}, "user@other.com", true, ErrEmailNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(tt.cfg)
			err := h.checkSignupAllowed(tt.email, tt.isNew)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got err=%v want=%v", err, tt.want)
			}
		})
	}
}

func TestEmailCodeAllowlistErrors(t *testing.T) {
	for _, path := range []string{"send-code", "verify-code"} {
		t.Run(path, func(t *testing.T) {
			email := path + "-allowlist-regression@example.com"
			h := newTestHandler(Config{AllowSignup: true, AllowedEmailDomains: []string{"company.com"}})
			h.Queries = testHandler.Queries
			dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
			body := map[string]string{"email": email}
			handler := h.SendCode
			if path == "verify-code" {
				// A code can remain valid after the instance's signup policy changes.
				body["code"] = "123456"
				dbfx.Insert(t, "verification_code", testutil.Cols{
					"email":      email,
					"code":       body["code"],
					"expires_at": testutil.Raw("now() + interval '10 minutes'"),
				})
				handler = h.VerifyCode
			}
			req := testutil.JSONRequest(http.MethodPost, "/auth/"+path, body)
			resp := testutil.Call(t, handler, req).Want(http.StatusForbidden)
			got := resp.Map()
			if got["error"] != ErrEmailNotAllowed.Error() {
				t.Fatalf("expected an actionable allowlist error, got %v", got)
			}
			if _, hasCode := got["code"]; hasCode {
				t.Fatal("email-code errors must retain their existing response shape")
			}
			if len(resp.Result().Cookies()) != 0 {
				t.Fatal("rejected signup must not establish an authenticated session")
			}
			if count := dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email); count != 0 {
				t.Fatalf("rejected signup created %d users", count)
			}
		})
	}
}

type mockDB struct {
	db.DBTX
	getUserErr error
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return &mockRow{err: m.getUserErr}
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 1"), nil
}

type mockRow struct {
	pgx.Row
	err error
}

func (m *mockRow) Scan(dest ...interface{}) error {
	return m.err
}

func TestFindOrCreateUserGating(t *testing.T) {
	t.Run("new_user_blocked", func(t *testing.T) {
		cfg := Config{AllowSignup: false}
		h := newTestHandler(cfg)
		h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

		_, isNew, err := h.findOrCreateUser(context.Background(), "new@blocked.com")
		if err == nil {
			t.Fatal("expected error for new user when signup disabled")
		}
		if isNew {
			t.Fatal("isNew should be false when signup is blocked")
		}
		if !strings.Contains(err.Error(), "registration is disabled") {
			t.Fatalf("expected registration disabled error, got %v", err)
		}
	})

	t.Run("existing_user_allowed", func(t *testing.T) {
		cfg := Config{AllowSignup: false}
		h := newTestHandler(cfg)
		// mockDB returns nil error for Scan, simulating user found
		h.Queries = db.New(&mockDB{getUserErr: nil})

		_, isNew, err := h.findOrCreateUser(context.Background(), "existing@test.com")
		if err != nil {
			t.Fatalf("expected no error for existing user, got %v", err)
		}
		if isNew {
			t.Fatal("existing user should not be flagged as new")
		}
	})

	t.Run("whitelisted_user_allowed", func(t *testing.T) {
		cfg := Config{AllowSignup: false, AllowedEmails: []string{"whitelisted@test.com"}}
		h := newTestHandler(cfg)
		h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

		// This will pass checkSignupAllowed and move to CreateUser.
		// Our mockDB Exec returns success, but Queries.CreateUser might expect QueryRow for RETURNING id.
		// Let's see if it works.
		_, _, err := h.findOrCreateUser(context.Background(), "whitelisted@test.com")
		if err != nil && strings.Contains(err.Error(), "registration is disabled") {
			t.Fatalf("expected whitelisted user to pass signup check, but got %v", err)
		}
	})
}
