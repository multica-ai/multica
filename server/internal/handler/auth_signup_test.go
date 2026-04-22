package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func (m *mockDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

type mockRow struct {
	pgx.Row
	err error
}

func (m *mockRow) Scan(dest ...interface{}) error {
	return m.err
}

type scriptedDB struct {
	db.DBTX
	rows []pgx.Row
}

func (s *scriptedDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if len(s.rows) == 0 {
		return &scriptedRow{err: pgx.ErrNoRows}
	}
	row := s.rows[0]
	s.rows = s.rows[1:]
	return row
}

func (s *scriptedDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (s *scriptedDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 1"), nil
}

type scriptedRow struct {
	pgx.Row
	values []any
	err    error
}

func (s *scriptedRow) Scan(dest ...interface{}) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.values) {
		return pgx.ErrNoRows
	}
	for i := range dest {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			continue
		}
		target.Elem().Set(reflect.ValueOf(s.values[i]))
	}
	return nil
}

func scriptedUserValues(
	userID pgtype.UUID,
	name string,
	email string,
	avatar pgtype.Text,
	now pgtype.Timestamptz,
) []any {
	return []any{
		userID,
		name,
		email,
		avatar,
		now,
		now,
		pgtype.Timestamptz{},
		[]byte(nil),
		pgtype.Text{},
		pgtype.Text{},
		pgtype.Text{},
	}
}

func scriptedOwnerLookupValues(
	userID pgtype.UUID,
	name string,
	email string,
	avatar pgtype.Text,
	now pgtype.Timestamptz,
) []any {
	return []any{
		userID,
		name,
		email,
		avatar,
		now,
		now,
	}
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

func TestResolveTrustedBootstrapOwner_ResumesExistingSoleUser(t *testing.T) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	userID := parseUUID("11111111-1111-1111-1111-111111111111")
	avatar := pgtype.Text{}
	h := newTestHandler(Config{AllowSignup: true})
	h.DB = &scriptedDB{
		rows: []pgx.Row{
			&scriptedRow{values: []any{1}},
			&scriptedRow{values: scriptedOwnerLookupValues(userID, "Existing Owner", "owner@example.com", avatar, now)},
		},
	}
	h.Queries = db.New(h.DB)

	owner, err := h.resolveTrustedBootstrapOwner(context.Background())
	if err != nil {
		t.Fatalf("resolveTrustedBootstrapOwner: %v", err)
	}
	if owner.ownerResolution != trustedBootstrapOwnerResolutionOld {
		t.Fatalf("expected owner_resolution %q, got %q", trustedBootstrapOwnerResolutionOld, owner.ownerResolution)
	}
	if owner.user.Email != "owner@example.com" {
		t.Fatalf("expected resumed owner email, got %q", owner.user.Email)
	}
}

func TestResolveTrustedBootstrapOwner_CreatesOwnerWhenDatabaseIsEmpty(t *testing.T) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	userID := parseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	avatar := pgtype.Text{}
	h := newTestHandler(Config{AllowSignup: false})
	h.DB = &scriptedDB{
		rows: []pgx.Row{
			&scriptedRow{values: []any{0}},
			&scriptedRow{values: scriptedUserValues(userID, trustedBootstrapOwnerName, trustedBootstrapOwnerEmail, avatar, now)},
		},
	}
	h.Queries = db.New(h.DB)

	owner, err := h.resolveTrustedBootstrapOwner(context.Background())
	if err != nil {
		t.Fatalf("resolveTrustedBootstrapOwner: %v", err)
	}
	if owner.ownerResolution != trustedBootstrapOwnerResolutionNew {
		t.Fatalf("expected owner_resolution %q, got %q", trustedBootstrapOwnerResolutionNew, owner.ownerResolution)
	}
	if owner.user.Name != trustedBootstrapOwnerName || owner.user.Email != trustedBootstrapOwnerEmail {
		t.Fatalf("expected trusted bootstrap owner %q <%s>, got %q <%s>", trustedBootstrapOwnerName, trustedBootstrapOwnerEmail, owner.user.Name, owner.user.Email)
	}
}

func TestResolveTrustedBootstrapOwner_ReusesOwnerAfterConcurrentCreateRace(t *testing.T) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	userID := parseUUID("abababab-abab-abab-abab-abababababab")
	avatar := pgtype.Text{}
	h := newTestHandler(Config{AllowSignup: false})
	h.DB = &scriptedDB{
		rows: []pgx.Row{
			&scriptedRow{values: []any{0}},
			&scriptedRow{err: &pgconn.PgError{Code: "23505"}},
			&scriptedRow{values: scriptedUserValues(userID, trustedBootstrapOwnerName, trustedBootstrapOwnerEmail, avatar, now)},
		},
	}
	h.Queries = db.New(h.DB)

	owner, err := h.resolveTrustedBootstrapOwner(context.Background())
	if err != nil {
		t.Fatalf("resolveTrustedBootstrapOwner: %v", err)
	}
	if owner.ownerResolution != trustedBootstrapOwnerResolutionOld {
		t.Fatalf("expected owner_resolution %q after race recovery, got %q", trustedBootstrapOwnerResolutionOld, owner.ownerResolution)
	}
	if owner.user.Email != trustedBootstrapOwnerEmail {
		t.Fatalf("expected trusted owner email %q, got %q", trustedBootstrapOwnerEmail, owner.user.Email)
	}
}

func TestResolveTrustedBootstrapOwner_FailsClosedForMultiUserDatabase(t *testing.T) {
	h := newTestHandler(Config{AllowSignup: true})
	h.DB = &scriptedDB{
		rows: []pgx.Row{
			&scriptedRow{values: []any{2}},
		},
	}
	h.Queries = db.New(h.DB)

	_, err := h.resolveTrustedBootstrapOwner(context.Background())
	if err == nil {
		t.Fatal("expected trusted bootstrap conflict error")
	}
	var conflictErr trustedBootstrapConflictError
	if !strings.Contains(err.Error(), "multi-user") && !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("expected trusted bootstrap conflict, got %v", err)
	}
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected trustedBootstrapConflictError, got %T", err)
	}
}

func TestFindOrCreateUser_TrustedBootstrapRejectsAdditionalUser(t *testing.T) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	userID := parseUUID("22222222-2222-2222-2222-222222222222")
	avatar := pgtype.Text{}
	h := newTestHandler(Config{AllowSignup: true})
	h.DB = &scriptedDB{
		rows: []pgx.Row{
			&scriptedRow{err: pgx.ErrNoRows},
			&scriptedRow{values: []any{1}},
			&scriptedRow{values: scriptedOwnerLookupValues(userID, trustedBootstrapOwnerName, trustedBootstrapOwnerEmail, avatar, now)},
		},
	}
	h.Queries = db.New(h.DB)

	_, isNew, err := h.findOrCreateUser(context.Background(), "other@example.com")
	if err == nil {
		t.Fatal("expected additional-user rejection under trusted bootstrap")
	}
	if isNew {
		t.Fatal("expected isNew=false when trusted bootstrap rejects additional users")
	}
	if err != ErrTrustedBootstrapAdditionalUser {
		t.Fatalf("expected ErrTrustedBootstrapAdditionalUser, got %v", err)
	}
}

func TestFindOrCreateUser_TrustedBootstrapReturnsExistingOwnerWhenEmailMatches(t *testing.T) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	userID := parseUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	avatar := pgtype.Text{}
	h := newTestHandler(Config{AllowSignup: false})
	h.DB = &scriptedDB{
		rows: []pgx.Row{
			&scriptedRow{err: pgx.ErrNoRows},
			&scriptedRow{values: []any{1}},
			&scriptedRow{values: scriptedOwnerLookupValues(userID, trustedBootstrapOwnerName, trustedBootstrapOwnerEmail, avatar, now)},
		},
	}
	h.Queries = db.New(h.DB)

	user, isNew, err := h.findOrCreateUser(context.Background(), trustedBootstrapOwnerEmail)
	if err != nil {
		t.Fatalf("findOrCreateUser: %v", err)
	}
	if isNew != false {
		t.Fatalf("expected isNew=false for resumed trusted owner, got %v", isNew)
	}
	if user.Email != trustedBootstrapOwnerEmail {
		t.Fatalf("expected trusted owner email %q, got %q", trustedBootstrapOwnerEmail, user.Email)
	}
}
