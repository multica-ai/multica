package daemon

import (
	"log/slog"
	"sync"
	"testing"
)

func TestClaimPauseRefcount_IndependentHolders(t *testing.T) {
	d := &Daemon{logger: slog.Default()}

	// drain + server-update + below-minimum each take a ref; releasing one
	// must never clear the others (NEX-38 decision two / AC-17).
	d.acquireClaimPause(claimPauseDrain)
	d.acquireClaimPause(claimPauseServerUpdate)
	d.acquireClaimPause(claimPauseDemotion)
	if !d.claimPaused() {
		t.Fatal("claims must be paused while any holder has a ref")
	}
	if d.claimPauseTotal != 3 {
		t.Fatalf("claimPauseTotal = %d, want 3", d.claimPauseTotal)
	}

	// server-update finishes first — drain and demotion must survive.
	d.releaseClaimPause(claimPauseServerUpdate)
	if !d.claimPaused() {
		t.Fatal("releasing server-update cleared the drain/demotion pause")
	}
	if d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must still refuse while drain holds a ref")
	}
	d.exitClaim()

	// drain aborts — demotion must survive.
	d.releaseClaimPause(claimPauseDrain)
	if !d.claimPaused() {
		t.Fatal("releasing drain cleared the demotion pause")
	}

	// Last holder releases — claiming resumes.
	d.releaseClaimPause(claimPauseDemotion)
	if d.claimPaused() {
		t.Fatal("claims must resume when the last ref is released")
	}
	if d.claimPauseTotal != 0 {
		t.Fatalf("claimPauseTotal = %d, want 0", d.claimPauseTotal)
	}
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must succeed after all refs are released")
	}
	d.exitClaim()
}

func TestClaimPauseRefcount_DoubleAcquireIsBalanced(t *testing.T) {
	d := &Daemon{logger: slog.Default()}

	// Two acquires by the same holder are ref-counted, so a single release
	// keeps the pause held until the second release.
	d.acquireClaimPause(claimPauseServerUpdate)
	d.acquireClaimPause(claimPauseServerUpdate)
	d.releaseClaimPause(claimPauseServerUpdate)
	if !d.claimPaused() {
		t.Fatal("claims must stay paused after one of two server-update refs is released")
	}
	d.releaseClaimPause(claimPauseServerUpdate)
	if d.claimPaused() {
		t.Fatal("claims must resume after the last server-update ref is released")
	}
}

func TestReleaseClaimPause_UnbalancedReleasePanics(t *testing.T) {
	d := &Daemon{logger: slog.Default()}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("releasing a ref that was never acquired must panic")
		}
	}()
	d.releaseClaimPause(claimPauseDrain)
}

// TestClaimPauseRefcount_ConcurrentInterleaving stresses AC-17: concurrent
// acquire/release across all three holders must never leave the daemon paused
// when no ref is held, nor unpaused while any ref is held.
func TestClaimPauseRefcount_ConcurrentInterleaving(t *testing.T) {
	d := &Daemon{logger: slog.Default()}

	holders := []claimPauseHolder{claimPauseDrain, claimPauseServerUpdate, claimPauseDemotion}
	var wg sync.WaitGroup
	for _, h := range holders {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(h claimPauseHolder) {
				defer wg.Done()
				d.acquireClaimPause(h)
				if !d.claimPaused() {
					t.Error("claims must be paused while a ref is held")
				}
				d.releaseClaimPause(h)
			}(h)
		}
	}
	wg.Wait()

	if d.claimPaused() {
		t.Fatal("claims must not stay paused after every ref is released")
	}
	if d.claimPauseTotal != 0 {
		t.Fatalf("claimPauseTotal = %d, want 0", d.claimPauseTotal)
	}
	for _, h := range holders {
		if d.claimPauseRefs[h] != 0 {
			t.Fatalf("holder %q refcount = %d, want 0", h, d.claimPauseRefs[h])
		}
	}
}

func TestDrainRefInteractsWithClaimBarrier(t *testing.T) {
	d := &Daemon{logger: slog.Default()}

	// A drain holding a ref defers auto-update / demotion barrier acquisition
	// (decision two: drain > server-update > below-minimum).
	d.acquireClaimPause(claimPauseDrain)
	if d.trySetClaimBarrier() {
		t.Fatal("trySetClaimBarrier must defer while drain is in effect")
	}
	d.releaseClaimPause(claimPauseDrain)

	// Once the drain ends the barrier can be acquired normally.
	if !d.trySetClaimBarrier() {
		t.Fatal("trySetClaimBarrier must succeed after the drain ends")
	}
	if d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must refuse while the server-update barrier is held")
	}
	d.releaseClaimBarrier()
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must succeed after the barrier is released")
	}
	d.exitClaim()
}
