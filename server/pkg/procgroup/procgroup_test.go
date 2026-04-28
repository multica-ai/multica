package procgroup

import (
	"os/exec"
	"testing"
)

func TestNew_ReturnsNonNil(t *testing.T) {
	startCreated := Counters().JobsCreated
	job, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer job.Close()

	if job == nil {
		t.Fatal("New returned nil Job")
	}
	if got := Counters().JobsCreated; got != startCreated+1 {
		t.Errorf("JobsCreated did not increment: was %d, now %d", startCreated, got)
	}
}

func TestClose_IsIdempotent(t *testing.T) {
	job, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	startClosed := Counters().JobsClosed
	if err := job.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if got := Counters().JobsClosed; got != startClosed+1 {
		t.Errorf("JobsClosed did not increment after first Close: was %d, now %d", startClosed, got)
	}

	// Second Close is a no-op; counter does NOT increment again.
	if err := job.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	if got := Counters().JobsClosed; got != startClosed+1 {
		t.Errorf("JobsClosed incremented on second Close: was %d, now %d", startClosed+1, got)
	}
}

func TestStart_ReturnsNonNilCleanup(t *testing.T) {
	cmd := exec.Command("true")
	if err := overrideForWindowsTrue(cmd); err != nil {
		t.Skip(err)
	}

	cleanup, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup is nil — must be a non-nil closure even on graceful path")
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd.Wait: %v", err)
	}
	cleanup() // must not panic, must be safe after Wait
	cleanup() // double-cleanup must also be safe (Job.Close idempotent)
}

func TestStart_PropagatesCmdStartError(t *testing.T) {
	// Nonexistent binary; cmd.Start should fail and Start should
	// propagate the error while still returning a non-nil no-op cleanup.
	cmd := exec.Command("/no/such/binary/that/exists")
	cleanup, err := Start(cmd)
	if err == nil {
		t.Fatal("expected error for nonexistent binary, got nil")
	}
	if cleanup == nil {
		t.Error("cleanup must be a non-nil closure even on cmd.Start failure")
	}
	cleanup() // safe to call
}

func TestNil_Job_MethodsAreSafe(t *testing.T) {
	// Defensive: nil Job should not panic on any method.
	var j *Job
	if err := j.Close(); err != nil {
		t.Errorf("nil.Close returned error: %v", err)
	}
	j.PrepareCmd(exec.Command("echo"))
	if err := j.Attach(nil); err != nil {
		t.Errorf("nil.Attach returned error: %v", err)
	}
}
