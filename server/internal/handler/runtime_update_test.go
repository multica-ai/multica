package handler

import "testing"

func TestUpdateStoreScopesByDaemon(t *testing.T) {
	store := NewUpdateStore()

	daemon1 := "daemon-1"
	daemon2 := "daemon-2"

	if _, err := store.Create("runtime-a", &daemon1, "v1.2.3"); err != nil {
		t.Fatalf("create first update: %v", err)
	}

	if _, err := store.Create("runtime-b", &daemon1, "v1.2.3"); err == nil {
		t.Fatal("expected conflict for same daemon")
	}

	if _, err := store.Create("runtime-c", &daemon2, "v1.2.3"); err != nil {
		t.Fatalf("create second daemon update: %v", err)
	}

	pending := store.PopPending("daemon-1")
	if pending == nil {
		t.Fatal("expected pending update for daemon-1")
	}
	if pending.RuntimeID != "runtime-a" {
		t.Fatalf("expected runtime-a, got %s", pending.RuntimeID)
	}

	if got := store.PopPending("daemon-1"); got != nil {
		t.Fatal("expected daemon-1 update to already be claimed")
	}

	if got := store.PopPending("daemon-2"); got == nil || got.RuntimeID != "runtime-c" {
		t.Fatalf("expected daemon-2 pending update for runtime-c, got %#v", got)
	}
}

func TestUpdateStoreFallsBackToRuntimeScope(t *testing.T) {
	store := NewUpdateStore()

	if _, err := store.Create("runtime-a", nil, "v1.2.3"); err != nil {
		t.Fatalf("create first update: %v", err)
	}

	if _, err := store.Create("runtime-b", nil, "v1.2.3"); err != nil {
		t.Fatalf("expected distinct runtime fallback scopes, got %v", err)
	}
}
