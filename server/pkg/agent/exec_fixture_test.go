package agent

import "testing"

// CEREBRO-PATCH(agent-test-exec-stability): Bound fake CLI process concurrency in fork validation.
// External-process fixtures are intentionally capped below testing's default
// parallelism. Starting every fake CLI at once can consume most of a short
// per-test timeout before the child process gets scheduled, producing failures
// unrelated to the behavior under test.
var testExecutableSlots = make(chan struct{}, 4)

func acquireTestExecutableSlot(tb testing.TB) {
	tb.Helper()
	testExecutableSlots <- struct{}{}
	tb.Cleanup(func() {
		<-testExecutableSlots
	})
}
