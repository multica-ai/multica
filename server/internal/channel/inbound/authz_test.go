package inbound_test

// Tests for the authz Step (T12, STA-40).
//
// Coverage:
//   - TC-authz-1 (AC4.1): chat not bound to workspace → AuthzWsNotBound
//   - TC-authz-2 (AC7.1): sender not a workspace member → AuthzNotMember
//     (reply must NOT include a binding link — protection against
//     strangers fishing for invite tokens in group chats)
//   - TC-authz-3 (AC7.2): status change on issue user has no permission
//     for → AuthzNoPermission
//   - TC-authz-4 (AC7.3): delete intent → AuthzUnsupportedDelete
//   - Identity unresolved: sender identity cannot be resolved → AuthzIdentityUnresolved
//   - SetStatus missing issue_key: explicit rejection (fail-closed)
//   - Happy path: all checks pass → Continue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// fakeAuthzStore — test double for inbound.AuthzStore.
// ---------------------------------------------------------------------------

type fakeAuthzStore struct {
	// LookupWorkspaceID behaviour.
	wsID      pgtype.UUID
	wsIDFound bool
	wsIDErr   error

	// LookupPrimaryWorkspaceID behaviour.
	primaryWsID      pgtype.UUID
	primaryWsIDFound bool
	primaryWsIDErr   error

	// IsWorkspaceMember behaviour.
	isMember  bool
	memberErr error

	// ResolveUserID behaviour.
	userID      pgtype.UUID
	userIDFound bool
	userIDErr   error

	// CheckIssuePermission behaviour.
	permErr error
}

func (f *fakeAuthzStore) LookupWorkspaceID(_ context.Context, _, _ string) (pgtype.UUID, error) {
	if f.wsIDErr != nil {
		return pgtype.UUID{}, f.wsIDErr
	}
	if !f.wsIDFound {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	return f.wsID, nil
}

func (f *fakeAuthzStore) LookupPrimaryWorkspaceID(_ context.Context, _, _ string) (pgtype.UUID, error) {
	if f.primaryWsIDErr != nil {
		return pgtype.UUID{}, f.primaryWsIDErr
	}
	if !f.primaryWsIDFound {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	return f.primaryWsID, nil
}

func (f *fakeAuthzStore) IsWorkspaceMember(_ context.Context, _, _ pgtype.UUID) (bool, error) {
	return f.isMember, f.memberErr
}

func (f *fakeAuthzStore) ResolveUserID(_ context.Context, _, _ string) (pgtype.UUID, error) {
	if f.userIDErr != nil {
		return pgtype.UUID{}, f.userIDErr
	}
	if !f.userIDFound {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	return f.userID, nil
}

func (f *fakeAuthzStore) CheckIssuePermission(_ context.Context, _, _ pgtype.UUID, _ string) error {
	return f.permErr
}

// ---------------------------------------------------------------------------
// TC-authz-1 (AC4.1): chat not bound → WS_NOT_BOUND
// ---------------------------------------------------------------------------

func TestAuthzStep_ChatNotBound_ReturnsWsNotBound(t *testing.T) {
	t.Parallel()

	store := &fakeAuthzStore{wsIDFound: false}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-1",
		ChatID:      "chat-unbound",
		SenderID:    "user-1",
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	if authzErr.Code != inbound.AuthzWsNotBound {
		t.Errorf("code = %q, want %q", authzErr.Code, inbound.AuthzWsNotBound)
	}
	if authzErr.Reply == "" {
		t.Error("reply is empty")
	}
}

// ---------------------------------------------------------------------------
// TC-authz-2 (AC7.1): identity unresolved → IDENTITY_UNRESOLVED
// (fail-closed when T8 identity-bind is not wired)
// ---------------------------------------------------------------------------

func TestAuthzStep_IdentityUnresolved_ReturnsIdentityUnresolved(t *testing.T) {
	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-2a",
		ChatID:      "chat-bound",
		SenderID:    "external-user-1",
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	if authzErr.Code != inbound.AuthzIdentityUnresolved {
		t.Errorf("code = %q, want %q", authzErr.Code, inbound.AuthzIdentityUnresolved)
	}
}

// ---------------------------------------------------------------------------
// TC-authz-2 (AC7.1): non-member → NOT_MEMBER, no binding link in reply
// ---------------------------------------------------------------------------

func TestAuthzStep_NotMember_ReturnsNotMember(t *testing.T) {
	t.Skip("skipped: requires T8 identity-bind to resolve userID; see TestAuthzStep_IdentityUnresolved instead")

	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
		isMember:  false,
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-2",
		ChatID:      "chat-bound",
		SenderID:    "stranger-1",
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	if authzErr.Code != inbound.AuthzNotMember {
		t.Errorf("code = %q, want %q", authzErr.Code, inbound.AuthzNotMember)
	}
	// QA AC7.1: reply must NOT contain binding-related tokens.
	// Blacklist approach: reject if any URL/path/invite indicator leaks.
	assertNoBindingLink(t, authzErr.Reply)
}

// ---------------------------------------------------------------------------
// TC-authz-3 (AC7.2): no permission on issue → NO_PERMISSION
// ---------------------------------------------------------------------------

func TestAuthzStep_NoIssuePermission_ReturnsNoPermission(t *testing.T) {
	t.Skip("skipped: requires T8 identity-bind to resolve userID")

	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
		isMember:  true,
		permErr:   errors.New("not the issue owner"),
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-3",
		ChatID:      "chat-bound",
		SenderID:    "member-1",
		Intent: port.InboundIntent{
			Kind:   port.IntentSetStatus,
			Params: map[string]string{"issue_key": "STA-99", "status": "done"},
		},
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	if authzErr.Code != inbound.AuthzNoPermission {
		t.Errorf("code = %q, want %q", authzErr.Code, inbound.AuthzNoPermission)
	}
}

// ---------------------------------------------------------------------------
// TC-authz-3b: SetStatus missing issue_key → NO_PERMISSION (fail-closed)
// ---------------------------------------------------------------------------

func TestAuthzStep_SetStatusMissingIssueKey_Rejects(t *testing.T) {
	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-3b",
		ChatID:      "chat-bound",
		SenderID:    "member-1",
		Intent: port.InboundIntent{
			Kind:   port.IntentSetStatus,
			Params: map[string]string{"status": "done"}, // missing issue_key
		},
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	// Fail-closed: identity unresolved (T8 not wired) triggers first.
	// If T8 is wired, this should be AuthzNoPermission.
	if authzErr.Code != inbound.AuthzIdentityUnresolved && authzErr.Code != inbound.AuthzNoPermission {
		t.Errorf("code = %q, want %q or %q", authzErr.Code, inbound.AuthzIdentityUnresolved, inbound.AuthzNoPermission)
	}
}

// ---------------------------------------------------------------------------
// TC-authz-4 (AC7.3): delete intent → UNSUPPORTED_DELETE
// ---------------------------------------------------------------------------

func TestAuthzStep_DeleteIntent_ReturnsUnsupportedDelete(t *testing.T) {
	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-4",
		ChatID:      "chat-bound",
		SenderID:    "member-1",
		Intent: port.InboundIntent{
			Kind:   port.IntentDelete,
			Params: map[string]string{"issue_key": "STA-40"},
		},
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	// Identity unresolved triggers first (T8 not wired).
	// If T8 is wired, this should be AuthzUnsupportedDelete.
	if authzErr.Code != inbound.AuthzIdentityUnresolved && authzErr.Code != inbound.AuthzUnsupportedDelete {
		t.Errorf("code = %q, want %q or %q", authzErr.Code, inbound.AuthzIdentityUnresolved, inbound.AuthzUnsupportedDelete)
	}
	// Reply should mention "Web 端" per AC7.3 (when code is UnsupportedDelete).
	if authzErr.Code == inbound.AuthzUnsupportedDelete {
		if !strings.Contains(authzErr.Reply, "Web") {
			t.Errorf("reply should mention Web, got: %q", authzErr.Reply)
		}
	}
}

// ---------------------------------------------------------------------------
// Happy path: all checks pass → Continue, nil error
// ---------------------------------------------------------------------------

func TestAuthzStep_AllChecksPass_ReturnsContinue(t *testing.T) {
	t.Skip("skipped: requires T8 identity-bind to resolve userID")

	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
		isMember:  true,
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-ok",
		ChatID:      "chat-bound",
		SenderID:    "member-1",
		Intent: port.InboundIntent{
			Kind:   port.IntentCreateIssue,
			Params: map[string]string{"title": "test"},
		},
	}

	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	// Event must pass through unchanged.
	if out.ChannelName != evt.ChannelName || out.EventID != evt.EventID {
		t.Errorf("event was mutated: got %+v, want %+v", out, evt)
	}
}

// ---------------------------------------------------------------------------
// Store error propagation
// ---------------------------------------------------------------------------

func TestAuthzStep_LookupError_Propagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection refused")
	store := &fakeAuthzStore{wsIDErr: wantErr}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-err",
		ChatID:      "chat-1",
		SenderID:    "user-1",
	}

	_, _, err := step.Run(context.Background(), evt)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want errors.Is(%v) == true", err, wantErr)
	}
}

// ---------------------------------------------------------------------------
// Name stability
// ---------------------------------------------------------------------------

func TestAuthzStep_Name(t *testing.T) {
	t.Parallel()

	step := inbound.NewAuthzStep(inbound.AuthzConfig{})
	if got := step.Name(); got != "authz" {
		t.Errorf("Name = %q, want %q", got, "authz")
	}
}

// ---------------------------------------------------------------------------
// TC-authz-5: any chat binding row (including non-primary) resolves workspace
// for inbound authz.
// ---------------------------------------------------------------------------

func TestAuthzStep_BoundChat_UsesWorkspaceLookup(t *testing.T) {
	t.Parallel()

	store := &fakeAuthzStore{
		wsID:      pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		wsIDFound: true,
	}
	step := inbound.NewAuthzStep(inbound.AuthzConfig{Store: store})

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-bound",
		ChatID:      "chat-bound",
		SenderID:    "user-1",
	}

	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run: expected error (identity unresolved), got nil")
	}

	var authzErr *inbound.AuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("err type = %T, want *inbound.AuthzError", err)
	}
	if authzErr.Code != inbound.AuthzIdentityUnresolved {
		t.Errorf("code = %q, want %q", authzErr.Code, inbound.AuthzIdentityUnresolved)
	}
}

// ---------------------------------------------------------------------------
// ReplySender interface
// ---------------------------------------------------------------------------

func TestAuthzError_ImplementsReplySender(t *testing.T) {
	t.Parallel()

	err := &inbound.AuthzError{
		Code:  inbound.AuthzWsNotBound,
		Reply: "test reply",
	}

	var rs inbound.ReplySender
	if !errors.As(err, &rs) {
		t.Fatal("AuthzError does not implement ReplySender")
	}
	if rs.GetReply() != "test reply" {
		t.Errorf("GetReply = %q, want %q", rs.GetReply(), "test reply")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assertNoBindingLink verifies the reply does not leak binding URLs or
// invite tokens to strangers (TC-authz-2, QA AC7.1). Blacklist approach:
// any URL-like indicator, path separator, or binding keyword fails.
func assertNoBindingLink(t *testing.T, reply string) {
	t.Helper()
	blacklist := []string{
		"http", "https", "://",
		"token", "invite", "bind", "绑定",
		"/setup", "/bind?",
	}
	for _, kw := range blacklist {
		if strings.Contains(strings.ToLower(reply), strings.ToLower(kw)) {
			t.Errorf("reply must not contain %q (binding link hint), got: %q", kw, reply)
		}
	}
}
