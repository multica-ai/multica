package execenv

import (
	"strings"
	"testing"
)

const issueStateTestIssueID = "55555555-6666-7777-8888-999999999999"

// coldIssueRead is the instruction every non-resumed run has always been
// handed. It is spelled out here rather than taken from the helper so a silent
// reword of the cold path fails this test instead of passing it.
const coldIssueRead = "Start by running `multica issue get " + issueStateTestIssueID +
	" --output json` to understand your task, then decide how to proceed.\n\n"

// TestBuildIssueStateHintUnchangedReplacesTheRead pins the one rendering that
// may stand in for workflow step 1: a real resume plus a server-computed
// comparison that found nothing. The read survives as a conditional fallback,
// never as an imperative.
func TestBuildIssueStateHintUnchangedReplacesTheRead(t *testing.T) {
	t.Parallel()
	hint := BuildIssueStateHint(issueStateTestIssueID, "in_progress", "agent", "agent-1", nil, true, true)

	for _, want := range []string{
		"The issue is unchanged since your last run",
		"the server compared title and description",
		"status: in_progress; assignee: agent agent-1",
		"That answers workflow step 1",
		"only if resumed memory is not enough",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("unchanged hint missing %q\n---\n%s", want, hint)
		}
	}
	if strings.Contains(hint, "Start by running") {
		t.Errorf("unchanged hint must not keep the unconditional read imperative\n---\n%s", hint)
	}
}

// TestBuildIssueStateHintNamesChangedFields: when the comparison DID find
// something, the agent is told which fields moved and reads the issue. Naming
// them is the point — a bare "it changed" leaves the agent diffing blind.
func TestBuildIssueStateHintNamesChangedFields(t *testing.T) {
	t.Parallel()
	hint := BuildIssueStateHint(issueStateTestIssueID, "todo", "member", "user-9",
		[]string{"description", "status"}, true, true)

	for _, want := range []string{
		"Since your last run the issue changed: description, status",
		"status: todo; assignee: member user-9",
		"Read it: `multica issue get " + issueStateTestIssueID + " --output json`",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("changed hint missing %q\n---\n%s", want, hint)
		}
	}
	if strings.Contains(hint, "unchanged") {
		t.Errorf("changed hint must not claim the issue is unchanged\n---\n%s", hint)
	}
}

// TestBuildIssueStateHintFallsBackToTheRead pins the safe default. Every state
// that is not "resumed AND compared" keeps the pre-existing instruction byte
// for byte: no prior context to continue from, or nothing looked.
func TestBuildIssueStateHintFallsBackToTheRead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		status        string
		changed       []string
		known, resume bool
	}{
		{name: "cold start", status: "", known: false, resume: false},
		{name: "resumed but delta unknown", status: "todo", known: false, resume: true},
		{name: "compared but resume dropped", status: "todo", known: true, resume: false},
		// An old server sends no issue state at all. Reporting a status we do
		// not have would be worse than the extra read, so the empty status
		// forces the cold rendering even with the flag somehow set.
		{name: "known flag without issue state", status: "", known: true, resume: true},
		{name: "changed fields without a real resume", status: "todo", changed: []string{"title"}, known: true, resume: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hint := BuildIssueStateHint(issueStateTestIssueID, tc.status, "agent", "agent-1", tc.changed, tc.known, tc.resume)
			if hint != coldIssueRead {
				t.Errorf("expected the unconditional read, got:\n%q", hint)
			}
		})
	}
}

// TestBuildIssueStateHintRendersUnassigned: a half-populated assignee pair is
// not an assignment, and must not read as one.
func TestBuildIssueStateHintRendersUnassigned(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, typ, id string }{
		{"both empty", "", ""},
		{"type without id", "agent", ""},
		{"id without type", "", "agent-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hint := BuildIssueStateHint(issueStateTestIssueID, "todo", tc.typ, tc.id, nil, true, true)
			if !strings.Contains(hint, "assignee: unassigned") {
				t.Errorf("expected an unassigned label, got:\n%s", hint)
			}
		})
	}
}
