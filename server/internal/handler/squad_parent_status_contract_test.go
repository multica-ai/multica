package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// The two tests below are composition tests, not text-presence tests. The
// parent-status contract is split across two systems that never see each
// other: the squad briefing (server-side, appended to the leader's
// Instructions) and the runtime brief (daemon-side CLAUDE.md). Asserting each
// half in isolation is exactly how the original contradiction shipped — the
// briefing said "move it to in_review when the goal is met" while the runtime
// brief said "do not change status unless the comment asks", and a member's
// delivery comment never asks.
//
// So each test assembles both halves for one real scenario and asserts the
// combined instruction set points one way.

// leaderCommentRuntimeBrief renders the CLAUDE.md a squad leader receives on a
// comment-triggered turn.
func leaderCommentRuntimeBrief(t *testing.T, instructions string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := execenv.InjectRuntimeConfig(dir, "claude", execenv.TaskContextForEnv{
		IssueID:           "issue-1",
		TriggerCommentID:  "comment-1",
		AgentInstructions: instructions,
		IsSquadLeader:     true,
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	return string(data)
}

// TestSquadAssignedLeaderCanWrapUpOnCommentTurn covers the squad's most common
// shape: work dispatched by @mention with no child issues, so no child-done
// system comment ever arrives to carry an explicit status ask. The member
// simply posts "done". The leader must still be able to close the parent out.
func TestSquadAssignedLeaderCanWrapUpOnCommentTurn(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Owning Squad", "")

	// The issue is assigned to this squad → the server grants status ownership.
	briefing := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad, true)
	brief := leaderCommentRuntimeBrief(t, briefing)

	if !strings.Contains(briefing, "Own the parent issue status") {
		t.Fatalf("squad-assigned briefing must grant status ownership:\n%s", briefing)
	}

	// The runtime brief must not restate the prohibition in its absolute form,
	// which is what contradicted the grant. The absolute sentence ends right
	// after "explicitly asks for it"; the leader variant continues past it.
	if strings.Contains(brief, "explicitly asks for it\n") {
		t.Error("leader runtime brief still carries the unqualified no-status-change rule, " +
			"which contradicts the Own-the-parent-issue-status grant")
	}
	for _, want := range []string{
		// The carve-out must name the granting section, not gesture at it —
		// the leader has to be able to tell whether it applies to this turn.
		`Squad Operating Protocol's "Own the parent issue status"`,
		"only appears when this issue is assigned to your squad",
		"without waiting to be asked",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("leader runtime brief missing %q\n--- brief ---\n%s", want, brief)
		}
	}

	// End to end: both halves must agree that in_review is reachable here.
	combined := briefing + "\n" + brief
	if !strings.Contains(combined, "multica issue status <issue-id> in_review") {
		t.Error("combined instructions never tell the owning leader how to wrap up")
	}
}

// TestGuestLeaderCannotChangeStatusOnCommentTurn is the other half of the
// scope fix (MUL-3724 path): the issue belongs to a plain agent and this squad
// was only @mentioned for help. The briefing still gets injected — the leader
// needs its roster — but no combination of the two halves may authorize a
// status change on someone else's issue.
func TestGuestLeaderCannotChangeStatusOnCommentTurn(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Guest Squad", "")

	// The issue is assigned to someone else → no status ownership.
	briefing := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad, false)
	brief := leaderCommentRuntimeBrief(t, briefing)

	// The leader still gets the coordination context it was pulled in for —
	// withholding status authority must not withhold the roster too.
	for _, want := range []string{
		"## Squad Roster",
		"Leader (you):",
		"Delegate by @mention",
		"Record your evaluation",
	} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("guest leader lost coordination context %q:\n%s", want, briefing)
		}
	}

	// But the grant is absent, so the runtime brief's carve-out has nothing to
	// activate and the default prohibition governs.
	if strings.Contains(briefing, "Own the parent issue status") {
		t.Errorf("guest leader must not receive the status-ownership grant:\n%s", briefing)
	}
	combined := briefing + "\n" + brief
	if strings.Contains(combined, "multica issue status <issue-id> in_review") {
		t.Error("combined instructions hand a guest leader an in_review command for " +
			"an issue assigned to someone else")
	}
	// The prohibition wraps across source lines, so match on compacted text.
	compact := strings.Join(strings.Fields(briefing), " ")
	for _, want := range []string{
		"Do NOT change this issue's status",
		"never run `multica issue status` on it",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("guest-leader briefing missing %q\n--- briefing ---\n%s", want, briefing)
		}
	}
}
