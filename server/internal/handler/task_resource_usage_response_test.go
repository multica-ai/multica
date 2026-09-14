package handler

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTaskToResponseIncludesStructuredResourceUsage(t *testing.T) {
	response := taskToResponse(db.AgentTaskQueue{ResourceUsage: []byte(`{
		"isolation_mode":"systemd_cgroup_v2",
		"kind":"memory",
		"memory_oom":true,
		"memory_peak_bytes":50331648,
		"swap_peak_bytes":0,
		"psi_some_avg10_peak":7.5,
		"victim_cgroup":"/multica-task.slice/task.slice",
		"last_command":"codex app-server"
	}`)}, "")

	if response.ResourceUsage == nil {
		t.Fatal("resource_usage was omitted")
	}
	if !response.ResourceUsage.MemoryOOM || response.ResourceUsage.MemoryPeakBytes != 48<<20 {
		t.Fatalf("resource_usage = %#v", response.ResourceUsage)
	}
	if response.ResourceUsage.VictimCgroup != "/multica-task.slice/task.slice" {
		t.Fatalf("victim_cgroup = %q", response.ResourceUsage.VictimCgroup)
	}
}

func TestTaskToResponseOmitsMalformedResourceUsage(t *testing.T) {
	response := taskToResponse(db.AgentTaskQueue{ResourceUsage: []byte(`{"memory_peak_bytes":`)}, "")
	if response.ResourceUsage != nil {
		t.Fatalf("resource_usage = %#v, want nil", response.ResourceUsage)
	}
}
