package handler

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryUpdateStore_HasPending(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryUpdateStore()

	if has, err := store.HasPending(ctx, "rt-1"); err != nil || has {
		t.Fatalf("empty store should not report pending: has=%v err=%v", has, err)
	}

	if _, err := store.Create(ctx, "rt-1", "v1.2.3"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if has, err := store.HasPending(ctx, "rt-1"); err != nil || !has {
		t.Fatalf("expected pending=true after Create: has=%v err=%v", has, err)
	}
	if has, err := store.HasPending(ctx, "rt-2"); err != nil || has {
		t.Fatalf("expected pending=false for unrelated runtime: has=%v err=%v", has, err)
	}

	if _, err := store.PopPending(ctx, "rt-1"); err != nil {
		t.Fatalf("pop: %v", err)
	}
	if has, err := store.HasPending(ctx, "rt-1"); err != nil || has {
		t.Fatalf("expected pending=false after PopPending: has=%v err=%v", has, err)
	}
}

func TestInMemoryUpdateStore_PopPendingIgnoresTerminalHistory(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryUpdateStore()

	first, err := store.Create(ctx, "rt-1", "v1.2.3")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := store.Fail(ctx, first.ID, "allow next request"); err != nil {
		t.Fatalf("fail first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := store.Create(ctx, "rt-1", "v1.2.4")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	got, err := store.PopPending(ctx, "rt-1")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if got == nil || got.ID != second.ID {
		t.Fatalf("expected second request, got %+v", got)
	}
}

func TestInMemoryUpdateStore_RunningRequestTimesOut(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryUpdateStore()

	req, err := store.Create(ctx, "rt-timeout", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := store.PopPending(ctx, "rt-timeout")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if claimed == nil || claimed.Status != UpdateRunning {
		t.Fatalf("expected running request, got %+v", claimed)
	}
	if claimed.RunStartedAt == nil {
		t.Fatal("expected RunStartedAt after PopPending")
	}

	aged := time.Now().Add(-(updateRunningTimeout + time.Second))
	claimed.RunStartedAt = &aged
	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != UpdateTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error")
	}
}

func TestInMemoryUpdateStore_RejectsConcurrentActiveUntilTerminal(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryUpdateStore()

	req, err := store.Create(ctx, "rt-1", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, "rt-1", "v1.2.4"); err != errUpdateInProgress {
		t.Fatalf("second create error = %v, want errUpdateInProgress", err)
	}
	if err := store.Complete(ctx, req.ID, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := store.Create(ctx, "rt-1", "v1.2.4"); err != nil {
		t.Fatalf("create after terminal should succeed: %v", err)
	}
}
