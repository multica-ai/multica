package daemon

import (
	"strings"
	"testing"
)

// MUL-8218 / GH #9492: a resumed session may quote the pre-edit text of the
// trigger comment. The prompt must say which version is current without
// claiming the earlier run's work was undone.
func TestBuildCommentPromptEditedTrigger(t *testing.T) {
	base := Task{
		IssueID:                "issue-1",
		TriggerCommentID:       "comment-1",
		TriggerCommentContent:  "Now it connects over TLS",
		PriorSessionID:         "session-1",
		TriggerCommentEditedAt: "2026-10-26T02:20:00Z",
		TriggerCommentRevision: 3,
	}

	t.Run("edited on a resumed session", func(t *testing.T) {
		out := buildCommentPrompt(base, "claude")
		for _, want := range []string{
			"[EDITED COMMENT] A user edited a comment after your previous run in this session started.",
			"> Now it connects over TLS",
			"This comment (comment-1, revision 3) was edited at 2026-10-26T02:20:00Z.",
			"The text quoted in this prompt is the current version and the only one to act on",
			"still happened and was not rolled back",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("prompt missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "[NEW COMMENT]") {
			t.Error("edited trigger must not be announced as a new comment")
		}
	})

	t.Run("not edited", func(t *testing.T) {
		task := base
		task.TriggerCommentEditedAt = ""
		task.TriggerCommentRevision = 0
		out := buildCommentPrompt(task, "claude")
		if !strings.Contains(out, "[NEW COMMENT] A user just left a new comment.") {
			t.Errorf("unedited trigger must keep the new-comment header:\n%s", out)
		}
		if strings.Contains(out, "[EDITED COMMENT]") || strings.Contains(out, "was edited at") {
			t.Error("unedited trigger must not carry the edit note")
		}
	})

	// Without a resumed session there is no earlier version in history to
	// warn about; the daemon clears PriorSessionID when it drops the session.
	for name, mutate := range map[string]func(*Task){
		"cold start":         func(t *Task) { t.PriorSessionID = "" },
		"resume unavailable": func(t *Task) { t.PriorSessionResumeUnavailable = true },
	} {
		t.Run(name, func(t *testing.T) {
			task := base
			mutate(&task)
			out := buildCommentPrompt(task, "claude")
			if strings.Contains(out, "[EDITED COMMENT]") || strings.Contains(out, "was edited at") {
				t.Errorf("%s: edit note must only render on a resumed session:\n%s", name, out)
			}
		})
	}
}

// An edited comment folded behind a newer, unchanged trigger must still be
// marked as the current version (review of PR #9526).
func TestBuildCommentPromptEditedCoalescedComment(t *testing.T) {
	base := Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-2",
		TriggerCommentContent: "Please continue",
		PriorSessionID:        "session-1",
		CoalescedComments: []CoalescedCommentData{
			{ID: "comment-1", ThreadID: "thread-1", Content: "edited instruction Y", EditedAt: "2026-10-26T02:20:00Z", Revision: 2},
			{ID: "comment-0", ThreadID: "thread-1", Content: "unchanged instruction"},
		},
	}

	out := buildCommentPrompt(base, "claude")
	for _, want := range []string{
		"[NEW COMMENT] A user just left a new comment.",
		"- comment comment-1 (A user) [thread thread-1] [EDITED at 2026-10-26T02:20:00Z, revision 2 — current version]:",
		"- comment comment-0 (A user) [thread thread-1]:",
		"1 comment(s) above marked EDITED changed after your previous run in this session started.",
		"still happened and was not rolled back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[EDITED COMMENT]") {
		t.Error("an unchanged trigger must keep the new-comment header")
	}

	cold := base
	cold.PriorSessionID = ""
	if out := buildCommentPrompt(cold, "claude"); strings.Contains(out, "EDITED") {
		t.Errorf("cold start must not mark edits:\n%s", out)
	}
}
