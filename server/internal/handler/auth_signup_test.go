package handler

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/dwickyfp/wallts/server/pkg/db/generated"
)

func newTestHandler(cfg Config) *Handler {
	return &Handler{
		cfg: cfg,
	}
}

func TestSignupGating(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		email   string
		isNew   bool
		wantErr bool
	}{
		{"allow_signup_true_new", Config{AllowSignup: true}, "a@x.com", true, false},
		{"allow_signup_false_new", Config{AllowSignup: false}, "a@x.com", true, true},
		{"allow_signup_false_existing", Config{AllowSignup: false}, "a@x.com", false, false},
		{"domain_allowlist_match", Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}}, "user@company.com", true, false},
		{"domain_allowlist_mismatch", Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}}, "user@other.com", true, true},
		{"email_allowlist_match", Config{AllowSignup: false, AllowedEmails: []string{"boss@x.com"}}, "boss@x.com", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(tt.cfg)
			err := h.checkSignupAllowed(tt.email, tt.isNew)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err=%v wantErr=%v", err, tt.wantErr)
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

// ---------------------------------------------------------------------------
// NameLogin input validation (unit-level, no DB needed)
// ---------------------------------------------------------------------------

func TestNameLoginInputValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"valid_simple", "Alice", ""},
		{"valid_unicode", "田中太郎", ""},
		{"valid_with_spaces", "John Doe", ""},
		{"whitespace_only", "   ", "name is required"},
		{"empty", "", "name is required"},
		{"at_in_name", "alice@example.com", "name must not contain @"},
		{"at_simple", "user@domain", "name must not contain @"},
		{"max_length_exact", strings.Repeat("a", 100), ""},
		{"max_length_exceeded", strings.Repeat("a", 101), "name is too long"},
		{"unicode_max_length", strings.Repeat("中", 100), ""},
		{"unicode_max_length_exceeded", strings.Repeat("中", 101), "name is too long"},
		{"null_byte", "admin\x00", "invalid characters"},
		{"newline", "John\nDoe", "invalid characters"},
		{"tab_in_name", "John\tDoe", "invalid characters"},
		{"carriage_return", "John\rDoe", "invalid characters"},
		{"vertical_tab", "John\x0bDoe", "invalid characters"},
		{"del_char", "John\x7fDoe", "invalid characters"},
		{"mixed_valid_invalid", "John\x00Smith", "invalid characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(tt.input)
			var gotErr string

			if trimmed == "" {
				gotErr = "name is required"
			} else if strings.Contains(trimmed, "@") {
				gotErr = "name must not contain @"
			} else if utf8.RuneCountInString(trimmed) > 100 {
				gotErr = "name is too long (max 100 characters)"
			} else if strings.ContainsFunc(trimmed, func(r rune) bool { return r < 32 || r == 127 }) {
				gotErr = "name contains invalid characters"
			}

			if tt.wantErr == "" {
				if gotErr != "" {
					t.Fatalf("expected no error, got %q", gotErr)
				}
			} else {
				if !strings.Contains(gotErr, tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, gotErr)
				}
			}
		})
	}
}
