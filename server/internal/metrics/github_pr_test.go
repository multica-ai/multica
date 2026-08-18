package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestEveryGitHubPRFailureCounterActuallyCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewGitHubPRMetrics()
	for _, c := range m.Collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	m.RecordLinkWriteFailure()
	m.RecordSnapshotDisabledTrigger()
	m.RecordSnapshotFetchFailure()
	m.RecordSnapshotWriteFailure()
	m.RecordSnapshotQueueDrop()

	seen := gatherWecomValues(t, reg)
	for _, want := range []string{
		"multica_github_pr_link_write_failures_total",
		"multica_github_pr_snapshot_disabled_triggers_total",
		"multica_github_pr_snapshot_fetch_failures_total",
		"multica_github_pr_snapshot_write_failures_total",
		"multica_github_pr_snapshot_queue_drops_total",
	} {
		if seen[want] != 1 {
			t.Errorf("%s = %v, want 1", want, seen[want])
		}
	}
}

func TestTheRegistryExposesGitHubPRCounters(t *testing.T) {
	r := NewRegistry(RegistryOptions{})
	if r.GitHubPR == nil {
		t.Fatal("Registry.GitHubPR is nil; nothing can report through it")
	}
	r.GitHubPR.RecordSnapshotDisabledTrigger()

	families, err := r.Gatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() == "multica_github_pr_snapshot_disabled_triggers_total" {
			return
		}
	}
	t.Fatal("multica_github_pr_snapshot_disabled_triggers_total is not exposed")
}
