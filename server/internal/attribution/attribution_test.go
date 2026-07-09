package attribution

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// uid builds a valid, deterministic pgtype.UUID from a single seed byte so the
// tests can assert on identity without importing the util package.
func uid(seed byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = seed
	}
	u.Valid = true
	return u
}

var (
	human    = uid(0x11)
	other    = uid(0x22)
	comment  = uid(0xC0)
	srcTask  = uid(0x5A)
	issue    = uid(0x15)
	originTk = uid(0x0A)
)

func TestClassifyComment_MemberAuthoredIsDirectHuman(t *testing.T) {
	got := ClassifyComment(CommentFacts{
		CommentID:  comment,
		AuthorType: "member",
		AuthorID:   human,
	}, SourceCommentSource)

	if got.Source != SourceDirectHuman {
		t.Fatalf("source = %q, want direct_human", got.Source)
	}
	if got.UserID != human {
		t.Errorf("accountable user mismatch")
	}
	if got.EvidenceKind != EvidenceComment || got.EvidenceRefID != comment {
		t.Errorf("evidence = %q/%v, want comment/%v", got.EvidenceKind, got.EvidenceRefID, comment)
	}
	if got.DelegatedFromTaskID.Valid {
		t.Errorf("member-authored comment must not set delegated_from")
	}
}

func TestClassifyComment_AgentAuthoredInheritsParentAsDelegation(t *testing.T) {
	// Explicit mention path: an agent @-mentions another agent → delegation,
	// copying the parent task's human and recording the delegation source task.
	got := ClassifyComment(CommentFacts{
		CommentID:        comment,
		AuthorType:       "agent",
		AuthorID:         other,
		SourceTaskID:     srcTask,
		ParentOriginator: human,
	}, SourceDelegation)

	if got.Source != SourceDelegation {
		t.Fatalf("source = %q, want delegation", got.Source)
	}
	if got.UserID != human {
		t.Errorf("delegation must copy the parent's human, got %v", got.UserID)
	}
	if got.DelegatedFromTaskID != srcTask {
		t.Errorf("delegated_from = %v, want %v", got.DelegatedFromTaskID, srcTask)
	}
}

func TestClassifyComment_AgentAuthoredUsesCommentSourceLabelForAssigneePath(t *testing.T) {
	// Same facts, but the issue-assignee-reacting path passes comment_source.
	got := ClassifyComment(CommentFacts{
		CommentID:        comment,
		AuthorType:       "agent",
		SourceTaskID:     srcTask,
		ParentOriginator: human,
	}, SourceCommentSource)

	if got.Source != SourceCommentSource {
		t.Fatalf("source = %q, want comment_source", got.Source)
	}
	if got.UserID != human {
		t.Errorf("comment_source must inherit the parent's human")
	}
}

func TestClassifyComment_AgentAuthoredNoSourceTaskIsUnattributed(t *testing.T) {
	got := ClassifyComment(CommentFacts{
		CommentID:  comment,
		AuthorType: "agent",
	}, SourceDelegation)

	if got.Source != SourceUnattributed {
		t.Fatalf("source = %q, want unattributed", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("no source task must yield no human")
	}
}

func TestClassifyComment_AgentAuthoredParentWithoutHumanIsUnattributed(t *testing.T) {
	// Source task exists but has no human at its own top of chain (e.g. an
	// autopilot-originated parent). Must not fabricate a human, but should still
	// record the delegation lineage for evidence.
	got := ClassifyComment(CommentFacts{
		CommentID:    comment,
		AuthorType:   "agent",
		SourceTaskID: srcTask,
	}, SourceDelegation)

	if got.Source != SourceUnattributed {
		t.Fatalf("source = %q, want unattributed", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("must not fabricate a human when the parent has none")
	}
	if got.DelegatedFromTaskID != srcTask {
		t.Errorf("delegation lineage should still be recorded as evidence")
	}
}

func TestClassifyComment_SystemAuthoredIsUnattributed(t *testing.T) {
	got := ClassifyComment(CommentFacts{
		CommentID:  comment,
		AuthorType: "system",
	}, SourceCommentSource)
	if got.Source != SourceUnattributed {
		t.Fatalf("source = %q, want unattributed", got.Source)
	}
}

func TestClassifyDirect_MemberCreatorIsDirectHuman(t *testing.T) {
	got := ClassifyDirect(DirectFacts{
		IssueID:     issue,
		CreatorType: "member",
		CreatorID:   human,
	})
	if got.Source != SourceDirectHuman {
		t.Fatalf("source = %q, want direct_human", got.Source)
	}
	if got.UserID != human {
		t.Errorf("member-created issue must attribute to its creator")
	}
	if got.EvidenceKind != EvidenceIssueAssignment || got.EvidenceRefID != issue {
		t.Errorf("evidence should point at the issue")
	}
}

func TestClassifyDirect_QuickCreateInheritsOriginAsDelegation(t *testing.T) {
	got := ClassifyDirect(DirectFacts{
		IssueID:          issue,
		CreatorType:      "agent",
		OriginType:       "quick_create",
		OriginTaskID:     originTk,
		OriginOriginator: human,
	})
	if got.Source != SourceDelegation {
		t.Fatalf("source = %q, want delegation", got.Source)
	}
	if got.UserID != human {
		t.Errorf("quick-create issue must inherit the origin task's human")
	}
	if got.DelegatedFromTaskID != originTk {
		t.Errorf("delegated_from = %v, want %v", got.DelegatedFromTaskID, originTk)
	}
}

func TestClassifyDirect_QuickCreateWithoutHumanIsUnattributed(t *testing.T) {
	got := ClassifyDirect(DirectFacts{
		IssueID:      issue,
		CreatorType:  "agent",
		OriginType:   "quick_create",
		OriginTaskID: originTk,
	})
	if got.Source != SourceUnattributed {
		t.Fatalf("source = %q, want unattributed", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("must not fabricate a human")
	}
}

func TestClassifyDirect_AgentCreatedNoOriginIsUnattributed(t *testing.T) {
	got := ClassifyDirect(DirectFacts{
		IssueID:     issue,
		CreatorType: "agent",
	})
	if got.Source != SourceUnattributed {
		t.Fatalf("source = %q, want unattributed", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("agent-created issue with no origin has no human")
	}
}

func TestClassifyDirect_ActorOverridesCreator(t *testing.T) {
	// A member directly assigned/promoted an issue someone else created: the
	// acting member is accountable, ahead of the creator (MUL-4302 §4).
	got := ClassifyDirect(DirectFacts{
		IssueID:     issue,
		CreatorType: "member",
		CreatorID:   other,
		ActorUserID: human,
	})
	if got.Source != SourceDirectHuman {
		t.Fatalf("source = %q, want direct_human", got.Source)
	}
	if got.UserID != human {
		t.Errorf("actor should be attributed, got %v want %v", got.UserID, human)
	}
	if got.EvidenceKind != EvidenceIssueAssignment || got.EvidenceRefID != issue {
		t.Errorf("evidence should point at the assigned issue")
	}
}

func TestClassifyDirect_ActorOverridesAgentOriginInheritance(t *testing.T) {
	// Even when the issue carries an agent origin link, a real member actor wins:
	// a human directly acted on it, so we do not fall through to origin delegation.
	got := ClassifyDirect(DirectFacts{
		IssueID:          issue,
		CreatorType:      "agent",
		OriginType:       "agent_create",
		OriginTaskID:     originTk,
		OriginOriginator: other,
		ActorUserID:      human,
	})
	if got.Source != SourceDirectHuman {
		t.Fatalf("source = %q, want direct_human", got.Source)
	}
	if got.UserID != human {
		t.Errorf("actor should win over origin inheritance, got %v", got.UserID)
	}
	if got.DelegatedFromTaskID.Valid {
		t.Errorf("actor path is not a delegation; delegated_from must stay empty")
	}
}

func TestClassifyDirect_AgentCreateInheritsOriginAsDelegation(t *testing.T) {
	// agent_create (an agent's ordinary `issue create`, MUL-4305) inherits the
	// origin task's human exactly like quick_create.
	got := ClassifyDirect(DirectFacts{
		IssueID:          issue,
		CreatorType:      "agent",
		OriginType:       "agent_create",
		OriginTaskID:     originTk,
		OriginOriginator: human,
	})
	if got.Source != SourceDelegation {
		t.Fatalf("source = %q, want delegation", got.Source)
	}
	if got.UserID != human || got.DelegatedFromTaskID != originTk {
		t.Errorf("agent_create must inherit origin human + lineage")
	}
}

// TestAccountableMirrorsOriginatorInvariant is the MUL-4302 §11 acceptance check
// at the classification layer: EVERY result the resolver produces must satisfy
// `originator (UserID) IS NOT NULL ⟹ accountable == originator`, and when there is
// no human both sides are invalid. finalizeAttribution centralizes this; the test
// guards against a future Classify path forgetting to route through it.
func TestAccountableMirrorsOriginatorInvariant(t *testing.T) {
	results := []Result{
		ClassifyComment(CommentFacts{CommentID: comment, AuthorType: "member", AuthorID: human}, SourceCommentSource),
		ClassifyComment(CommentFacts{CommentID: comment, AuthorType: "agent", SourceTaskID: srcTask, ParentOriginator: human}, SourceDelegation),
		ClassifyComment(CommentFacts{CommentID: comment, AuthorType: "agent"}, SourceDelegation),
		ClassifyComment(CommentFacts{CommentID: comment, AuthorType: "system"}, SourceCommentSource),
		ClassifyDirect(DirectFacts{IssueID: issue, CreatorType: "member", CreatorID: human}),
		ClassifyDirect(DirectFacts{IssueID: issue, CreatorType: "member", CreatorID: other, ActorUserID: human}),
		ClassifyDirect(DirectFacts{IssueID: issue, CreatorType: "agent", OriginType: "quick_create", OriginTaskID: originTk, OriginOriginator: human}),
		ClassifyDirect(DirectFacts{IssueID: issue, CreatorType: "agent"}),
		DirectHumanRun(human, EvidenceComment, comment),
		DirectHumanRun(pgtype.UUID{}, "", pgtype.UUID{}),
		Unattributed(EvidenceAutopilotRun, srcTask),
	}
	for i, r := range results {
		if r.AccountableUserID != r.UserID {
			t.Errorf("result[%d]: accountable %v != originator %v — invariant violated", i, r.AccountableUserID, r.UserID)
		}
		if r.UserID.Valid != r.AccountableUserID.Valid {
			t.Errorf("result[%d]: validity mismatch originator=%v accountable=%v", i, r.UserID.Valid, r.AccountableUserID.Valid)
		}
	}
}

func TestDirectHumanRun(t *testing.T) {
	got := DirectHumanRun(human, EvidenceComment, comment)
	if got.Source != SourceDirectHuman || got.UserID != human || got.AccountableUserID != human {
		t.Errorf("valid member should be a direct_human originator+accountable, got %+v", got)
	}
	if got.EvidenceKind != EvidenceComment || got.EvidenceRefID != comment {
		t.Errorf("evidence should be carried through")
	}

	unresolved := DirectHumanRun(pgtype.UUID{}, "", pgtype.UUID{})
	if unresolved.Source != SourceUnattributed {
		t.Errorf("invalid user must degrade to unattributed, got %q", unresolved.Source)
	}
	if unresolved.UserID.Valid || unresolved.AccountableUserID.Valid {
		t.Errorf("invalid user must not fabricate a human")
	}
}

func TestUnattributed(t *testing.T) {
	got := Unattributed(EvidenceAutopilotRun, srcTask)
	if got.Source != SourceUnattributed {
		t.Fatalf("source = %q, want unattributed", got.Source)
	}
	if got.UserID.Valid || got.AccountableUserID.Valid {
		t.Errorf("unattributed must carry no human on either side")
	}
	if got.EvidenceKind != EvidenceAutopilotRun || got.EvidenceRefID != srcTask {
		t.Errorf("evidence should be carried so the row is not a NULL-source bypass")
	}
}

func TestSourcePrecise(t *testing.T) {
	precise := []Source{SourceDirectHuman, SourceDelegation, SourceCommentSource, SourceRuleOwner}
	degraded := []Source{SourceOwnerFallback, SourceBackfill, SourceUnattributed, Source("")}
	for _, s := range precise {
		if !s.Precise() {
			t.Errorf("%q should be precise", s)
		}
	}
	for _, s := range degraded {
		if s.Precise() {
			t.Errorf("%q should be degraded", s)
		}
	}
}

func TestSourceStringDefaultsToUnattributed(t *testing.T) {
	if Source("").String() != string(SourceUnattributed) {
		t.Errorf("empty source must stringify to unattributed, got %q", Source("").String())
	}
	if SourceDirectHuman.String() != "direct_human" {
		t.Errorf("unexpected string for direct_human: %q", SourceDirectHuman.String())
	}
}
