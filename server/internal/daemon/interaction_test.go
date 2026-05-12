package daemon

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestInteractionRegistry_CreateAndGet(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{
		TaskID:   "task-1",
		Provider: "claude",
		Type:     protocol.InteractionCommandApproval,
		Title:    "Run rm -rf /tmp/foo",
		Options: []protocol.InteractionOption{
			{ID: "allow", Label: "Allow"},
			{ID: "deny", Label: "Deny"},
		},
	})

	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != protocol.InteractionStatusPending {
		t.Errorf("status = %q, want %q", got.Status, protocol.InteractionStatusPending)
	}
	if got.TaskID != "task-1" {
		t.Errorf("task_id = %q, want %q", got.TaskID, "task-1")
	}
	if got.ID != id {
		t.Errorf("id = %q, want %q", got.ID, id)
	}
}

func TestInteractionRegistry_GetNotFound(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	_, err := r.Get("nonexistent")
	if err != ErrInteractionNotFound {
		t.Fatalf("expected ErrInteractionNotFound, got %v", err)
	}
}

func TestInteractionRegistry_Respond(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{
		TaskID: "task-1",
		Type:   protocol.InteractionCommandApproval,
		Options: []protocol.InteractionOption{
			{ID: "allow", Label: "Allow"},
			{ID: "deny", Label: "Deny"},
		},
	})

	if err := r.Respond(id, "allow", ""); err != nil {
		t.Fatalf("Respond: %v", err)
	}

	got, _ := r.Get(id)
	if got.Status != protocol.InteractionStatusApproved {
		t.Errorf("status = %q, want %q", got.Status, protocol.InteractionStatusApproved)
	}
	if got.ChosenOption != "allow" {
		t.Errorf("chosen_option = %q, want %q", got.ChosenOption, "allow")
	}
	if got.RespondedAt == nil {
		t.Error("responded_at should be set")
	}
}

func TestInteractionRegistry_RespondDeny(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{
		TaskID: "task-1",
		Type:   protocol.InteractionCommandApproval,
	})

	if err := r.Respond(id, "deny", ""); err != nil {
		t.Fatalf("Respond: %v", err)
	}

	got, _ := r.Get(id)
	if got.Status != protocol.InteractionStatusDenied {
		t.Errorf("status = %q, want %q", got.Status, protocol.InteractionStatusDenied)
	}
}

func TestInteractionRegistry_RespondStoresResponseMessage(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{
		TaskID: "task-1",
		Type:   protocol.InteractionPlanApproval,
	})

	if err := r.Respond(id, "revise", "focus phase 1 first"); err != nil {
		t.Fatalf("Respond: %v", err)
	}

	got, _ := r.Get(id)
	if got.ResponseMessage != "focus phase 1 first" {
		t.Errorf("response_message = %q", got.ResponseMessage)
	}
	if got.Status != protocol.InteractionStatusDenied {
		t.Errorf("status = %q, want %q", got.Status, protocol.InteractionStatusDenied)
	}
}

func TestInteractionRegistry_RespondAlreadyResolved(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{TaskID: "task-1", Type: protocol.InteractionCommandApproval})
	_ = r.Respond(id, "allow", "")

	err := r.Respond(id, "deny", "")
	if err != ErrInteractionResolved {
		t.Fatalf("expected ErrInteractionResolved, got %v", err)
	}
}

func TestInteractionRegistry_Cancel(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{TaskID: "task-1", Type: protocol.InteractionPlanApproval})

	if err := r.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got, _ := r.Get(id)
	if got.Status != protocol.InteractionStatusCancelled {
		t.Errorf("status = %q, want %q", got.Status, protocol.InteractionStatusCancelled)
	}
}

func TestInteractionRegistry_CancelAlreadyResolved(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{TaskID: "task-1", Type: protocol.InteractionCommandApproval})
	_ = r.Respond(id, "allow", "")

	err := r.Cancel(id)
	if err != ErrInteractionResolved {
		t.Fatalf("expected ErrInteractionResolved, got %v", err)
	}
}

func TestInteractionRegistry_ListFilterByStatus(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	r.Create(protocol.InteractionRequest{TaskID: "t1", Type: protocol.InteractionCommandApproval})
	id2 := r.Create(protocol.InteractionRequest{TaskID: "t2", Type: protocol.InteractionFileChangeApproval})
	_ = r.Respond(id2, "allow", "")

	pending := r.List(protocol.InteractionStatusPending)
	if len(pending) != 1 {
		t.Errorf("pending count = %d, want 1", len(pending))
	}

	all := r.List("")
	if len(all) != 2 {
		t.Errorf("all count = %d, want 2", len(all))
	}

	approved := r.List(protocol.InteractionStatusApproved)
	if len(approved) != 1 {
		t.Errorf("approved count = %d, want 1", len(approved))
	}
}

func TestInteractionRegistry_Timeout(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	past := time.Now().Add(-time.Minute)
	r.Create(protocol.InteractionRequest{
		TaskID:    "task-1",
		Type:      protocol.InteractionCommandApproval,
		CreatedAt: past,
		ExpiresAt: past.Add(time.Second),
	})

	// Manually trigger expiry check.
	r.expireAt(time.Now())

	items := r.List(protocol.InteractionStatusTimedOut)
	if len(items) != 1 {
		t.Fatalf("timed_out count = %d, want 1", len(items))
	}
	if items[0].RespondedAt == nil {
		t.Error("responded_at should be set on timeout")
	}
}

func TestInteractionRegistry_TimeoutDoesNotAffectResolved(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	past := time.Now().Add(-time.Minute)
	id := r.Create(protocol.InteractionRequest{
		TaskID:    "task-1",
		Type:      protocol.InteractionCommandApproval,
		CreatedAt: past,
		ExpiresAt: past.Add(time.Second),
	})
	_ = r.Respond(id, "allow", "")

	r.expireAt(time.Now())

	got, _ := r.Get(id)
	if got.Status != protocol.InteractionStatusApproved {
		t.Errorf("status = %q, want %q (should not be overwritten by timeout)", got.Status, protocol.InteractionStatusApproved)
	}
}

func TestInteractionRegistry_CreateWithExplicitID(t *testing.T) {
	r := NewInteractionRegistry()
	defer r.Stop()

	id := r.Create(protocol.InteractionRequest{
		ID:     "custom-id-123",
		TaskID: "task-1",
		Type:   protocol.InteractionCommandApproval,
	})

	if id != "custom-id-123" {
		t.Errorf("id = %q, want %q", id, "custom-id-123")
	}

	got, err := r.Get("custom-id-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TaskID != "task-1" {
		t.Errorf("task_id = %q, want %q", got.TaskID, "task-1")
	}
}
