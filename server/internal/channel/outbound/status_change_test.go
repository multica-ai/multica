package outbound

// TC-status-1~3 (PRD M3a): status change notifications for in_review, done,
// blocked. When an issue's status transitions to one of these three values,
// the outbound subscriber must enqueue a card notification for the issue's
// bound subscribers (excluding the actor who made the change). If the user's
// preference for the specific status kind is muted, the notification is
// dropped.
//
// The subscriber listens on EventIssueUpdated (already published by the
// handler layer with status_changed=true/false). When status_changed is true
// and the new status is in {in_review, done, blocked}, we enqueue a durable
// outbox notification with a status-specific template.

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// TC-status-1: in_review status change → card sent
// ---------------------------------------------------------------------------

func TestSubscriber_StatusChange_InReview_SendsCard(t *testing.T) {
	t.Parallel()

	bus := events.New()
	ch := &mockChannel{name: "feishu"}
	userID := "00000000-0000-0000-0000-000000000099"
	bindingStore := &mockBindingStore{
		bindings: map[string]map[string]string{
			"feishu": {"ext-user-1": userID},
		},
	}
	prefStore := &mockPrefStore{prefs: map[string]map[string]string{}}
	outbox := &mockOutbox{}

	sub := NewSubscriber(bus, ch, bindingStore, prefStore, "00000000-0000-0000-0000-000000000100")
	sub.SetNotificationEnqueuer(outbox)
	sub.Start()

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "00000000-0000-0000-0000-000000000100",
		ActorID:     "actor-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":            "issue-1",
				"workspace_id":  "00000000-0000-0000-0000-000000000100",
				"identifier":    "STA-1",
				"title":         "Test Issue",
				"status":        "in_review",
				"creator_type":  "member",
				"creator_id":    "00000000-0000-0000-0000-000000000001",
				"assignee_type": "member",
				"assignee_id":   userID,
			},
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	time.Sleep(10 * time.Millisecond)

	requests := outbox.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 outbox request for in_review, got %d", len(requests))
	}
	if requests[0].TargetChatID != "group-chat-1" {
		t.Errorf("TargetChatID = %q, want group-chat-1", requests[0].TargetChatID)
	}
	if requests[0].MentionExternalUserID != "ext-user-1" {
		t.Errorf("MentionExternalUserID = %q, want ext-user-1", requests[0].MentionExternalUserID)
	}
	if requests[0].Body == "" {
		t.Error("Body is empty; expected a status-change card template")
	}
}

// ---------------------------------------------------------------------------
// TC-status-2: done status change → card sent
// ---------------------------------------------------------------------------

func TestSubscriber_StatusChange_Done_SendsCard(t *testing.T) {
	t.Parallel()

	bus := events.New()
	ch := &mockChannel{name: "feishu"}
	userID := "00000000-0000-0000-0000-000000000099"
	bindingStore := &mockBindingStore{
		bindings: map[string]map[string]string{
			"feishu": {"ext-user-1": userID},
		},
	}
	prefStore := &mockPrefStore{prefs: map[string]map[string]string{}}
	outbox := &mockOutbox{}

	sub := NewSubscriber(bus, ch, bindingStore, prefStore, "00000000-0000-0000-0000-000000000100")
	sub.SetNotificationEnqueuer(outbox)
	sub.Start()

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "00000000-0000-0000-0000-000000000100",
		ActorID:     "actor-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":            "issue-1",
				"workspace_id":  "00000000-0000-0000-0000-000000000100",
				"identifier":    "STA-1",
				"title":         "Test Issue",
				"status":        "done",
				"creator_type":  "member",
				"creator_id":    "00000000-0000-0000-0000-000000000001",
				"assignee_type": "member",
				"assignee_id":   userID,
			},
			"status_changed": true,
			"prev_status":    "in_review",
		},
	})

	time.Sleep(10 * time.Millisecond)

	if got := len(outbox.Requests()); got != 1 {
		t.Fatalf("expected 1 outbox request for done, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// TC-status-3: blocked status change → card sent
// ---------------------------------------------------------------------------

func TestSubscriber_StatusChange_Blocked_SendsCard(t *testing.T) {
	t.Parallel()

	bus := events.New()
	ch := &mockChannel{name: "feishu"}
	userID := "00000000-0000-0000-0000-000000000099"
	bindingStore := &mockBindingStore{
		bindings: map[string]map[string]string{
			"feishu": {"ext-user-1": userID},
		},
	}
	prefStore := &mockPrefStore{prefs: map[string]map[string]string{}}
	outbox := &mockOutbox{}

	sub := NewSubscriber(bus, ch, bindingStore, prefStore, "00000000-0000-0000-0000-000000000100")
	sub.SetNotificationEnqueuer(outbox)
	sub.Start()

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "00000000-0000-0000-0000-000000000100",
		ActorID:     "actor-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":            "issue-1",
				"workspace_id":  "00000000-0000-0000-0000-000000000100",
				"identifier":    "STA-1",
				"title":         "Test Issue",
				"status":        "blocked",
				"creator_type":  "member",
				"creator_id":    "00000000-0000-0000-0000-000000000001",
				"assignee_type": "member",
				"assignee_id":   userID,
			},
			"status_changed": true,
			"prev_status":    "in_progress",
		},
	})

	time.Sleep(10 * time.Millisecond)

	if got := len(outbox.Requests()); got != 1 {
		t.Fatalf("expected 1 outbox request for blocked, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// TC-status-4: preference muted → drop
// ---------------------------------------------------------------------------

func TestSubscriber_StatusChange_PrefMuted_DropsEvent(t *testing.T) {
	t.Parallel()

	bus := events.New()
	ch := &mockChannel{name: "feishu"}
	userID := "00000000-0000-0000-0000-000000000099"
	bindingStore := &mockBindingStore{
		bindings: map[string]map[string]string{
			"feishu": {"ext-user-1": userID},
		},
	}
	prefStore := &mockPrefStore{
		prefs: map[string]map[string]string{
			userID: {
				"feishu.status_in_review": "muted",
			},
		},
	}
	outbox := &mockOutbox{}

	sub := NewSubscriber(bus, ch, bindingStore, prefStore, "00000000-0000-0000-0000-000000000100")
	sub.SetNotificationEnqueuer(outbox)
	sub.Start()

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "00000000-0000-0000-0000-000000000100",
		ActorID:     "actor-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":            "issue-1",
				"workspace_id":  "00000000-0000-0000-0000-000000000100",
				"identifier":    "STA-1",
				"title":         "Test Issue",
				"status":        "in_review",
				"creator_type":  "member",
				"creator_id":    "00000000-0000-0000-0000-000000000001",
				"assignee_type": "member",
				"assignee_id":   userID,
			},
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	time.Sleep(10 * time.Millisecond)

	if got := len(outbox.Requests()); got != 0 {
		t.Errorf("expected 0 outbox requests when status pref muted, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Defensive: status_changed=false → no card
// ---------------------------------------------------------------------------

func TestSubscriber_StatusChange_NotChanged_DropsEvent(t *testing.T) {
	t.Parallel()

	bus := events.New()
	ch := &mockChannel{name: "feishu"}
	userID := "00000000-0000-0000-0000-000000000099"
	bindingStore := &mockBindingStore{
		bindings: map[string]map[string]string{
			"feishu": {"ext-user-1": userID},
		},
	}
	prefStore := &mockPrefStore{prefs: map[string]map[string]string{}}
	outbox := &mockOutbox{}

	sub := NewSubscriber(bus, ch, bindingStore, prefStore, "00000000-0000-0000-0000-000000000100")
	sub.SetNotificationEnqueuer(outbox)
	sub.Start()

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "00000000-0000-0000-0000-000000000100",
		ActorID:     "actor-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":            "issue-1",
				"workspace_id":  "00000000-0000-0000-0000-000000000100",
				"identifier":    "STA-1",
				"title":         "Test Issue",
				"status":        "in_review",
				"creator_type":  "member",
				"creator_id":    "00000000-0000-0000-0000-000000000001",
				"assignee_type": "member",
				"assignee_id":   userID,
			},
			"status_changed": false,
			"prev_status":    "todo",
		},
	})

	time.Sleep(10 * time.Millisecond)

	if got := len(outbox.Requests()); got != 0 {
		t.Errorf("expected 0 outbox requests when status not changed, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Defensive: unsupported status (e.g. todo) → no card
// ---------------------------------------------------------------------------

func TestSubscriber_StatusChange_UnsupportedStatus_DropsEvent(t *testing.T) {
	t.Parallel()

	bus := events.New()
	ch := &mockChannel{name: "feishu"}
	userID := "00000000-0000-0000-0000-000000000099"
	bindingStore := &mockBindingStore{
		bindings: map[string]map[string]string{
			"feishu": {"ext-user-1": userID},
		},
	}
	prefStore := &mockPrefStore{prefs: map[string]map[string]string{}}
	outbox := &mockOutbox{}

	sub := NewSubscriber(bus, ch, bindingStore, prefStore, "00000000-0000-0000-0000-000000000100")
	sub.SetNotificationEnqueuer(outbox)
	sub.Start()

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "00000000-0000-0000-0000-000000000100",
		ActorID:     "actor-1",
		Payload: map[string]any{
			"issue": map[string]any{
				"id":            "issue-1",
				"workspace_id":  "00000000-0000-0000-0000-000000000100",
				"identifier":    "STA-1",
				"title":         "Test Issue",
				"status":        "todo",
				"creator_type":  "member",
				"creator_id":    "00000000-0000-0000-0000-000000000001",
				"assignee_type": "member",
				"assignee_id":   userID,
			},
			"status_changed": true,
			"prev_status":    "backlog",
		},
	})

	time.Sleep(10 * time.Millisecond)

	if got := len(outbox.Requests()); got != 0 {
		t.Errorf("expected 0 outbox requests for unsupported status 'todo', got %d", got)
	}
}
