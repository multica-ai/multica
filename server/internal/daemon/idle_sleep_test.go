package daemon

import (
	"sync"
	"testing"
)

type recordingIdleSleepAssertion struct {
	acquires int
	releases int
}

func (a *recordingIdleSleepAssertion) Acquire() error {
	a.acquires++
	return nil
}

func (a *recordingIdleSleepAssertion) Release() {
	a.releases++
}

func TestActiveTaskSleepAssertionIsReferenceCounted(t *testing.T) {
	assertion := &recordingIdleSleepAssertion{}
	d := &Daemon{idleSleepAssertion: assertion}

	d.taskStarted()
	d.taskStarted()
	if got := assertion.acquires; got != 1 {
		t.Fatalf("acquires after two starts = %d, want 1", got)
	}

	d.taskFinished()
	if got := assertion.releases; got != 0 {
		t.Fatalf("releases while one task remains = %d, want 0", got)
	}

	d.taskFinished()
	if got := assertion.releases; got != 1 {
		t.Fatalf("releases after the last task = %d, want 1", got)
	}

	d.taskStarted()
	d.releaseIdleSleepAssertion()
	if got := assertion.releases; got != 2 {
		t.Fatalf("releases on daemon shutdown with an active task = %d, want 2", got)
	}
}

func TestConcurrentTasksShareOneSleepAssertion(t *testing.T) {
	assertion := &recordingIdleSleepAssertion{}
	d := &Daemon{idleSleepAssertion: assertion}

	const taskCount = 20
	var wg sync.WaitGroup
	for range taskCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.taskStarted()
		}()
	}
	wg.Wait()
	if got := assertion.acquires; got != 1 {
		t.Fatalf("concurrent acquires = %d, want 1", got)
	}

	for range taskCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.taskFinished()
		}()
	}
	wg.Wait()
	if got := assertion.releases; got != 1 {
		t.Fatalf("concurrent releases = %d, want 1", got)
	}
}
