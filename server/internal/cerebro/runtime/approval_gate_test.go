package runtime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// --- fakes (assignable to permgate.Gate's exported interface fields) --------

type gateFakeResolver struct {
	decision permissions.Decision
	calls    int
}

func (f *gateFakeResolver) Can(_ context.Context, _ permissions.Request) (permissions.Decision, error) {
	f.calls++
	return f.decision, nil
}

type gateFakeApprovals struct {
	intakes int
	status  string

	// reusable, when set, is returned by FindReusable to model a still-valid
	// prior grant — an approved, unexpired row that GuardDecisionReusing
	// short-circuits to allow without a new ask (TECH-3498 period grant). Its
	// expires_at gates whether it is honoured, mirroring the real SQL filter.
	reusable *cerebrodb.CerebroApprovalRequest
}

func (f *gateFakeApprovals) Intake(_ context.Context, p approvals.IntakeParams) (cerebrodb.CerebroApprovalRequest, error) {
	f.intakes++
	return cerebrodb.CerebroApprovalRequest{ID: gateTestUUID(2), WorkspaceID: p.WorkspaceID, Status: approvals.StatusPending}, nil
}

func (f *gateFakeApprovals) Get(_ context.Context, _, _ pgtype.UUID) (cerebrodb.CerebroApprovalRequest, error) {
	return cerebrodb.CerebroApprovalRequest{ID: gateTestUUID(2), Status: f.status}, nil
}

// FindReusable models the real FindReusable SQL filter: a stored approved row is
// reusable only while its expires_at is NULL or in the future. The blocking tool
// gate (Guard/GuardDecision) never calls this, but GuardDecisionReusing does, so
// the fake honours a configured period grant and otherwise reports "none".
func (f *gateFakeApprovals) FindReusable(_ context.Context, _ approvals.ReusableQuery) (cerebrodb.CerebroApprovalRequest, bool, error) {
	if f.reusable == nil {
		return cerebrodb.CerebroApprovalRequest{}, false, nil
	}
	if f.reusable.ExpiresAt.Valid && !f.reusable.ExpiresAt.Time.After(time.Now()) {
		return cerebrodb.CerebroApprovalRequest{}, false, nil
	}
	return *f.reusable, true, nil
}

func gateTestUUID(seed byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = seed
	}
	u.Valid = true
	return u
}

func newGatedExecutor(res *gateFakeResolver, ap *gateFakeApprovals, allow ...pgtype.UUID) *FirtalGatewayExecutor {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	gate := &permgate.Gate{Resolver: res, Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: time.Second}
	e.EnableApprovalGate(gate, allow)
	return e
}

// --- toolCapabilityKey ------------------------------------------------------

func TestToolCapabilityKey(t *testing.T) {
	cases := map[string]string{
		"web_fetch":           "network.external",
		"firtal_registry":     "network.external",
		"credential_list":     "credentials.read",
		"gogcli_sheets_write": "prod.write",
		// Internal Multica CRUD stays ungated on purpose.
		"get_issue":    "",
		"add_comment":  "",
		"create_issue": "",
		"list_runtimes": "",
		"unknown_tool": "",
	}
	for tool, want := range cases {
		if got := toolCapabilityKey(tool); got != want {
			t.Errorf("toolCapabilityKey(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestDecodeToolArgs(t *testing.T) {
	if got := decodeToolArgs(`{"url":"https://x"}`); got["url"] != "https://x" {
		t.Errorf("expected url parsed, got %v", got)
	}
	if got := decodeToolArgs(""); len(got) != 0 {
		t.Errorf("empty args should decode to empty map, got %v", got)
	}
	if got := decodeToolArgs("not json"); got == nil || len(got) != 0 {
		t.Errorf("invalid json should yield empty (non-nil) map, got %v", got)
	}
}

// --- guardToolCall ----------------------------------------------------------

// FIR-2388: approvalInboxActive decides whether an Ask reaches the approval inbox
// (and can therefore block for a human) rather than being downgraded to Allow.
// The API-connection endpoint Ask fail-closed hinges on it: inbox inactive → the
// secrets-fronting Ask endpoint is blocked instead of dispatched unapproved.
func TestApprovalInboxActive(t *testing.T) {
	agent := gateTestUUID(1)
	ws := gateTestUUID(9)

	// gate nil → inbox cannot run.
	e0 := &FirtalGatewayExecutor{logger: slog.Default()}
	if e0.approvalInboxActive(context.Background(), agent, ws) {
		t.Fatalf("nil gate must report inbox inactive")
	}

	// gate on, no allowlist (all agents in scope), cerebro nil → workspace approval
	// flag defaults ON → inbox active.
	e1 := newGatedExecutor(&gateFakeResolver{}, &gateFakeApprovals{})
	if !e1.approvalInboxActive(context.Background(), agent, ws) {
		t.Fatalf("gate on + agent in scope + flag default-on must report inbox active")
	}

	// gate scoped to a DIFFERENT agent → inbox inactive for ours.
	e2 := newGatedExecutor(&gateFakeResolver{}, &gateFakeApprovals{}, gateTestUUID(7))
	if e2.approvalInboxActive(context.Background(), agent, ws) {
		t.Fatalf("agent outside the gate's rollout scope must report inbox inactive")
	}
}

func TestGuardToolCall_NilGate_AllowsWithoutLookup(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()} // gate == nil
	allowed, reason := e.guardToolCall(context.Background(), gateTestUUID(1), gateTestUUID(9), "web_fetch", nil, nil, GatewayRequestMeta{})
	if !allowed || reason != "" {
		t.Fatalf("nil gate must allow; got allowed=%v reason=%q", allowed, reason)
	}
}

func TestGuardToolCall_AgentOutsideAllowlist_Allows(t *testing.T) {
	res := &gateFakeResolver{decision: permissions.Decision{Kind: permissions.DecisionDeny}}
	ap := &gateFakeApprovals{}
	agent := gateTestUUID(1)
	other := gateTestUUID(7)
	e := newGatedExecutor(res, ap, other) // gate scoped to a different agent

	allowed, _ := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, nil, GatewayRequestMeta{})
	if !allowed {
		t.Fatal("agent outside the allowlist must run ungated")
	}
	if res.calls != 0 {
		t.Fatalf("resolver must not be consulted for an unscoped agent, got %d calls", res.calls)
	}
}

func TestGuardToolCall_UngatedTool_Allows(t *testing.T) {
	res := &gateFakeResolver{decision: permissions.Decision{Kind: permissions.DecisionDeny}}
	ap := &gateFakeApprovals{}
	agent := gateTestUUID(1)
	e := newGatedExecutor(res, ap, agent)

	allowed, _ := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "get_issue", nil, nil, GatewayRequestMeta{})
	if !allowed {
		t.Fatal("ungated tool must be allowed")
	}
	if res.calls != 0 {
		t.Fatalf("ungated tool must not consult the resolver, got %d calls", res.calls)
	}
}

func TestGuardToolCall_Allowed(t *testing.T) {
	res := &gateFakeResolver{decision: permissions.Decision{Kind: permissions.DecisionAllow, Reason: "grant matched"}}
	ap := &gateFakeApprovals{}
	agent := gateTestUUID(1)
	e := newGatedExecutor(res, ap, agent)

	allowed, reason := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, nil, GatewayRequestMeta{})
	if !allowed || reason != "" {
		t.Fatalf("allow decision must pass; got allowed=%v reason=%q", allowed, reason)
	}
	if ap.intakes != 0 {
		t.Fatalf("allow must not create an approval ask, got %d", ap.intakes)
	}
}

func TestGuardToolCall_Denied_Blocks(t *testing.T) {
	res := &gateFakeResolver{decision: permissions.Decision{Kind: permissions.DecisionDeny, Reason: "no matching grant"}}
	ap := &gateFakeApprovals{}
	agent := gateTestUUID(1)
	e := newGatedExecutor(res, ap, agent)

	allowed, reason := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "credential_list", nil, nil, GatewayRequestMeta{})
	if allowed {
		t.Fatal("deny decision must block the tool")
	}
	if reason == "" {
		t.Fatal("blocked call must carry a reason")
	}
	if ap.intakes != 0 {
		t.Fatalf("deny must not create an approval ask, got %d", ap.intakes)
	}
}

func TestGuardToolCall_NeedsApproval_ApprovedContinues(t *testing.T) {
	res := &gateFakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &gateFakeApprovals{status: approvals.StatusApproved} // human already approved
	agent := gateTestUUID(1)
	e := newGatedExecutor(res, ap, agent)

	allowed, _ := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, nil, GatewayRequestMeta{})
	if !allowed {
		t.Fatal("approved ask must let the tool continue")
	}
	if ap.intakes != 1 {
		t.Fatalf("needs_approval must create exactly one ask, got %d", ap.intakes)
	}
}

func TestGuardToolCall_NeedsApproval_RejectedBlocks(t *testing.T) {
	res := &gateFakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &gateFakeApprovals{status: approvals.StatusRejected}
	agent := gateTestUUID(1)
	e := newGatedExecutor(res, ap, agent)

	allowed, reason := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, nil, GatewayRequestMeta{})
	if allowed {
		t.Fatal("rejected ask must block the tool")
	}
	if reason == "" {
		t.Fatal("rejected call must carry a reason")
	}
	if ap.intakes != 1 {
		t.Fatalf("needs_approval must create exactly one ask, got %d", ap.intakes)
	}
}

// --- toolPolicyDecision (FIR-2230 chain verdict → gate decision) ------------

func TestToolPolicyDecision(t *testing.T) {
	cases := []struct {
		setting toolpolicy.Setting
		want    permissions.DecisionKind
	}{
		{toolpolicy.SettingAllow, permissions.DecisionAllow},
		{toolpolicy.SettingAsk, permissions.DecisionNeedsApproval},
		{toolpolicy.SettingDeny, permissions.DecisionDeny},
		// Resolve never yields Inherit; an unexpected value must fail closed.
		{toolpolicy.SettingInherit, permissions.DecisionDeny},
	}
	for _, c := range cases {
		got := toolPolicyDecision(toolpolicy.Effective{Setting: c.setting, Reason: "r"})
		if got.Kind != c.want {
			t.Errorf("toolPolicyDecision(%q).Kind = %q, want %q", c.setting, got.Kind, c.want)
		}
	}
	// The Effective reason is carried through for the audit trail.
	if got := toolPolicyDecision(toolpolicy.Effective{Setting: toolpolicy.SettingDeny, Reason: "Capped by user"}); got.Reason != "Capped by user" {
		t.Errorf("reason not carried through, got %q", got.Reason)
	}
}

// --- env knobs (the rollout wiring, FIR-1609 item 6) ------------------------

// TestApprovalGateEnvEnabled pins the CEREBRO_APPROVAL_GATE_ENABLED parser: only
// the explicit truthy spellings turn the gate on, everything else (incl. unset)
// keeps production unchanged.
func TestApprovalGateEnvEnabled(t *testing.T) {
	on := []string{"1", "true", "TRUE", "yes", "on", " On "}
	off := []string{"", "0", "false", "no", "off", "toolpolicy", "garbage"}
	for _, v := range on {
		t.Setenv(envApprovalGateEnabled, v)
		if !approvalGateEnvEnabled() {
			t.Errorf("CEREBRO_APPROVAL_GATE_ENABLED=%q should enable the gate", v)
		}
	}
	for _, v := range off {
		t.Setenv(envApprovalGateEnabled, v)
		if approvalGateEnvEnabled() {
			t.Errorf("CEREBRO_APPROVAL_GATE_ENABLED=%q must NOT enable the gate", v)
		}
	}
}

// TestApprovalGateModeToolPolicy pins the CEREBRO_APPROVAL_GATE_MODE parser: the
// three accepted spellings select the FIR-2230 per-tool chain; anything else
// (incl. unset) stays on the legacy capability resolver, so prod is unchanged
// until an operator opts in.
func TestApprovalGateModeToolPolicy(t *testing.T) {
	policy := []string{"toolpolicy", "tool-policy", "tool_policy", "ToolPolicy", " toolpolicy "}
	capability := []string{"", "capability", "caps", "true", "1", "anything-else"}
	for _, v := range policy {
		t.Setenv(envApprovalGateMode, v)
		if !approvalGateModeToolPolicy() {
			t.Errorf("CEREBRO_APPROVAL_GATE_MODE=%q should select the tool-policy chain", v)
		}
	}
	for _, v := range capability {
		t.Setenv(envApprovalGateMode, v)
		if approvalGateModeToolPolicy() {
			t.Errorf("CEREBRO_APPROVAL_GATE_MODE=%q must stay on the capability resolver", v)
		}
	}
}

// TestBuildApprovalGate_DisabledReturnsNil proves the master switch: with
// CEREBRO_APPROVAL_GATE_ENABLED off, BuildApprovalGate returns nil before it
// touches any dependency, so every caller keeps its prior behaviour (the nil
// deps here are never dereferenced precisely because the env gate is off).
func TestBuildApprovalGate_DisabledReturnsNil(t *testing.T) {
	t.Setenv(envApprovalGateEnabled, "")
	if gate := BuildApprovalGate(nil, nil, nil); gate != nil {
		t.Fatal("BuildApprovalGate must return nil when the env flag is off")
	}
}
