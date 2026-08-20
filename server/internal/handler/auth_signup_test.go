package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTestHandler(cfg Config) *Handler {
	return &Handler{
		cfg: cfg,
	}
}

func TestSignupGating(t *testing.T) {
	tests := []struct {
		name              string
		cfg               Config
		email             string
		isNew             bool
		pendingInvitation bool
		wantErr           bool
	}{
		{name: "allow_signup_true_new", cfg: Config{AllowSignup: true}, email: "a@x.com", isNew: true},
		{name: "allow_signup_false_new", cfg: Config{AllowSignup: false}, email: "a@x.com", isNew: true, wantErr: true},
		{name: "allow_signup_false_existing", cfg: Config{AllowSignup: false}, email: "a@x.com"},
		{name: "domain_allowlist_match", cfg: Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}}, email: "user@company.com", isNew: true},
		{name: "domain_allowlist_mismatch", cfg: Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}}, email: "user@other.com", isNew: true, wantErr: true},
		{name: "email_allowlist_match", cfg: Config{AllowSignup: false, AllowedEmails: []string{"boss@x.com"}}, email: "boss@x.com", isNew: true},
		{name: "pending_invitation_match", cfg: Config{AllowSignup: false}, email: "invited@x.com", isNew: true, pendingInvitation: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(tt.cfg)
			h.Queries = db.New(&mockDB{pendingInvitation: tt.pendingInvitation})
			err := h.checkSignupAllowed(context.Background(), tt.email, tt.isNew)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err=%v wantErr=%v", err, tt.wantErr)
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
	getUserErr           error
	pendingInvitation    bool
	pendingInvitationErr error
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, "FROM workspace_invitation") {
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
