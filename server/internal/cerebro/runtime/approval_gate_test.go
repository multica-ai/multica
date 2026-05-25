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
}

func (f *gateFakeApprovals) Intake(_ context.Context, p approvals.IntakeParams) (cerebrodb.CerebroApprovalRequest, error) {
	f.intakes++
	return cerebrodb.CerebroApprovalRequest{ID: gateTestUUID(2), WorkspaceID: p.WorkspaceID, Status: approvals.StatusPending}, nil
}

func (f *gateFakeApprovals) Get(_ context.Context, _, _ pgtype.UUID) (cerebrodb.CerebroApprovalRequest, error) {
	return cerebrodb.CerebroApprovalRequest{ID: gateTestUUID(2), Status: f.status}, nil
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
		"firtal_bq_query":     "network.external",
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

func TestGuardToolCall_NilGate_AllowsWithoutLookup(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()} // gate == nil
	allowed, reason := e.guardToolCall(context.Background(), gateTestUUID(1), gateTestUUID(9), "web_fetch", nil, GatewayRequestMeta{})
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

	allowed, _ := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, GatewayRequestMeta{})
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

	allowed, _ := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "get_issue", nil, GatewayRequestMeta{})
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

	allowed, reason := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, GatewayRequestMeta{})
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

	allowed, reason := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "credential_list", nil, GatewayRequestMeta{})
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

	allowed, _ := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, GatewayRequestMeta{})
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

	allowed, reason := e.guardToolCall(context.Background(), agent, gateTestUUID(9), "web_fetch", nil, GatewayRequestMeta{})
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
