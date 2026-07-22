package failrouter

// FIR-3651: idle_watchdog had no entry in the routes table, so Lookup missed
// and FailTask fell back to Surface — a run stopped because its agent backend
// froze got no second attempt.

import "testing"

func TestIdleWatchdogRetriesOnFreshSession(t *testing.T) {
	t.Parallel()

	route, ok := Lookup("idle_watchdog")
	if !ok {
		t.Fatal("idle_watchdog has no route; FailTask will silently surface it")
	}
	if route.Action != ActionRetry {
		t.Errorf("Action = %q, want %q", route.Action, ActionRetry)
	}
	if !route.FreshSession {
		t.Error("a wedged session must not be resumed; want FreshSession")
	}
}

// TestRuntimePausedStaysUnrouted pins the deliberate omission: pause
// resumption is owned by UnpauseRuntime (see cerebro/runtime/pause.go), which
// keys on failure_reason='runtime_paused'. Giving it a retry route here would
// re-queue the task while its runtime is still paused.
func TestRuntimePausedStaysUnrouted(t *testing.T) {
	t.Parallel()

	if _, ok := Lookup("runtime_paused"); ok {
		t.Error("runtime_paused must stay unrouted — UnpauseRuntime owns resumption")
	}
}
