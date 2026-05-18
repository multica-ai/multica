package conversation

import "testing"

func TestFilterIssueEntityRefsByPrefix(t *testing.T) {
	t.Parallel()
	refs := ExtractIssueEntityRefs("ws-1", "STA-82 covers AC-2, AC-5 and MUL-9", EntityRoleMentioned)
	got := FilterIssueEntityRefsByPrefix(refs, "STA")
	if len(got) != 1 {
		t.Fatalf("filtered refs = %+v, want only STA-82", got)
	}
	if got[0].EntityKey != "STA-82" {
		t.Fatalf("EntityKey = %q, want STA-82", got[0].EntityKey)
	}
}

func TestFilterIssueEntityRefsByPrefixKeepsNonIssueEntities(t *testing.T) {
	t.Parallel()
	refs := []EntityRef{
		{EntityType: EntityTypeIssue, EntityKey: "AC-2"},
		{EntityType: EntityTypeAgent, EntityID: "agent-1"},
	}
	got := FilterIssueEntityRefsByPrefix(refs, "STA")
	if len(got) != 1 || got[0].EntityType != EntityTypeAgent {
		t.Fatalf("filtered refs = %+v, want non-issue entity kept", got)
	}
}
