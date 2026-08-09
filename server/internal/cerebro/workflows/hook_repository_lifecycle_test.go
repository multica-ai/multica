package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryHookRepositorySaveDraftKeepsLivePolicyEffective(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	actor := HookPermissionActor{Type: "member", ID: "member-1"}
	live := newTestHookPolicy("live-policy-4", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace})
	live.FamilyID = "family-1"
	live.Version = 4
	repo.Seed("workspace-1", live)

	draft, err := repo.Update(ctx, "workspace-1", actor, live.ID, HookPolicy{
		Name: "Draft changes",
		Mode: HookModeDryRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.ID == live.ID {
		t.Fatal("saving a Draft reused the Live policy ID")
	}
	if draft.FamilyID != live.FamilyID || draft.Revision != 1 {
		t.Fatalf("draft identity = %#v", draft)
	}
	if draft.Lifecycle.State != HookLifecycleLiveWithDraft || draft.Lifecycle.LivePolicyID != live.ID {
		t.Fatalf("draft lifecycle = %#v", draft.Lifecycle)
	}

	effective, err := repo.ListEffective(ctx, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) != 1 || effective[0].ID != live.ID || effective[0].Mode != HookModeEnforce {
		t.Fatalf("effective policies = %#v, want unchanged Live policy", effective)
	}
}

func TestMemoryHookRepositoryDiscardDraftPreservesLivePolicy(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	actor := HookPermissionActor{Type: "member", ID: "member-1"}
	live := newTestHookPolicy("live-policy-4", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace})
	live.FamilyID = "family-1"
	live.Version = 4
	repo.Seed("workspace-1", live)
	draft, err := repo.Update(ctx, "workspace-1", actor, live.ID, HookPolicy{Name: "Unwanted Draft"})
	if err != nil {
		t.Fatal(err)
	}

	remaining, err := repo.DiscardDraft(ctx, "workspace-1", actor, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.ID != live.ID || remaining.Lifecycle.State != HookLifecycleLive {
		t.Fatalf("remaining hook = %#v, want original Live policy", remaining)
	}
	effective, err := repo.ListEffective(ctx, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) != 1 || effective[0].ID != live.ID {
		t.Fatalf("effective policies after Discard = %#v", effective)
	}
}

func TestMemoryHookRepositoryPublishPromotesDraftAndClearsDraftPointer(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	actor := HookPermissionActor{Type: "member", ID: "member-1"}
	live := newTestHookPolicy("live-policy-4", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace})
	live.FamilyID = "family-1"
	live.Version = 4
	repo.Seed("workspace-1", live)
	draft, err := repo.Update(ctx, "workspace-1", actor, live.ID, HookPolicy{
		Name:            "Draft v5",
		ContractRule:    "Tasks must meet the completion rule.",
		ContractSatisfy: "Meet the completion rule before publishing.",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.RecordObservedRun("workspace-1", draft.ID)
	repo.MarkBaselineFresh("workspace-1", draft.ID)

	published, err := repo.Publish(ctx, "workspace-1", draft.ID, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.ID == live.ID || published.ID == draft.ID {
		t.Fatalf("published policy reused an existing identity: %#v", published)
	}
	if published.FamilyID != live.FamilyID || published.Version != 5 || published.Mode != HookModeEnforce {
		t.Fatalf("published policy = %#v", published)
	}
	if published.Lifecycle.State != HookLifecycleLive || published.Lifecycle.DraftID != "" {
		t.Fatalf("published lifecycle = %#v", published.Lifecycle)
	}

	effective, err := repo.ListEffective(ctx, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) != 1 || effective[0].ID != published.ID {
		t.Fatalf("effective policies = %#v, want promoted Draft", effective)
	}
	compatibility, err := repo.Get(ctx, "workspace-1", draft.ID)
	if err != nil {
		t.Fatalf("published Draft ID no longer resolves to its family: %v", err)
	}
	if compatibility.ID != published.ID || compatibility.FamilyID != draft.FamilyID {
		t.Fatalf("published Draft compatibility projection = %#v", compatibility)
	}
}

func TestMemoryHookRepositoryRejectsPublishingWithoutReadableContract(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	actor := HookPermissionActor{Type: "member", ID: "member-1"}
	draft, err := repo.Create(ctx, "workspace-1", actor, HookPolicy{Name: "Unreadable Draft"})
	if err != nil {
		t.Fatal(err)
	}
	repo.RecordObservedRun("workspace-1", draft.ID)
	repo.MarkBaselineFresh("workspace-1", draft.ID)

	if _, err := repo.Publish(ctx, "workspace-1", draft.ID, actor.ID); err == nil || !strings.Contains(err.Error(), "plain-language contract") {
		t.Fatalf("publish error = %v, want missing plain-language contract", err)
	}
}

func TestMemoryHookRepositoryRejectsStaleDraftRevision(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	actor := HookPermissionActor{Type: "member", ID: "member-1"}
	created, err := repo.Create(ctx, "workspace-1", actor, HookPolicy{Name: "Draft r1"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Update(ctx, "workspace-1", actor, created.ID, HookPolicy{Name: "Draft r2", Revision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 2 {
		t.Fatalf("saved revision = %d, want 2", saved.Revision)
	}

	_, err = repo.Update(ctx, "workspace-1", actor, created.ID, HookPolicy{Name: "Stale overwrite", Revision: created.Revision})
	if !errors.Is(err, ErrHookDraftRevisionStale) {
		t.Fatalf("stale save error = %v, want %v", err, ErrHookDraftRevisionStale)
	}
}

func TestMemoryHookRepositoryRetainsOnlyAllowlistedEventDataForSevenDays(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	event := HookEvent{
		EventID:     "event-1",
		Type:        HookBeforeTaskComplete,
		WorkspaceID: "workspace-1",
		IssueID:     "issue-1",
		SessionID:   "session-1",
		Context: map[string]any{
			"issue":  map[string]any{"status": "in_review", "secret": "remove-me"},
			"tool":   map[string]any{"name": "shell"},
			"prompt": "never-retain-me",
		},
		Proposed:    map[string]any{"status": "done", "message": "private"},
		BeforeState: map[string]any{"token": "private"},
	}

	retained, err := repo.CaptureEvent(ctx, "workspace-1", event)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ExpiresAt.Sub(retained.OccurredAt) != 7*24*time.Hour {
		t.Fatalf("retention = %s, want seven days", retained.ExpiresAt.Sub(retained.OccurredAt))
	}
	replayed, err := repo.ReplayEvent(ctx, "workspace-1", retained.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.WorkspaceID != "workspace-1" || replayed.IssueID != "issue-1" || replayed.SessionID != "session-1" {
		t.Fatalf("replayed identity = %#v", replayed)
	}
	issue, _ := replayed.Context["issue"].(map[string]any)
	if issue["status"] != "in_review" || issue["secret"] != nil {
		t.Fatalf("allowlisted issue context = %#v", issue)
	}
	if replayed.Context["prompt"] != nil || replayed.Context["tool"] != nil || replayed.Proposed != nil || replayed.BeforeState != nil {
		t.Fatalf("sensitive replay data retained: %#v", replayed)
	}
	if _, err := repo.ReplayEvent(ctx, "workspace-2", retained.ID); !errors.Is(err, ErrHookEventNotFound) {
		t.Fatalf("cross-workspace replay error = %v, want not found", err)
	}
	now = now.Add(7*24*time.Hour + time.Second)
	if _, err := repo.ReplayEvent(ctx, "workspace-1", retained.ID); !errors.Is(err, ErrHookEventNotFound) {
		t.Fatalf("expired replay error = %v, want not found", err)
	}
	if _, err := repo.CaptureEvent(ctx, "workspace-1", HookEvent{Type: HookEventType("unknown.event")}); !errors.Is(err, ErrHookEventNotRetainable) {
		t.Fatalf("unknown event capture error = %v, want not retainable", err)
	}
}
