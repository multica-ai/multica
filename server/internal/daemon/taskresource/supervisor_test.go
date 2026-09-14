//go:build linux

package taskresource

import (
	"reflect"
	"testing"
)

func TestScopePrefixKeepsTaskInDedicatedSlice(t *testing.T) {
	s := &Supervisor{mode: IsolationSystemd, sliceUnit: "multica-task-aabbcc.slice"}
	want := []string{
		"systemd-run", "--user", "--scope", "--quiet",
		"--slice=multica-task-aabbcc.slice",
		"--property=OOMPolicy=kill",
		"--property=KillMode=control-group",
		"--",
	}
	if got := s.CommandPrefix(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CommandPrefix() = %#v, want %#v", got, want)
	}
}

func TestParseResourceSnapshot(t *testing.T) {
	snapshot := parseResourceSnapshot(
		"/user.slice/user-1000.slice/user@1000.service/multica.slice/multica-task-aabbcc.slice",
		[]byte("MemoryPeak=12582912\nMemorySwapPeak=4096\n"),
		[]byte("low 0\nhigh 2\nmax 7\noom 3\noom_kill 1\noom_group_kill 1\n"),
		[]byte("some avg10=12.50 avg60=3.25 avg300=1.00 total=99\nfull avg10=8.75 avg60=2.00 avg300=0.50 total=42\n"),
	)

	if snapshot.MemoryPeakBytes != 12<<20 || snapshot.SwapPeakBytes != 4096 {
		t.Fatalf("memory peaks = %d/%d", snapshot.MemoryPeakBytes, snapshot.SwapPeakBytes)
	}
	if snapshot.OOMKills != 1 || snapshot.OOMGroupKills != 1 {
		t.Fatalf("oom counters = %d/%d", snapshot.OOMKills, snapshot.OOMGroupKills)
	}
	if snapshot.PSISomeAvg10 != 12.5 || snapshot.PSIFullAvg10 != 8.75 {
		t.Fatalf("PSI avg10 = %f/%f", snapshot.PSISomeAvg10, snapshot.PSIFullAvg10)
	}
	if snapshot.VictimCgroup == "" {
		t.Fatal("victim cgroup must be retained")
	}
}

func TestResourceUsageRecognizesMemoryOOM(t *testing.T) {
	s := &Supervisor{
		mode:        IsolationSystemd,
		sliceUnit:   "multica-task-aabbcc.slice",
		memoryHigh:  8 << 20,
		memoryMax:   12 << 20,
		swapMax:     0,
		lastCommand: "codex app-server",
	}
	s.samples = resourceSnapshot{
		VictimCgroup:    "/user.slice/multica-task-aabbcc.slice",
		MemoryPeakBytes: 12 << 20,
		OOMKills:        1,
		PSISomeAvg10:    22.4,
		PSIFullAvg10:    10.2,
	}

	usage := s.resourceUsage()
	if !usage.MemoryOOM || usage.Kind != "memory" {
		t.Fatalf("usage = %#v, want memory OOM", usage)
	}
	if usage.LastCommand != "codex app-server" || usage.MemoryMaxBytes != 12<<20 {
		t.Fatalf("usage command/limit = %#v", usage)
	}
}
