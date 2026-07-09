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
