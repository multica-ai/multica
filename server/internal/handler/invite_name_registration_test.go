package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mockInviteDB extends mockDB with control over GetLatestPendingInvitationNameByEmail.
// The base mockDB.QueryRow returns the same error for all queries, which is too
// coarse — we need GetUserByEmail to return "not found" (new user) while
// GetLatestPendingInvitationNameByEmail returns an invite name.
// We achieve this by tracking query call order: first QueryRow call is
// GetUserByEmail, second is GetLatestPendingInvitationNameByEmail.
type mockInviteDB struct {
	db.DBTX
	getUserResult  db.User
	getUserErr     error
	inviteeName    string // empty string = no invite name
	queryCallCount int
}

func (m *mockInviteDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	m.queryCallCount++
	switch m.queryCallCount {
	case 1:
		// GetUserByEmail
		return &mockInviteRow{err: m.getUserErr, user: m.getUserResult}
	case 2:
		// GetLatestPendingInvitationNameByEmail
		if m.inviteeName != "" {
			return &mockTextRow{val: pgtype.Text{String: m.inviteeName, Valid: true}}
		}
		return &mockTextRow{err: pgx.ErrNoRows}
	default:
		// CreateUser — return a minimal user
		return &mockInviteRow{user: db.User{Name: "created"}}
	}
}

type mockInviteRow struct {
	pgx.Row
	err  error
	user db.User
}

func (m *mockInviteRow) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	// Scan the user fields in column order matching CreateUser / GetUserByEmail
	// RETURNING: id, name, email, avatar_url, onboarded_at, ...
	// For our purposes we only care that Scan returns nil (success).
	return nil
}

type mockTextRow struct {
	pgx.Row
	err error
	val pgtype.Text
}

func (m *mockTextRow) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	if len(dest) > 0 {
		if t, ok := dest[0].(*pgtype.Text); ok {
			*t = m.val
		}
	}
	return nil
}

// TestFindOrCreateUser_InviteName_NewUser verifies that a brand-new user gets
// the invitee_name from a pending invitation instead of the email prefix.
// This is the core AE1/AE5 behaviour at the findOrCreateUser level.
//
// NOTE: This test is written BEFORE the implementation is changed
// (test-first, per the plan's Execution note). It will fail until
// findOrCreateUser is updated to call GetLatestPendingInvitationNameByEmail.
func TestFindOrCreateUser_InviteName_NewUser(t *testing.T) {
	cfg := Config{AllowSignup: true}
	h := newTestHandler(cfg)
	h.Queries = db.New(&mockInviteDB{
		getUserErr:  pgx.ErrNoRows, // new user
		inviteeName: "潘纪元",
	})

	_, isNew, hadInviteName, err := h.findOrCreateUser(context.Background(), "jiyuan.pan@upai.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true for a new user")
	}
	if !hadInviteName {
		t.Error("expected hadInviteName=true when a pending invite has invitee_name")
	}
}

// TestFindOrCreateUser_NoInviteName_NewUser verifies that without an invite name,
// a new user gets the email prefix (AE2 / R3 backward-compatibility).
func TestFindOrCreateUser_NoInviteName_NewUser(t *testing.T) {
	cfg := Config{AllowSignup: true}
	h := newTestHandler(cfg)
	h.Queries = db.New(&mockInviteDB{
		getUserErr:  pgx.ErrNoRows, // new user
		inviteeName: "",             // no invite name
	})

	_, _, hadInviteName, err := h.findOrCreateUser(context.Background(), "newuser@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hadInviteName {
		t.Error("expected hadInviteName=false when no invite name is set")
	}
}

// TestFindOrCreateUser_ExistingUser_NoInviteName verifies that an existing user
// returning via login never gets hadInviteName=true (not the create branch).
func TestFindOrCreateUser_ExistingUser_NoInviteName(t *testing.T) {
	cfg := Config{AllowSignup: true}
	h := newTestHandler(cfg)
	h.Queries = db.New(&mockInviteDB{
		getUserErr:  nil, // existing user — Scan returns nil = found
		inviteeName: "潘纪元",
	})

	_, isNew, hadInviteName, err := h.findOrCreateUser(context.Background(), "jiyuan.pan@upai.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isNew {
		t.Error("expected isNew=false for existing user")
	}
	if hadInviteName {
		t.Error("expected hadInviteName=false for existing user (create branch not taken)")
	}
}
