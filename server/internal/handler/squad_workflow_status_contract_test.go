package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Composition tests for MUL-8145, in the style of
// squad_parent_status_contract_test.go: the leader's status contract is split
// between the squad protocol (Agent Identity, which outranks the workflow) and
// the runtime brief. When the issue follows a project workflow, both halves
// must move the parent only through [Project workflow] — neither may name the
// built-in in_progress / in_review moves, which the workflow may not list and
// which can hand the parent to another step's handler mid-dispatch.
//
// They use the protocol text directly rather than a seeded squad, so they need
// no database; the roster is irrelevant to the status contract.

// leaderRuntimeBriefFor renders the runtime brief file a squad leader gets
// from provider, with the squad protocol as its Agent Identity.
func leaderRuntimeBriefFor(t *testing.T, provider, instructions string, hasProjectWorkflow bool) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := execenv.InjectRuntimeConfig(dir, provider, execenv.TaskContextForEnv{
		IssueID:            "issue-1",
		TriggerCommentID:   "comment-1",
		AgentInstructions:  instructions,
		IsSquadLeader:      true,
		HasProjectWorkflow: hasProjectWorkflow,
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig(%s): %v", provider, err)
	}
	name := "AGENTS.md"
	if provider == "claude" {
		name = "CLAUDE.md"
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// builtInParentMoves are the built-in status moves a leader must not be told
// to make on a workflow issue, in either half of its instructions.
var builtInParentMoves = []string{
	"multica issue status <issue-id> in_review",
	"move the parent to `in_progress`",
	"set `in_progress` FIRST",
	"moves to `in_review`",
	"always lands here",
}

func TestOwningLeaderInWorkflowMovesOnlyByWorkflowCommands(t *testing.T) {
	protocol := squadOperatingProtocolFor(true, leaderWorkflowCurrentDaemon)
	for _, provider := range []string{"claude", "codex"} {
		combined := strings.Join(strings.Fields(protocol+"\n"+leaderRuntimeBriefFor(t, provider, protocol, true)), " ")
		for _, move := range builtInParentMoves {
			if strings.Contains(combined, move) {
				t.Errorf("%s: workflow leader instructions still carry built-in move %q", provider, move)
			}
		}
		for _, want := range []string{
			// The ownership grant and its standing wrap-up authorization survive.
			"Own the parent issue status — through its project workflow.",
			"run that block's Step done command",
			"do it even when no comment asked you to",
			// Dispatch is not delivery, in workflow terms, in both halves.
			"The parent stays at its current step while members work",
			"a dispatch turn leaves the parent at its current step",
		} {
			if !strings.Contains(combined, want) {
				t.Errorf("%s: workflow leader instructions missing %q", provider, want)
			}
		}
	}
}

// Without a workflow the owning leader keeps the built-in arc, so issues
// outside a handoff workflow see no change.
func TestOwningLeaderWithoutWorkflowKeepsBuiltInMoves(t *testing.T) {
	protocol := squadOperatingProtocolFor(true, leaderNoWorkflow)
	if strings.Contains(protocol, "through its project workflow") {
		t.Fatalf("owner protocol without a workflow must be the built-in form:\n%s", protocol)
	}
	compact := strings.Join(strings.Fields(protocol), " ")
	for _, want := range []string{"move the parent to `in_progress`", "multica issue status <issue-id> in_review"} {
		if !strings.Contains(compact, want) {
			t.Errorf("owner protocol without a workflow lost %q", want)
		}
	}
}

// A guest leader gets the prohibition whether or not the issue has a
// workflow: the workflow never widens status authority.
func TestGuestLeaderProtocolIgnoresWorkflow(t *testing.T) {
	if squadOperatingProtocolFor(false, leaderWorkflowCurrentDaemon) != squadOperatingProtocolFor(false, leaderNoWorkflow) || squadOperatingProtocolFor(false, leaderWorkflowLegacyDaemon) != squadOperatingProtocolFor(false, leaderNoWorkflow) {
		t.Fatal("guest leader protocol must not vary with the project workflow")
	}
	if strings.Contains(squadOperatingProtocolFor(false, leaderWorkflowCurrentDaemon), "Step done") {
		t.Fatal("guest leader protocol must not hand out the Step done move")
	}
}

// A current daemon's runtime brief states no built-in moves, so the protocol
// forbids none; only an older daemon's brief, which still states them, gets
// the override (MUL-8145).
func TestOwningLeaderInWorkflowOverridesBuiltInMovesOnlyForLegacyDaemons(t *testing.T) {
	current := strings.Join(strings.Fields(squadOperatingProtocolFor(true, leaderWorkflowCurrentDaemon)), " ")
	legacy := strings.Join(strings.Fields(squadOperatingProtocolFor(true, leaderWorkflowLegacyDaemon)), " ")
	for _, word := range []string{"in_progress", "in_review"} {
		if strings.Contains(current, word) {
			t.Errorf("current-daemon protocol names %q, which nothing in its brief asks for", word)
		}
	}
	if !strings.Contains(legacy, "This overrides the in_progress / in_review rules in your runtime instructions") {
		t.Errorf("legacy-daemon protocol must override the built-in moves:\n%s", legacy)
	}
	if !strings.Contains(legacy, strings.Join(strings.Fields(squadParentStatusOwnedInWorkflow), " ")) {
		t.Errorf("legacy-daemon protocol must keep the workflow responsibility it extends")
	}
}
