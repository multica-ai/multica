package toolpolicy

import (
	"context"
	"errors"
	"testing"
)

// fakeResolver decides by exact resource match: a resource listed in deny/ask
// gets that setting, everything else is Allow (mirroring Base=Allow). An error
// resource forces a resolve error so the fail-closed path is exercised.
type fakeResolver struct {
	deny    map[string]bool
	ask     map[string]bool
	errOn   map[string]bool
	lastCtx RequestContext // records the context the audit threaded in
}

func (f *fakeResolver) Resolve(_ context.Context, in Query) (Effective, error) {
	f.lastCtx = in.RequestContext
	if f.errOn[in.ResourcePattern] {
		return Effective{}, errors.New("boom")
	}
	switch {
	case f.deny[in.ResourcePattern]:
		return Effective{Setting: SettingDeny, Reason: "denied by test row"}, nil
	case f.ask[in.ResourcePattern]:
		return Effective{Setting: SettingAsk, Reason: "ask by test row"}, nil
	default:
		return Effective{Setting: SettingAllow, Reason: "Allowed by base"}, nil
	}
}

func grants() []LegacyGrant {
	return []LegacyGrant{
		{Path: LegacyPathRepoAllowlist, ToolKey: "repo.checkout", Resource: "github.com/firtal/a"},
		{Path: LegacyPathRepoAllowlist, ToolKey: "repo.checkout", Resource: "github.com/firtal/b"},
		{Path: LegacyPathCredentialGrant, ToolKey: "credential.reveal", Resource: "cred-1"},
	}
}

// The safe-enablement invariant: with no Deny/Ask rows (Base=Allow), every
// legacy grant still resolves to Allow, so the lock-out delta is empty and the
// gate is 1:1 — the go-signal.
func TestAuditEnablement_NoRows_IsEmptyDelta(t *testing.T) {
	r := &fakeResolver{}
	risks, err := AuditEnablement(context.Background(), r, grants())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(risks) != 0 {
		t.Fatalf("expected empty delta with no Deny/Ask rows, got %d: %+v", len(risks), risks)
	}
	if !ReadyForEnablement(risks) {
		t.Fatal("expected ReadyForEnablement true on empty delta")
	}
}

// A Deny (or Ask) row on a currently-allowed resource is a lock-out risk that
// must become an explicit Allow row before enablement.
func TestAuditEnablement_DenyAndAsk_AreLockoutRisks(t *testing.T) {
	r := &fakeResolver{
		deny: map[string]bool{"github.com/firtal/b": true},
		ask:  map[string]bool{"cred-1": true},
	}
	risks, err := AuditEnablement(context.Background(), r, grants())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(risks) != 2 {
		t.Fatalf("expected 2 lock-out risks (one Deny, one Ask), got %d: %+v", len(risks), risks)
	}
	if ReadyForEnablement(risks) {
		t.Fatal("expected ReadyForEnablement false when risks exist")
	}
	for _, risk := range risks {
		if risk.Verdict.Setting == SettingAllow {
			t.Fatalf("a lock-out risk must not be Allow: %+v", risk)
		}
	}
}

// A resolve error is fail-closed: the grant is recorded as a risk, never
// assumed safe.
func TestAuditEnablement_ResolveError_FailsClosed(t *testing.T) {
	r := &fakeResolver{errOn: map[string]bool{"cred-1": true}}
	risks, err := AuditEnablement(context.Background(), r, grants())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(risks) != 1 {
		t.Fatalf("expected 1 fail-closed risk, got %d: %+v", len(risks), risks)
	}
	if risks[0].Grant.Resource != "cred-1" || risks[0].Verdict.Setting != SettingDeny {
		t.Fatalf("expected fail-closed Deny on cred-1, got %+v", risks[0])
	}
}

// The audit threads the action verb in exactly as the live gates do, so an
// action-scoped Condition is evaluated rather than silently skipped.
func TestAuditEnablement_DerivesActionVerb(t *testing.T) {
	r := &fakeResolver{}
	_, err := AuditEnablement(context.Background(), r, []LegacyGrant{
		{Path: LegacyPathCredentialGrant, ToolKey: "credential.reveal", Resource: "cred-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.lastCtx.Action; got != ActionOf("credential.reveal") {
		t.Fatalf("expected action %q threaded in, got %q", ActionOf("credential.reveal"), got)
	}
}

func TestAuditEnablement_NilResolver_NoRisks(t *testing.T) {
	risks, err := AuditEnablement(context.Background(), nil, grants())
	if err != nil || risks != nil {
		t.Fatalf("nil resolver should be a no-op, got risks=%+v err=%v", risks, err)
	}
}
