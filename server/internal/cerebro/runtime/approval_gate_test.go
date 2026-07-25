package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/util"
)

type gateFakeTaskMandates struct {
	authorizeErr error
	calls        []string
}

func (f *gateFakeTaskMandates) Issue(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, []string, time.Time) error {
	return nil
}

func (f *gateFakeTaskMandates) Authorize(_ context.Context, _, _, _ pgtype.UUID, tool string) error {
	f.calls = append(f.calls, tool)
	return f.authorizeErr
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

func newGatedExecutor(ap *gateFakeApprovals) *FirtalGatewayExecutor {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	gate := &permgate.Gate{Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: time.Second}
	e.EnableApprovalGate(gate)
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
		"get_issue":     "",
		"add_comment":   "",
		"create_issue":  "",
		"list_runtimes": "",
		"unknown_tool":  "",
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

func TestGuardToolCallTaskMandateDeniesEveryCallPathBeforePolicy(t *testing.T) {
	taskID := gateTestUUID(7)
	meta := GatewayRequestMeta{TaskID: util.UUIDToString(taskID)}

	for _, tc := range []struct {
		name string
		tool string
		err  error
	}{
		{name: "expired ordinary tool", tool: "web_fetch", err: errors.New("task mandate expired")},
		{name: "tool outside mandate", tool: "web_fetch", err: errors.New("tool is outside task mandate")},
		{name: "expired create_issue early path", tool: "create_issue", err: errors.New("task mandate expired")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mandates := &gateFakeTaskMandates{authorizeErr: tc.err}
			e := (&FirtalGatewayExecutor{logger: slog.Default()}).SetTaskMandates(mandates)
			allowed, reason := e.guardToolCall(context.Background(), gateTestUUID(1), gateTestUUID(9), tc.tool, nil, nil, meta)
			if allowed || !strings.Contains(reason, tc.err.Error()) {
				t.Fatalf("guardToolCall() = (%v, %q), want denial containing %q", allowed, reason, tc.err)
			}
			if len(mandates.calls) != 1 || mandates.calls[0] != tc.tool {
				t.Fatalf("mandate calls = %v, want [%s]", mandates.calls, tc.tool)
			}
		})
	}
}

func TestGuardToolCallMissingTaskMandateFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    *FirtalGatewayExecutor
		meta GatewayRequestMeta
	}{
		{name: "task id has no mandate row", e: (&FirtalGatewayExecutor{logger: slog.Default()}).SetTaskMandates(&gateFakeTaskMandates{authorizeErr: errors.New("task mandate missing")}), meta: GatewayRequestMeta{TaskID: util.UUIDToString(gateTestUUID(7))}},
		{name: "wired store has no task id", e: (&FirtalGatewayExecutor{logger: slog.Default()}).SetTaskMandates(&gateFakeTaskMandates{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed, reason := tc.e.guardToolCall(context.Background(), gateTestUUID(1), gateTestUUID(9), "web_fetch", nil, nil, tc.meta)
			if allowed || !strings.Contains(reason, "task mandate missing") {
				t.Fatalf("guardToolCall() = (%v, %q), want missing-mandate denial", allowed, reason)
			}
		})
	}
}

// --- guardToolCall ----------------------------------------------------------

// FIR-2388: approvalInboxActive decides whether an Ask reaches the approval inbox
// (and can therefore block for a human) rather than being downgraded to Allow.
// The API-connection endpoint Ask fail-closed hinges on it: inbox inactive → the
// secrets-fronting Ask endpoint is blocked instead of dispatched unapproved.
func TestApprovalInboxActive(t *testing.T) {
	ws := gateTestUUID(9)

	// gate nil → inbox cannot run.
	e0 := &FirtalGatewayExecutor{logger: slog.Default()}
	if e0.approvalInboxActive(context.Background(), ws) {
		t.Fatalf("nil gate must report inbox inactive")
	}

	// Gate on, cerebro nil → workspace approval flag defaults ON → inbox active.
	e1 := newGatedExecutor(&gateFakeApprovals{})
	if !e1.approvalInboxActive(context.Background(), ws) {
		t.Fatalf("gate on + flag default-on must report inbox active")
	}
}

func TestGuardToolCall_MissingPolicyDecisionServiceFailsClosed(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	allowed, reason := e.guardToolCall(context.Background(), gateTestUUID(1), gateTestUUID(9), "web_fetch", nil, policyTestRegistry("web_fetch"), GatewayRequestMeta{})
	if allowed || reason == "" {
		t.Fatalf("missing policy decision service must deny; got allowed=%v reason=%q", allowed, reason)
	}
}

// TestGuardToolCall_EmitsDecisionTrace confirms the FIR-2243 B1 runtime audit
// line fires on the fail-closed path and
// carries the tool, decision, allowed flag, and run identity.
func TestGuardToolCall_EmitsDecisionTrace(t *testing.T) {
	rec := &decisionCaptureHandler{}
	e := &FirtalGatewayExecutor{logger: slog.New(rec)}
	allowed, _ := e.guardToolCall(context.Background(), gateTestUUID(1), gateTestUUID(9), "web_fetch", nil, nil,
		GatewayRequestMeta{AgentID: "agent-1", AgentName: "Mia", TaskID: "task-9", IssueID: "issue-7", Surface: "issue"})
	if allowed {
		t.Fatal("missing policy decision service must deny")
	}
	var line map[string]string
	for _, r := range rec.records {
		if r["event"] == "tool_call_decision" {
			line = r
			break
		}
	}
	if line == nil {
		t.Fatalf("expected a tool_call_decision trace line, got %v", rec.records)
	}
	want := map[string]string{
		"tool": "web_fetch", "decision": "policy_decision_service", "allowed": "false",
		"agent_id": "agent-1", "task_id": "task-9", "issue_id": "issue-7",
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("trace attr %q = %q, want %q", k, line[k], v)
		}
	}
}

// decisionCaptureHandler is a minimal slog.Handler that records each record's
// attributes (flattened to string) for asserting the B1 decision trace line.
type decisionCaptureHandler struct {
	records []map[string]string
}

func (h *decisionCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *decisionCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	h.records = append(h.records, m)
	return nil
}
func (h *decisionCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *decisionCaptureHandler) WithGroup(string) slog.Handler      { return h }

func TestBuildApprovalGateIsAlwaysAvailable(t *testing.T) {
	if gate := BuildApprovalGate(nil, nil, nil); gate == nil {
		t.Fatal("BuildApprovalGate must not depend on a server rollout switch")
	}
}
