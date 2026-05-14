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
// PersonaPolicyChecker — id-scoped first, then type-scoped fallback.
// ---------------------------------------------------------------------------

type stubPersona struct {
	allowResource string
	calls         []string
}

func (s *stubPersona) CheckCredential(_ context.Context, action, resource, _ string) PolicyDecision {
	s.calls = append(s.calls, action+"|"+resource)
	if resource == s.allowResource {
		return Allow("persona allow")
	}
	return Deny("persona deny")
}

func TestPersonaPolicyChecker_IDScopeAllowsWithoutFallback(t *testing.T) {
	credID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	stub := &stubPersona{allowResource: "cerebro-credential:" + util.UUIDToString(credID)}
	checker := NewPersonaPolicyChecker(stub)

	dec := checker.Check(context.Background(), PolicyRequest{
		WorkspaceID:    uuidFromString(t, "44444444-4444-4444-4444-444444444444"),
		CredentialID:   credID,
		CredentialType: TypeAPIKey,
		Permission:     PermReveal,
		ActorType:      "agent",
		ActorID:        uuidFromString(t, "55555555-5555-5555-5555-555555555555"),
	})
	if !dec.Allowed {
		t.Fatalf("expected allow on id-scope, got %+v", dec)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected exactly one persona call (id-scope), got %d: %v", len(stub.calls), stub.calls)
	}
}

func TestPersonaPolicyChecker_TypeFallback(t *testing.T) {
	credID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	stub := &stubPersona{allowResource: "cerebro-credential-type:api_key"}
	checker := NewPersonaPolicyChecker(stub)

	dec := checker.Check(context.Background(), PolicyRequest{
		WorkspaceID:    uuidFromString(t, "44444444-4444-4444-4444-444444444444"),
		CredentialID:   credID,
		CredentialType: TypeAPIKey,
		Permission:     PermReveal,
		ActorType:      "agent",
		ActorID:        uuidFromString(t, "55555555-5555-5555-5555-555555555555"),
	})
	if !dec.Allowed {
		t.Fatalf("expected allow via type fallback, got %+v", dec)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("expected id-scope then type-fallback (2 calls), got %d: %v", len(stub.calls), stub.calls)
	}
}

func TestPersonaPolicyChecker_BothDeny(t *testing.T) {
	credID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	stub := &stubPersona{allowResource: "never-matches"}
	checker := NewPersonaPolicyChecker(stub)

	dec := checker.Check(context.Background(), PolicyRequest{
		WorkspaceID:    uuidFromString(t, "44444444-4444-4444-4444-444444444444"),
		CredentialID:   credID,
		CredentialType: TypeAPIKey,
		Permission:     PermReveal,
		ActorType:      "agent",
		ActorID:        uuidFromString(t, "55555555-5555-5555-5555-555555555555"),
	})
	if dec.Allowed {
		t.Fatalf("expected deny when neither scope matches, got %+v", dec)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("expected two checks before final deny, got %d: %v", len(stub.calls), stub.calls)
	}
}

func TestPersonaPolicyChecker_PermissionMappedToCredentialAction(t *testing.T) {
	credID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	stub := &stubPersona{}
	checker := NewPersonaPolicyChecker(stub)

	for _, p := range []Permission{PermAttach, PermReadRedacted, PermReveal, PermRotate, PermRevoke} {
		stub.calls = nil
		_ = checker.Check(context.Background(), PolicyRequest{
			CredentialID:   credID,
			CredentialType: TypeAPIKey,
			Permission:     p,
			ActorType:      "agent",
			ActorID:        uuidFromString(t, "55555555-5555-5555-5555-555555555555"),
		})
		if len(stub.calls) == 0 {
			t.Fatalf("expected at least one persona call for permission %s", p)
		}
		want := "credential." + string(p)
		got := stub.calls[0]
		if got[:len(want)] != want {
			t.Fatalf("permission %s: expected action prefix %q, got %q", p, want, got)
		}
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
