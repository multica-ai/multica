package daemon

import (
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/taskresource"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestApplyTaskResourceUsageTurnsOOMIntoStructuredFailure(t *testing.T) {
	result := TaskResult{Status: "blocked", Comment: "signal: killed", FailureReason: "agent_error.process_failure"}
	usage := taskresource.Usage{
		IsolationMode:   taskresource.IsolationSystemd,
		Kind:            "memory",
		MemoryOOM:       true,
		MemoryPeakBytes: 12 << 30,
		SwapPeakBytes:   2 << 30,
		VictimCgroup:    "/user.slice/multica-task-aabb.slice",
		LastCommand:     "codex app-server",
	}

	err := applyTaskResourceUsage(&result, errors.New("signal: killed"), usage)
	if err != nil {
		t.Fatalf("error = %v, want nil after OOM classification", err)
	}
	if result.FailureReason != taskfailure.ReasonResourceExhaustedMemory.String() {
		t.Fatalf("failure reason = %q", result.FailureReason)
	}
	if result.ResourceUsage == nil || result.ResourceUsage.VictimCgroup != usage.VictimCgroup {
		t.Fatalf("resource usage = %#v", result.ResourceUsage)
	}
	if result.Comment == "signal: killed" {
		t.Fatal("generic process error must be replaced with an actionable resource failure")
	}
}

func TestApplyTaskResourceUsageKeepsManualKillDistinct(t *testing.T) {
	result := TaskResult{Status: "blocked", Comment: "signal: killed", FailureReason: "agent_error.process_failure"}
	usage := taskresource.Usage{IsolationMode: taskresource.IsolationSystemd, MemoryPeakBytes: 64 << 20}
	errIn := errors.New("signal: killed")
	if err := applyTaskResourceUsage(&result, errIn, usage); !errors.Is(err, errIn) {
		t.Fatalf("error = %v, want original manual-kill error", err)
	}
	if result.FailureReason != "agent_error.process_failure" {
		t.Fatalf("manual kill reclassified as %q", result.FailureReason)
	}
}
