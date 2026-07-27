package service

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/providerfailover"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

// chainRootForTask: chat chains collapse to chat_input_task_id; a plain issue
// task is its own chain root. This underpins at-most-one-per-chain.
func TestChainRootForTask(t *testing.T) {
	t.Parallel()
	self := uuid(1)
	chatInput := uuid(2)

	issueTask := db.AgentTaskQueue{ID: self}
	if got := chainRootForTask(issueTask); got != self {
		t.Fatalf("issue task chain root = %v, want self %v", got, self)
	}

	chatTask := db.AgentTaskQueue{ID: self, ChatInputTaskID: chatInput}
	if got := chainRootForTask(chatTask); got != chatInput {
		t.Fatalf("chat task chain root = %v, want chat input %v", got, chatInput)
	}
}

// AC11 / #7: system agents and explicitly protected user agents are
// authority-sensitive. Names and system_key remain deliberately irrelevant.
func TestAgentAuthoritySensitive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		agent db.Agent
		want  bool
	}{
		{"plain user agent", db.Agent{Kind: "user"}, false},
		{"system kind", db.Agent{Kind: "system"}, true},
		{"agent-builder system key is system kind", db.Agent{Kind: "system", SystemKey: pgtype.Text{String: "agent_builder:abc", Valid: true}}, true},
		{"user agent with reviewer-ish name key is NOT sensitive", db.Agent{Kind: "user", SystemKey: pgtype.Text{String: "protected_reviewer", Valid: true}}, false},
		{"legacy Protected Reviewer identity", db.Agent{Kind: "user", Name: "Protected Reviewer"}, true},
		{"legacy Protected Reviewer identity is case-insensitive", db.Agent{Kind: "user", Name: " protected reviewer "}, true},
		{"other reviewer name is not sensitive", db.Agent{Kind: "user", Name: "Release Reviewer"}, false},
		{"user agent with explicit protection marker", db.Agent{Kind: "user", RuntimeConfig: []byte(`{"provider_failover_protected":true}`)}, true},
		{"user agent with explicit false marker", db.Agent{Kind: "user", RuntimeConfig: []byte(`{"provider_failover_protected":false}`)}, false},
		{"user agent with malformed config", db.Agent{Kind: "user", RuntimeConfig: []byte(`{`)}, false},
		{"user agent, benign system key", db.Agent{Kind: "user", SystemKey: pgtype.Text{String: "triage_bot", Valid: true}}, false},
		{"null system key", db.Agent{Kind: "user"}, false},
	}
	for _, tc := range cases {
		if got := agentAuthoritySensitive(tc.agent, db.AgentTaskQueue{}); got != tc.want {
			t.Errorf("%s: agentAuthoritySensitive = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFailoverTargetRequiresSameOwnerAndExplicitOptIn(t *testing.T) {
	t.Parallel()
	owner := uuid(1)
	otherOwner := uuid(2)
	source := db.Agent{OwnerID: owner}

	cases := []struct {
		name   string
		target db.Agent
		want   bool
	}{
		{
			name: "same owner and opted in",
			target: db.Agent{
				OwnerID:       owner,
				Kind:          "user",
				RuntimeConfig: []byte(`{"provider_failover_target":true}`),
			},
			want: true,
		},
		{
			name: "cross owner rejected",
			target: db.Agent{
				OwnerID:       otherOwner,
				Kind:          "user",
				RuntimeConfig: []byte(`{"provider_failover_target":true}`),
			},
		},
		{
			name: "missing opt in rejected",
			target: db.Agent{
				OwnerID:       owner,
				Kind:          "user",
				RuntimeConfig: []byte(`{}`),
			},
		},
		{
			name: "missing owner rejected",
			target: db.Agent{
				Kind:          "user",
				RuntimeConfig: []byte(`{"provider_failover_target":true}`),
			},
		},
		{
			name: "malformed config rejected",
			target: db.Agent{
				OwnerID:       owner,
				Kind:          "user",
				RuntimeConfig: []byte(`{`),
			},
		},
		{
			name: "authority-sensitive target rejected",
			target: db.Agent{
				OwnerID:       owner,
				Kind:          "system",
				RuntimeConfig: []byte(`{"provider_failover_target":true}`),
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := failoverTargetStructurallyEligible(source, tc.target); got != tc.want {
				t.Fatalf("failoverTargetStructurallyEligible() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLivenessFailoverModeIsAlwaysShadowWhenEnabled(t *testing.T) {
	t.Parallel()
	cases := []struct {
		configured providerfailover.Mode
		want       providerfailover.Mode
	}{
		{configured: providerfailover.ModeOff, want: providerfailover.ModeOff},
		{configured: providerfailover.ModeShadow, want: providerfailover.ModeShadow},
		{configured: providerfailover.ModeActive, want: providerfailover.ModeShadow},
	}
	for _, tc := range cases {
		if got := livenessFailoverMode(tc.configured); got != tc.want {
			t.Errorf("livenessFailoverMode(%s) = %s, want %s", tc.configured, got, tc.want)
		}
	}
}

// td-836aa9 #2/#8: completeness is proven only when the daemon sent a
// completeness-marked evidence object. Nil evidence (old daemon / pre-execution
// fail path) and Complete=false both keep the surface unproven, and the active
// policy then fails closed with ReasonSideEffectsUnproven. This exercises the
// real service seam, not only the pure policy.
func TestFailoverSideEffectsComplete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		evidence *providerfailover.SideEffectEvidence
		want     bool
	}{
		{"nil evidence (legacy daemon) is unproven", nil, false},
		{"evidence without complete marker is unproven",
			&providerfailover.SideEffectEvidence{ObservedToolCalls: 0, Complete: false}, false},
		{"complete evidence is proven",
			&providerfailover.SideEffectEvidence{ObservedToolCalls: 0, Complete: true}, true},
		{"complete evidence with tools is still proven-complete (blocked elsewhere by the side-effect gate)",
			&providerfailover.SideEffectEvidence{ObservedToolCalls: 3, Complete: true}, true},
	}
	for _, tc := range cases {
		if got := failoverSideEffectsComplete(tc.evidence); got != tc.want {
			t.Errorf("%s: failoverSideEffectsComplete = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// td-836aa9 #7: an older daemon (nil evidence) leaves active fail-closed with
// ReasonSideEffectsUnproven, since completeness is never proven.
func TestFailoverActive_LegacyEvidenceFailsClosed(t *testing.T) {
	t.Parallel()
	in := providerfailover.Input{
		FailureReason:       taskfailure.ReasonAgentProviderCapacityOrRateLimit,
		SourceProvider:      "codex",
		Mode:                providerfailover.ModeActive,
		ClaudeAvailable:     true,
		SideEffectsComplete: failoverSideEffectsComplete(nil),
	}
	d := providerfailover.Decide(in)
	if d.Outcome != providerfailover.OutcomeDeclined || d.Reason != providerfailover.ReasonSideEffectsUnproven {
		t.Fatalf("active service path must fail closed on unproven side effects, got %+v", d)
	}
}

// td-836aa9 #1: a NEW daemon reporting zero tools and no partial output proves a
// clean surface, so active mode proceeds (given an eligible trigger + source).
func TestFailoverActive_NewDaemonZeroToolsNoPartialProceeds(t *testing.T) {
	t.Parallel()
	s := &TaskService{}
	ev := &providerfailover.SideEffectEvidence{ObservedToolCalls: 0, PartialUserOutput: false, Complete: true}
	se := s.gatherFailoverSideEffects(t.Context(), db.AgentTaskQueue{ID: uuid(1)}, ev)
	in := providerfailover.Input{
		FailureReason:       taskfailure.ReasonAgentProviderQuotaLimit,
		SourceProvider:      "codex",
		Mode:                providerfailover.ModeActive,
		ClaudeAvailable:     true,
		SideEffects:         se,
		SideEffectsComplete: failoverSideEffectsComplete(ev),
	}
	if d := providerfailover.Decide(in); d.Outcome != providerfailover.OutcomeProceed {
		t.Fatalf("new-daemon zero-tools no-partial must proceed in active mode, got %+v", d)
	}
}

// td-836aa9: any observed tool call blocks the handoff (side_effects_present) —
// even with complete evidence and in both shadow and active modes.
func TestFailoverEvidence_AnyToolBlocks(t *testing.T) {
	t.Parallel()
	s := &TaskService{}
	ev := &providerfailover.SideEffectEvidence{ObservedToolCalls: 1, Complete: true}
	se := s.gatherFailoverSideEffects(t.Context(), db.AgentTaskQueue{ID: uuid(1)}, ev)
	if !se.HasObservableSideEffects() {
		t.Fatal("a non-zero observed tool count must be an observable side effect")
	}
	for _, mode := range []providerfailover.Mode{providerfailover.ModeShadow, providerfailover.ModeActive} {
		in := providerfailover.Input{
			FailureReason:       taskfailure.ReasonAgentProviderCapacityOrRateLimit,
			SourceProvider:      "codex",
			Mode:                mode,
			ClaudeAvailable:     true,
			SideEffects:         se,
			SideEffectsComplete: failoverSideEffectsComplete(ev),
		}
		d := providerfailover.Decide(in)
		if d.WouldFailOver || d.Reason != providerfailover.ReasonSideEffects {
			t.Fatalf("%s: any tool call must block with side_effects_present, got %+v", mode, d)
		}
	}
}

// td-836aa9: any partial user output blocks the handoff, independent of the tool
// count.
func TestFailoverEvidence_AnyPartialOutputBlocks(t *testing.T) {
	t.Parallel()
	s := &TaskService{}
	ev := &providerfailover.SideEffectEvidence{ObservedToolCalls: 0, PartialUserOutput: true, Complete: true}
	se := s.gatherFailoverSideEffects(t.Context(), db.AgentTaskQueue{ID: uuid(1)}, ev)
	if !se.HasObservableSideEffects() {
		t.Fatal("partial user output must be an observable side effect")
	}
	in := providerfailover.Input{
		FailureReason:       taskfailure.ReasonAgentProviderCapacityOrRateLimit,
		SourceProvider:      "codex",
		Mode:                providerfailover.ModeActive,
		ClaudeAvailable:     true,
		SideEffects:         se,
		SideEffectsComplete: failoverSideEffectsComplete(ev),
	}
	if d := providerfailover.Decide(in); d.WouldFailOver || d.Reason != providerfailover.ReasonSideEffects {
		t.Fatalf("partial output must block with side_effects_present, got %+v", d)
	}
}

// The chain-owner unique-violation is what enforces at-most-one under a race;
// the classifier must recognize exactly that constraint and nothing else.
func TestIsFailoverChainOwnerConflict(t *testing.T) {
	t.Parallel()
	chainConflict := &pgconn.PgError{Code: "23505", ConstraintName: "provider_failover_chain_owner_uidx"}
	if !isFailoverChainOwnerConflict(chainConflict) {
		t.Error("chain-owner 23505 must be recognized")
	}
	otherUnique := &pgconn.PgError{Code: "23505", ConstraintName: "provider_failover_original_task_uidx"}
	if isFailoverChainOwnerConflict(otherUnique) {
		t.Error("a different unique index must not be treated as a chain-owner conflict")
	}
	notPg := fmt.Errorf("some wrapped error")
	if isFailoverChainOwnerConflict(notPg) {
		t.Error("non-pg error must not be a chain-owner conflict")
	}
}

// AC7: a chain owned by a handoff supersedes a late primary completion; a
// non-owning (declined/shadow) row does not.
func TestSupersedeStateSemantics(t *testing.T) {
	t.Parallel()
	if !providerfailover.HandoffState("HANDOFF_PENDING").IsOwning() {
		t.Error("PENDING must own the chain (supersede a late completion)")
	}
	if !providerfailover.HandoffState("HANDOFF_DISPATCHED").IsOwning() {
		t.Error("DISPATCHED must own the chain")
	}
	if providerfailover.HandoffState("HANDOFF_SHADOW").IsOwning() {
		t.Error("SHADOW must never supersede a completion")
	}
	if providerfailover.HandoffState("HANDOFF_DECLINED").IsOwning() {
		t.Error("DECLINED must never supersede a completion")
	}
}

// gatherFailoverSideEffects maps persisted delivery receipts into the snapshot.
// For a chat task (no issue) the mapping is pure — no HasAgentCommentedSince
// probe — so it exercises the delivered-comment signal without a DB.
func TestGatherSideEffects_ChatTask(t *testing.T) {
	t.Parallel()
	s := &TaskService{}
	task := db.AgentTaskQueue{
		ID:                  uuid(9),
		ChatSessionID:       uuid(8),
		DeliveredCommentIds: []pgtype.UUID{uuid(3), uuid(4)},
	}
	se := s.gatherFailoverSideEffects(t.Context(), task, nil)
	if se.DeliveredCommentIDs != 2 {
		t.Fatalf("delivered comment count = %d, want 2", se.DeliveredCommentIDs)
	}
	if !se.HasObservableSideEffects() {
		t.Fatal("delivered comments must count as an observable side effect")
	}
}
