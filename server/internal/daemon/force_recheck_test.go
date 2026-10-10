package daemon

import (
	"sort"
	"testing"
)

// TestDaemon_WokenRuntimeTracking pins the daemon side of #7452: a targeted
// `task_available` records its runtime so the next claim can force a server-side
// re-check, an empty wakeup (catch-up) is ignored, and draining clears the set
// so a runtime is forced exactly once per wakeup.
func TestDaemon_WokenRuntimeTracking(t *testing.T) {
	d := &Daemon{}

	// Nothing woken yet.
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("drain on empty set = %v, want nil", got)
	}

	// A catch-up wakeup carries no runtime id and must not enter the set.
	d.noteWokenRuntime("")
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("drain after empty wakeup = %v, want nil", got)
	}

	// Two targeted wakeups (with a duplicate) collapse to the distinct set.
	d.noteWokenRuntime("rt-1")
	d.noteWokenRuntime("rt-2")
	d.noteWokenRuntime("rt-1")
	got := d.drainWokenRuntimes()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "rt-1" || got[1] != "rt-2" {
		t.Fatalf("drain = %v, want [rt-1 rt-2]", got)
	}

	// Draining clears the set: the same runtime is not force-rechecked again
	// until it is woken anew.
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("drain after clear = %v, want nil", got)
	}
}

// TestClaimTasksBody_ForceRecheckIsOptional pins the request-compat contract of
// #7452: with no woken runtime the body is byte-for-byte the pre-#7452 request
// (so an older server sees no new field), and the field appears only when a
// runtime was actually woken.
func TestClaimTasksBody_ForceRecheckIsOptional(t *testing.T) {
	base := claimTasksBody("daemon-x", []string{"rt-1"}, 3)
	if _, present := base["force_recheck_runtime_ids"]; present {
		t.Fatalf("force_recheck_runtime_ids must be omitted when no runtime is woken; body = %v", base)
	}
	if base["daemon_id"] != "daemon-x" || base["max_tasks"] != 3 {
		t.Fatalf("base body lost its existing fields: %v", base)
	}

	withForce := claimTasksBody("daemon-x", []string{"rt-1"}, 3, "rt-1")
	ids, ok := withForce["force_recheck_runtime_ids"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "rt-1" {
		t.Fatalf("force_recheck_runtime_ids = %v, want [rt-1]", withForce["force_recheck_runtime_ids"])
	}
}
