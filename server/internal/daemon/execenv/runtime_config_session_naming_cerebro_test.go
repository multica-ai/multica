package execenv

import (
	"strings"
	"testing"
)

const (
	sessionNamingIssueID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionNamingRootID  = "11111111-2222-3333-4444-555555555555"
)

func TestSessionNamingBriefNamesNewTopLevelTrigger(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          sessionNamingIssueID,
		TriggerCommentID: sessionNamingRootID,
		TriggerThreadID:  sessionNamingRootID,
	})

	for _, want := range []string{
		"## Session naming for new threads",
		"This run was triggered by a new top-level thread.",
		"Before doing any other work, give this session a short, human-readable name that describes the thread's concrete purpose.",
		"`issue_id`: `" + sessionNamingIssueID + "`",
		"`root_comment_id`: `" + sessionNamingRootID + "`",
		"Do not use a generic name such as `Session 1`, `Plan`, `Build 1`, or `Review 1`.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("new top-level brief missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestSessionNamingBriefCoversAgentCreatedRootsInBothIssueWorkflows(t *testing.T) {
	t.Parallel()

	contexts := map[string]TaskContextForEnv{
		"comment-triggered": {
			IssueID:          sessionNamingIssueID,
			TriggerCommentID: "reply-comment-id",
			TriggerThreadID:  sessionNamingRootID,
		},
		"assignment-triggered": {
			IssueID: sessionNamingIssueID,
		},
	}

	for name, ctx := range contexts {
		name, ctx := name, ctx
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", ctx)
			for _, want := range []string{
				"Whenever you create a new top-level comment (no `--parent`)",
				"use the returned comment ID as the thread root",
				"immediately name the new session with `rename_session` or `multica issue session rename`",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("%s brief missing agent-created-thread rule %q\n--- output ---\n%s", name, want, out)
				}
			}
		})
	}
}

func TestSessionNamingBriefDoesNotTreatExistingThreadAsNew(t *testing.T) {
	t.Parallel()

	contexts := map[string]TaskContextForEnv{
		"reply": {
			IssueID:          sessionNamingIssueID,
			TriggerCommentID: "reply-comment-id",
			TriggerThreadID:  sessionNamingRootID,
		},
		"wakeup": {
			IssueID:           sessionNamingIssueID,
			TriggerCommentID:  sessionNamingRootID,
			TriggerThreadID:   sessionNamingRootID,
			WakeupPrompt:      "continue the original work",
			WakeupTriggerType: "time",
		},
	}

	for name, ctx := range contexts {
		name, ctx := name, ctx
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", ctx)
			if strings.Contains(out, "This run was triggered by a new top-level thread.") {
				t.Fatalf("%s brief incorrectly presents an existing thread as new:\n%s", name, out)
			}
			if !strings.Contains(out, "Whenever you create a new top-level comment (no `--parent`)") {
				t.Fatalf("%s brief lost the agent-created-thread rule:\n%s", name, out)
			}
		})
	}
}

func TestSessionNamingBriefSkipsNonIssueRuns(t *testing.T) {
	t.Parallel()

	contexts := map[string]TaskContextForEnv{
		"chat":         {ChatSessionID: "chat-1"},
		"quick-create": {QuickCreatePrompt: "create an issue"},
		"autopilot":    {AutopilotRunID: "run-1"},
	}

	for name, ctx := range contexts {
		name, ctx := name, ctx
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", ctx)
			if strings.Contains(out, "## Session naming for new threads") {
				t.Fatalf("%s brief must not include issue session naming guidance:\n%s", name, out)
			}
		})
	}
}

func TestSessionNamingBriefSurvivesWorkspaceBriefOff(t *testing.T) {
	t.Parallel()

	contexts := map[string]struct {
		ctx           TaskContextForEnv
		isCurrentRoot bool
	}{
		"top-level": {
			ctx: TaskContextForEnv{
				IssueID:            sessionNamingIssueID,
				TriggerCommentID:   sessionNamingRootID,
				TriggerThreadID:    sessionNamingRootID,
				WorkspaceBriefMode: "off",
			},
			isCurrentRoot: true,
		},
		"reply": {
			ctx: TaskContextForEnv{
				IssueID:            sessionNamingIssueID,
				TriggerCommentID:   "reply-comment-id",
				TriggerThreadID:    sessionNamingRootID,
				WorkspaceBriefMode: "off",
			},
		},
		"wakeup": {
			ctx: TaskContextForEnv{
				IssueID:            sessionNamingIssueID,
				TriggerCommentID:   sessionNamingRootID,
				TriggerThreadID:    sessionNamingRootID,
				WakeupPrompt:       "continue the original work",
				WorkspaceBriefMode: "off",
			},
		},
		"assignment": {
			ctx: TaskContextForEnv{
				IssueID:            sessionNamingIssueID,
				WorkspaceBriefMode: "off",
			},
		},
	}

	for name, tc := range contexts {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)
			if !strings.Contains(out, "Whenever you create a new top-level comment (no `--parent`)") {
				t.Fatalf("%s off-brief lost the agent-created-thread rule:\n%s", name, out)
			}
			gotCurrentRoot := strings.Contains(out, "This run was triggered by a new top-level thread.")
			if gotCurrentRoot != tc.isCurrentRoot {
				t.Fatalf("%s current-root instruction = %t, want %t:\n%s", name, gotCurrentRoot, tc.isCurrentRoot, out)
			}
			if tc.isCurrentRoot && (!strings.Contains(out, "`issue_id`: `"+sessionNamingIssueID+"`") || !strings.Contains(out, "`root_comment_id`: `"+sessionNamingRootID+"`")) {
				t.Fatalf("%s off-brief is missing exact current-root IDs:\n%s", name, out)
			}
		})
	}
}
