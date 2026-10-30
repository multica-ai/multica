package handler

import (
	"context"
	"errors"
	"fmt"
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
			h.Queries = db.New(&mockDB{})
			err := h.checkSignupAllowed(context.Background(), tt.email, tt.isNew)
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

func TestSignupGatingReturnsPendingInvitationLookupError(t *testing.T) {
	h := newTestHandler(Config{AllowSignup: false})
	h.Queries = db.New(&mockDB{pendingInvitationErr: context.Canceled})

	err := h.checkSignupAllowed(context.Background(), "invited@x.com", true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err=%v, want context canceled", err)
	}
}

type mockDB struct {
	db.DBTX
	invitationLookups    int
	invitationEmail      string
	getUserErr           error
	pendingInvitation    bool
	pendingInvitationErr error
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.HasPrefix(sql, "-- name: HasPendingInvitationForEmail :one\n") {
		m.invitationLookups++
		m.invitationEmail = args[0].(string)
		return &mockRow{err: m.pendingInvitationErr, boolValue: &m.pendingInvitation}
	}
	return &mockRow{err: m.getUserErr}
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 1"), nil
}

type mockRow struct {
	pgx.Row
	err       error
	boolValue *bool
}

func (m *mockRow) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	if m.boolValue != nil {
		value, ok := dest[0].(*bool)
		if !ok {
			return fmt.Errorf("expected *bool destination, got %T", dest[0])
		}
		*value = *m.boolValue
	}
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

// Invitations are explicit per-email exceptions for both signup flag values.
func TestSignupInvitationAllowlistInteraction(t *testing.T) {
	for _, allowSignup := range []bool{false, true} {
		for _, allowlist := range []string{"none", "email", "domain", "both"} {
			for _, invited := range []bool{false, true} {
				t.Run(fmt.Sprintf("signup=%t/allowlist=%s/invited=%t", allowSignup, allowlist, invited), func(t *testing.T) {
					cfg := Config{AllowSignup: allowSignup}
					if allowlist == "email" || allowlist == "both" {
						cfg.AllowedEmails = []string{"boss@company.com"}
					}
					if allowlist == "domain" || allowlist == "both" {
						cfg.AllowedEmailDomains = []string{"company.com"}
					}
					mock := &mockDB{pendingInvitation: invited}
					h := newTestHandler(cfg)
					h.Queries = db.New(mock)
					err := h.checkSignupAllowed(context.Background(), "OUTSIDER@OTHER.COM", true)
					var want error
					openSignup := allowSignup && allowlist == "none"
					if !invited && !openSignup {
						want = ErrSignupProhibited
						if allowSignup {
							want = ErrEmailNotAllowed
						}
					}
					if !errors.Is(err, want) {
						t.Fatalf("got %v, want %v", err, want)
					}
					if !openSignup && (mock.invitationLookups != 1 || mock.invitationEmail != "outsider@other.com") {
						t.Fatalf("expected one normalized email lookup, got %d for %q", mock.invitationLookups, mock.invitationEmail)
					}
				})
			}
		}
	}
}

func TestSignupSkipsUnnecessaryInvitationLookup(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cfg   Config
		isNew bool
	}{
		{"existing", Config{}, false},
		{"open", Config{AllowSignup: true}, true},
		{"email_match", Config{AllowedEmails: []string{"USER@COMPANY.COM"}}, true},
		{"domain_match", Config{AllowedEmailDomains: []string{"COMPANY.COM"}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDB{pendingInvitationErr: context.Canceled}
			h := newTestHandler(tt.cfg)
			h.Queries = db.New(mock)
			if err := h.checkSignupAllowed(context.Background(), "user@company.com", tt.isNew); err != nil {
				t.Fatal(err)
			}
			if mock.invitationLookups != 0 {
				t.Fatalf("unexpected invitation lookups: %d", mock.invitationLookups)
			}
		})
	}
}
