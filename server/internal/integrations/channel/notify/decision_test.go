package notify

import "testing"

func TestDecide(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		notifType       string
		effectiveStatus string
		want            Decision
	}{
		// in_review requests a decision on completed work; blocked requests the
		// input or intervention needed to resume it. Mirrors
		// delegatedStatusNotify's reasoning in notification_listeners.go.
		{"in_review pushes and is replyable", "status_changed", "in_review", Decision{Push: true, Replyable: true}},
		// A custom status is normalised to its category by the caller, so it
		// arrives here already reading "in_review".
		{"custom status in the in_review category pushes", "status_changed", "in_review", Decision{Push: true, Replyable: true}},
		{"done does not push", "status_changed", "done", Decision{}},
		{"in_progress does not push", "status_changed", "in_progress", Decision{}},
		{"backlog does not push", "status_changed", "backlog", Decision{}},
		{"cancelled does not push", "status_changed", "cancelled", Decision{}},
		{"blocked pushes and is replyable", "status_changed", "blocked", Decision{Push: true, Replyable: true}},
		{"empty status does not push", "status_changed", "", Decision{}},

		// task_failed carries ReasonAgentBlocked ("Waiting on human input"),
		// which is the existing vehicle for "the agent needs a human".
		{"task_failed pushes and is replyable", "task_failed", "", Decision{Push: true, Replyable: true}},
		{"task_failed ignores status", "task_failed", "in_progress", Decision{Push: true, Replyable: true}},

		// Quick-create failures have no issue, so there is nothing to inject a
		// reply into. They still push — the user asked for something and it
		// did not happen.
		{"quick_create_failed pushes but is not replyable", "quick_create_failed", "", Decision{Push: true}},
		{"quick_create_unconfirmed pushes but is not replyable", "quick_create_unconfirmed", "", Decision{Push: true}},
		{"workspace_idle pushes but is not replyable", "workspace_idle", "", Decision{Push: true}},

		// Everything else is noise for an IM DM.
		{"new_comment does not push", "new_comment", "", Decision{}},
		{"mentioned does not push", "mentioned", "", Decision{}},
		{"issue_assigned does not push", "issue_assigned", "", Decision{}},
		{"agent_completed does not push", "agent_completed", "", Decision{}},
		{"task_completed does not push", "task_completed", "", Decision{}},
		{"unknown type does not push", "some_future_type", "in_review", Decision{}},
		{"empty type does not push", "", "in_review", Decision{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Decide(tt.notifType, tt.effectiveStatus); got != tt.want {
				t.Errorf("Decide(%q, %q) = %+v, want %+v", tt.notifType, tt.effectiveStatus, got, tt.want)
			}
		})
	}
}

// Replyable must never be true without Push: an unpushed notification has no
// message for anyone to reply to, and the ledger would record a dangling row.
func TestDecideNeverReplyableWithoutPush(t *testing.T) {
	t.Parallel()
	types := []string{
		"status_changed", "task_failed", "quick_create_failed",
		"quick_create_unconfirmed", "workspace_idle", "new_comment", "mentioned", "",
	}
	statuses := []string{"", "backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
	for _, ty := range types {
		for _, st := range statuses {
			if d := Decide(ty, st); d.Replyable && !d.Push {
				t.Errorf("Decide(%q, %q) = %+v: replyable without push", ty, st, d)
			}
		}
	}
}
