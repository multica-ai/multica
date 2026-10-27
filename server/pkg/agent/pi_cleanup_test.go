package agent

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

type failedPiProcessTracker struct{ signalled, closed bool }

func (*failedPiProcessTracker) unobservedTools() bool { return false }

func (*failedPiProcessTracker) snapshot(bool) error {
	return errors.New("cannot inspect process identity")
}
func (p *failedPiProcessTracker) signal(syscall.Signal) error { p.signalled = true; return nil }
func (*failedPiProcessTracker) remaining() ([]int, error)     { return nil, nil }
func (p *failedPiProcessTracker) close()                      { p.closed = true }

func TestPiCleanupInspectionFailureIsNotSuccess(t *testing.T) {
	tracker := &failedPiProcessTracker{}
	observed := make(chan struct{})
	close(observed)
	c := &piRunCleanup{tracker: tracker, done: make(chan struct{}), stop: make(chan struct{}), observed: observed}
	pipesClosed := false
	err := c.finish(&exec.Cmd{}, func() {}, func() {
		if !tracker.signalled {
			t.Error("output pipes released before cleanup finished")
		}
		pipesClosed = true
	}, time.Millisecond)
	if err == nil || !pipesClosed || !tracker.closed {
		t.Fatalf("inspection failure was lost: err=%v pipesClosed=%v", err, pipesClosed)
	}
}

type recoveringPiProcessTracker struct {
	failedPiProcessTracker
	failNext bool
}

func (p *recoveringPiProcessTracker) snapshot(bool) error {
	if p.failNext {
		p.failNext = false
		return errors.New("temporary process inspection failure")
	}
	return nil
}

func TestPiCleanupObservationFailureCanRecover(t *testing.T) {
	tracker := &recoveringPiProcessTracker{failNext: true}
	observed := make(chan struct{})
	close(observed)
	c := &piRunCleanup{tracker: tracker, done: make(chan struct{}), stop: make(chan struct{}), observed: observed}
	c.snapshot() // A failed observation is not proof that final cleanup failed.
	if err := c.finish(&exec.Cmd{}, func() {}, func() {}, time.Millisecond); err != nil {
		t.Fatalf("successful final verification retained a transient failure: %v", err)
	}
}

func TestPiCleanupNormalCompletionDoesNotSignal(t *testing.T) {
	// Even an unavailable process-inspection API must not change a successful
	// run into cleanup_unconfirmed when no cancellation was requested.
	tracker := &failedPiProcessTracker{}
	observed := make(chan struct{})
	close(observed)
	c := &piRunCleanup{tracker: tracker, done: make(chan struct{}), stop: make(chan struct{}), observed: observed}
	c.snapshot()
	if err := c.complete(func() {}, func() {}); err != nil || tracker.signalled || !tracker.closed {
		t.Fatalf("normal completion: err=%v signalled=%v closed=%v", err, tracker.signalled, tracker.closed)
	}
}

type signalRacePiProcessTracker struct {
	failedPiProcessTracker
	checksUntilExit int
	verificationErr error
}

func (*signalRacePiProcessTracker) snapshot(bool) error         { return nil }
func (*signalRacePiProcessTracker) signal(syscall.Signal) error { return syscall.EPERM }
func (p *signalRacePiProcessTracker) remaining() ([]int, error) {
	if p.verificationErr != nil {
		return nil, p.verificationErr
	}
	if p.checksUntilExit > 0 {
		p.checksUntilExit--
		return []int{42}, nil
	}
	return nil, nil
}

func TestPiCleanupSignalRaceRequiresFinalVerification(t *testing.T) {
	for _, tc := range []struct {
		name            string
		checksUntilExit int
		verificationErr error
	}{
		{name: "already exited"},
		{name: "exec race recovers", checksUntilExit: 2},
		{name: "cannot verify exit", verificationErr: syscall.EPERM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &signalRacePiProcessTracker{checksUntilExit: tc.checksUntilExit, verificationErr: tc.verificationErr}
			observed := make(chan struct{})
			close(observed)
			c := &piRunCleanup{tracker: tracker, done: make(chan struct{}), stop: make(chan struct{}), observed: observed}
			err := c.finish(&exec.Cmd{}, func() {}, func() {}, 0)
			if (err != nil) != (tc.verificationErr != nil) {
				t.Fatalf("cleanup err=%v, verification err=%v", err, tc.verificationErr)
			}
		})
	}
}
