package detsteps

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

// TestMain lets the test binary act as the step-sandbox child: when RunSubprocess
// re-execs os.Args[0] with the sentinel set, MaybeRunStepChild handles the run
// and exits before the test runner starts. Otherwise tests run normally.
func TestMain(m *testing.M) {
	MaybeRunStepChild()
	os.Exit(m.Run())
}

func TestRunSubprocessOK(t *testing.T) {
	src := `package step
import "strings"
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	return map[string]any{"status": "ok", "summary": "hi " + strings.ToUpper(name)}
}`
	res := RunSubprocess(context.Background(), os.Args[0], src, map[string]any{"name": "world"}, 5*time.Second)
	if res.Status != dettools.StatusOK || res.Summary != "hi WORLD" {
		t.Fatalf("result = %+v, want ok/hi WORLD", res)
	}
}

func TestRunSubprocessPropagatesErrorEnvelope(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any {
	return map[string]any{"status": "error", "error_code": "POLICY_FAILURE", "summary": "nope"}
}`
	res := RunSubprocess(context.Background(), os.Args[0], src, nil, 5*time.Second)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodePolicyFailure {
		t.Fatalf("result = %+v, want error/POLICY_FAILURE", res)
	}
}

// An infinite loop must be preempted: the call returns a TIMEOUT result well
// within the parent kill grace, and the child process is gone (no goroutine
// accumulates in this test process, unlike the in-process path).
func TestRunSubprocessHardKillsInfiniteLoop(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any { for { _ = 1 } }`

	start := time.Now()
	res := RunSubprocess(context.Background(), os.Args[0], src, nil, 300*time.Millisecond)
	elapsed := time.Since(start)

	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeTimeout {
		t.Fatalf("result = %+v, want error/TIMEOUT for an infinite loop", res)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("preemption took %s; expected prompt termination", elapsed)
	}
}

func TestRunSubprocessParentContextCancel(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any { for { _ = 1 } }`
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	res := RunSubprocess(ctx, os.Args[0], src, nil, 10*time.Second)
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeTimeout {
		t.Fatalf("result = %+v, want error/TIMEOUT for a cancelled context", res)
	}
}

// With no binary to re-exec, RunSubprocess degrades to the in-process path
// rather than failing.
func TestRunSubprocessFallsBackWhenNoBinary(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any { return map[string]any{"status":"ok","summary":"inproc"} }`
	res := RunSubprocess(context.Background(), "", src, nil, 2*time.Second)
	if res.Status != dettools.StatusOK || res.Summary != "inproc" {
		t.Fatalf("result = %+v, want ok/inproc via in-process fallback", res)
	}
}
