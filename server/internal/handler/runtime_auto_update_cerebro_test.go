package handler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resetAutoUpdateGate clears the process-local cooldown state so each test
// starts from a clean gate.
func resetAutoUpdateGate(t *testing.T) {
	t.Helper()
	autoUpdateGate.Lock()
	defer autoUpdateGate.Unlock()
	autoUpdateGate.nextAllowed = make(map[string]time.Time)
}

func TestReserveAutoUpdateSlotHoldsForCooldown(t *testing.T) {
	resetAutoUpdateGate(t)
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	if !reserveAutoUpdateSlot("rt-1", start) {
		t.Fatal("first reservation must be allowed")
	}
	if reserveAutoUpdateSlot("rt-1", start.Add(15*time.Second)) {
		t.Fatal("second reservation one heartbeat later must be refused")
	}
	if reserveAutoUpdateSlot("rt-1", start.Add(autoUpdateCooldown-time.Second)) {
		t.Fatal("reservation just before the cooldown expires must be refused")
	}
	if !reserveAutoUpdateSlot("rt-1", start.Add(autoUpdateCooldown)) {
		t.Fatal("reservation after the cooldown must be allowed")
	}
}

func TestReserveAutoUpdateSlotIsPerRuntime(t *testing.T) {
	resetAutoUpdateGate(t)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	if !reserveAutoUpdateSlot("rt-1", now) {
		t.Fatal("rt-1 must be allowed")
	}
	if !reserveAutoUpdateSlot("rt-2", now) {
		t.Fatal("rt-2 must not inherit rt-1's cooldown")
	}
}

// TestReserveAutoUpdateSlotBoundsHeartbeatLoop pins the FIR-4369 regression: a
// runtime whose reported version never reaches the release line used to get one
// update order per 15s heartbeat, forever. An hour of heartbeats must now yield
// exactly one.
func TestReserveAutoUpdateSlotBoundsHeartbeatLoop(t *testing.T) {
	resetAutoUpdateGate(t)
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	scheduled := 0
	for elapsed := time.Duration(0); elapsed < autoUpdateCooldown; elapsed += 15 * time.Second {
		if reserveAutoUpdateSlot("rt-1", start.Add(elapsed)) {
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Fatalf("scheduled %d updates in one cooldown window, want 1", scheduled)
	}
}

func TestShouldScheduleRuntimeUpdate(t *testing.T) {
	old := db.AgentRuntime{CliVersion: pgtype.Text{String: "1.0.0", Valid: true}, Metadata: []byte(`{}`)}
	if !shouldScheduleRuntimeUpdate(old, "v1.1.0") {
		t.Fatal("old standalone runtime should receive an update")
	}
	current := old
	current.CliVersion.String = "1.1.0"
	if shouldScheduleRuntimeUpdate(current, "v1.1.0") {
		t.Fatal("current runtime must not receive an update")
	}
	desktop := old
	desktop.Metadata = []byte(`{"launched_by":"desktop"}`)
	if shouldScheduleRuntimeUpdate(desktop, "v1.1.0") {
		t.Fatal("desktop-managed runtime must not self-update")
	}
}
