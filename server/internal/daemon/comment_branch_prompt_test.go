package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommentBranchPromptUsesOnlyFrozenPath(t *testing.T) {
	snapshot := map[string]any{
		"version": 1, "captured_at": "2026-08-16T00:01:00Z", "branch_point_comment_id": "selected",
		"issue": map[string]any{
			"id": "issue-1", "identifier": "MUL-1", "title": "Frozen title",
			"description": "Frozen description", "revision": 7,
		},
		"comments": []map[string]any{
			{"id": "root", "author_type": "member", "author_name": "A", "content": "root body", "created_at": "2026-08-16T00:00:00Z"},
			{"id": "selected", "parent_id": "root", "author_type": "member", "author_name": "B", "content": "selected body", "created_at": "2026-08-16T00:01:00Z"},
		},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	out := BuildPrompt(Task{
		IssueID: "issue-1", BranchPointCommentID: "selected", BranchContext: raw,
	}, "codex")

	for _, expected := range []string{
		"Frozen title", "Frozen description", "root body", "selected body",
		"root (A, 2026-08-16T00:00:00Z)",
		"selected (B, 2026-08-16T00:01:00Z)",
		"Do not scan the issue timeline", "selected",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("branch prompt missing %q:\n%s", expected, out)
		}
	}
	if strings.Contains(out, "--roots-only --summary") {
		t.Fatalf("branch prompt included the normal history-scan command:\n%s", out)
	}
	if strings.Contains(out, "comment add issue-1 --parent") {
		t.Fatalf("branch prompt instructed the result to become a nested reply:\n%s", out)
	}
	if !strings.Contains(out, "Post exactly one result as a new top-level comment") {
		t.Fatalf("branch prompt lost the top-level result requirement:\n%s", out)
	}
}

func TestCommentBranchPromptOmitsLiveSiblingRunContext(t *testing.T) {
	snapshot := map[string]any{
		"version": 1, "captured_at": "2026-08-16T00:01:00Z", "branch_point_comment_id": "selected",
		"issue": map[string]any{
			"id": "issue-1", "identifier": "MUL-1", "title": "Frozen title", "revision": 7,
		},
		"comments": []map[string]any{
			{"id": "selected", "author_type": "member", "author_name": "B", "content": "selected body", "created_at": "2026-08-16T00:01:00Z"},
		},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	out := BuildPrompt(Task{
		IssueID:              "issue-1",
		BranchPointCommentID: "selected",
		BranchContext:        raw,
		ActiveSiblingRuns: []ActiveSiblingRunData{{
			TaskID: "later-task", IssueID: "issue-1", IssueIdentifier: "MUL-1", Status: "running",
		}},
	}, "codex")

	for _, banned := range []string{
		"Active sibling runs",
		"multica issue comment list issue-1",
		"multica issue run-messages later-task",
	} {
		if strings.Contains(out, banned) {
			t.Fatalf("comment branch prompt escaped the frozen ancestry through %q:\n%s", banned, out)
		}
	}
}

func TestCommentBranchPromptNeverInheritsSquadLeaderNoAction(t *testing.T) {
	snapshot := map[string]any{
		"version": 1, "captured_at": "2026-08-16T00:01:00Z", "branch_point_comment_id": "selected",
		"issue": map[string]any{
			"id": "issue-1", "identifier": "MUL-1", "title": "Frozen title", "revision": 7,
		},
		"comments": []map[string]any{
			{"id": "selected", "author_type": "member", "author_name": "B", "content": "selected body", "created_at": "2026-08-16T00:01:00Z"},
		},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	out := BuildPrompt(Task{
		IssueID:              "issue-1",
		BranchPointCommentID: "selected",
		BranchContext:        raw,
		LeaderRoleResolved:   true,
		IsLeaderTask:         true,
		SquadID:              "squad-1",
		Agent: &AgentData{
			Instructions: "## Squad Operating Protocol\n\nA stale coordinator briefing.",
		},
	}, "codex")

	for _, banned := range []string{
		"Unless your outcome is `no_action`",
		"multica squad activity",
	} {
		if strings.Contains(out, banned) {
			t.Fatalf("comment branch inherited squad-leader behavior %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "Post exactly one result as a new top-level comment") {
		t.Fatalf("comment branch lost the unconditional reply requirement:\n%s", out)
	}
	if taskIsSquadLeader(Task{
		BranchContext:      raw,
		LeaderRoleResolved: true,
		IsLeaderTask:       true,
		SquadID:            "squad-1",
	}) {
		t.Fatal("comment branch was classified as a squad-leader task")
	}
}

func TestCommentBranchPromptRejectsInvalidSnapshots(t *testing.T) {
	t.Run("unknown version", func(t *testing.T) {
		out := BuildPrompt(Task{BranchContext: json.RawMessage(`{"version":2}`)}, "codex")
		if !strings.Contains(out, "snapshot is invalid") {
			t.Fatalf("unexpected invalid-snapshot prompt: %s", out)
		}
	})

	t.Run("truncated ancestry", func(t *testing.T) {
		snapshot := map[string]any{
			"version": 1, "captured_at": "2026-08-16T00:01:00Z", "branch_point_comment_id": "selected",
			"issue": map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "title": "Frozen title", "revision": 7,
			},
			"comments": []map[string]any{
				{"id": "selected", "parent_id": "missing", "author_type": "member", "content": "selected body", "created_at": "2026-08-16T00:01:00Z"},
			},
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}

		out := BuildPrompt(Task{
			IssueID: "issue-1", BranchPointCommentID: "selected", BranchContext: raw,
		}, "codex")
		if !strings.Contains(out, "snapshot is invalid") {
			t.Fatalf("unexpected truncated-snapshot prompt: %s", out)
		}
	})
}
