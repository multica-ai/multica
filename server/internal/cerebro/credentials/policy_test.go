package credentials

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// ---------------------------------------------------------------------------
// PolicyChecker unit tests (no DB) — exercise the in-package primitives so
// they stay correct without depending on the integration harness.
// ---------------------------------------------------------------------------

func TestChainPolicyChecker_FirstAllowWins(t *testing.T) {
	chain := NewChainPolicyChecker(
		PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Deny("nope") }),
		PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Allow("yes") }),
		PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Deny("never reached") }),
	)
	dec := chain.Check(context.Background(), PolicyRequest{Permission: PermReveal})
	if !dec.Allowed {
		t.Fatalf("expected allow, got %+v", dec)
	}
	if dec.Reason != "yes" {
		t.Fatalf("expected reason from middle checker, got %q", dec.Reason)
	}
}

func TestChainPolicyChecker_LastDenyReturned(t *testing.T) {
	chain := NewChainPolicyChecker(
		PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Deny("first") }),
		PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Deny("second") }),
	)
	dec := chain.Check(context.Background(), PolicyRequest{Permission: PermReveal})
	if dec.Allowed {
		t.Fatalf("expected deny, got %+v", dec)
	}
	if dec.Reason != "second" {
		t.Fatalf("expected most-specific deny reason, got %q", dec.Reason)
	}
}

func TestChainPolicyChecker_EmptyChainDeniesByDefault(t *testing.T) {
	chain := NewChainPolicyChecker()
	dec := chain.Check(context.Background(), PolicyRequest{Permission: PermReveal})
	if dec.Allowed {
		t.Fatalf("empty chain should deny, got %+v", dec)
	}
}

func TestChainPolicyChecker_SkipsNilCheckers(t *testing.T) {
	chain := NewChainPolicyChecker(
		nil,
		PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision { return Allow("ok") }),
	)
	dec := chain.Check(context.Background(), PolicyRequest{Permission: PermReveal})
	if !dec.Allowed {
		t.Fatalf("expected allow despite nil entries, got %+v", dec)
	}
}

func TestPolicyDeniedError_WrapsErrPolicyDenied(t *testing.T) {
	err := &PolicyDeniedError{Permission: PermReveal, Decision: Deny("no grant")}
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("errors.Is(err, ErrPolicyDenied) = false; want true")
	}
	if msg := err.Error(); msg == "" {
		t.Fatalf("PolicyDeniedError.Error() should not be empty")
	}
}

// ---------------------------------------------------------------------------
// OwnerPolicyChecker — exercise role mapping with a stub MemberLookup.
// ---------------------------------------------------------------------------

type stubMemberLookup struct {
	role string
	err  error
}

func (s *stubMemberLookup) GetMemberRole(_ context.Context, _, _ pgtype.UUID) (string, error) {
	return s.role, s.err
}

func TestOwnerPolicyChecker_OwnerAllows(t *testing.T) {
	checker := NewOwnerPolicyChecker(&stubMemberLookup{role: "owner"})
	dec := checker.Check(context.Background(), PolicyRequest{
		ActorType: "member",
		ActorID:   uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
	})
	if !dec.Allowed {
		t.Fatalf("owner should be allowed, got %+v", dec)
	}
}

func TestOwnerPolicyChecker_AdminAllows(t *testing.T) {
	checker := NewOwnerPolicyChecker(&stubMemberLookup{role: "admin"})
	dec := checker.Check(context.Background(), PolicyRequest{
		ActorType: "member",
		ActorID:   uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
	})
	if !dec.Allowed {
		t.Fatalf("admin should be allowed, got %+v", dec)
	}
}

func TestOwnerPolicyChecker_MemberRoleDenied(t *testing.T) {
	checker := NewOwnerPolicyChecker(&stubMemberLookup{role: "member"})
	dec := checker.Check(context.Background(), PolicyRequest{
		ActorType: "member",
		ActorID:   uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
	})
	if dec.Allowed {
		t.Fatalf("regular member should be denied, got %+v", dec)
	}
}

func TestOwnerPolicyChecker_AgentNeverOwner(t *testing.T) {
	checker := NewOwnerPolicyChecker(&stubMemberLookup{role: "owner"})
	dec := checker.Check(context.Background(), PolicyRequest{
		ActorType: "agent",
		ActorID:   uuidFromString(t, "22222222-2222-2222-2222-222222222222"),
	})
	if dec.Allowed {
		t.Fatalf("agent must not pass owner check, got %+v", dec)
	}
}

// ---------------------------------------------------------------------------
// permissionToAuditAction — mapping kept in one place; verify it.
// ---------------------------------------------------------------------------

func TestPermissionToAuditAction(t *testing.T) {
	cases := []struct {
		perm Permission
		want Action
	}{
		{PermAttach, ActionBind},
		{PermReadRedacted, ActionRead},
		{PermReveal, ActionReveal},
		{PermRotate, ActionRotate},
		{PermRevoke, ActionDelete},
	}
	for _, c := range cases {
		if got := permissionToAuditAction(c.perm); got != c.want {
			t.Errorf("permissionToAuditAction(%s) = %q; want %q", c.perm, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func uuidFromString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return id
}
