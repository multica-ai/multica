package daemon

// CEREBRO-PATCH(daemon-trace-upload-wiring-guard): FIR-2757 regression guard.

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonRunStartsTraceUpload(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	if !strings.Contains(string(src), "d.startTraceUpload(ctx)") {
		t.Fatal("Daemon.Run must start trace upload; an upstream sync likely removed the Cerebro wiring")
	}
}

func TestDaemonHandleTaskEnqueuesTraceUpload(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	handleTaskStart := strings.Index(string(src), "func (d *Daemon) handleTask(")
	handleTaskEnd := strings.Index(string(src), "func (d *Daemon) acquireLocalDirectoryLockIfNeeded(")
	if handleTaskStart < 0 || handleTaskEnd <= handleTaskStart {
		t.Fatal("locate Daemon.handleTask source")
	}
	handleTask := string(src[handleTaskStart:handleTaskEnd])

	runnerReturn := strings.Index(handleTask, "result, err := d.runner.run(")
	enqueue := strings.Index(handleTask, "d.enqueueTraceUpload(task, provider, result)")
	cancellationCheck := strings.Index(handleTask, "// Check if we were cancelled by the polling goroutine.")
	if runnerReturn < 0 || cancellationCheck < 0 {
		t.Fatal("locate task runner and cancellation boundary in Daemon.handleTask")
	}
	if enqueue < 0 {
		t.Fatal("Daemon.handleTask must enqueue trace upload; an upstream sync likely removed the Cerebro wiring")
	}
	if strings.Count(handleTask, "d.enqueueTraceUpload(task, provider, result)") != 1 {
		t.Fatal("Daemon.handleTask must enqueue trace upload exactly once")
	}
	if enqueue < runnerReturn || enqueue > cancellationCheck {
		t.Fatal("Daemon.handleTask must enqueue trace upload after the runner returns and before cancellation can discard the result")
	}
}
