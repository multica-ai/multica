package metrics

import "github.com/prometheus/client_golang/prometheus"

// GitHubPRMetrics exposes the failure modes that otherwise leave PR links or
// API snapshots silently absent. Counters intentionally carry no workspace,
// installation, repository, or PR labels; those identifiers are unbounded and
// belong in the adjacent structured logs.
type GitHubPRMetrics struct {
	linkWriteFailures        prometheus.Counter
	snapshotDisabledTriggers prometheus.Counter
	snapshotFetchFailures    prometheus.Counter
	snapshotWriteFailures    prometheus.Counter
	snapshotQueueDrops       prometheus.Counter
}

func NewGitHubPRMetrics() *GitHubPRMetrics {
	counter := func(name, help string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "github_pr",
			Name:      name,
			Help:      help,
		})
	}
	return &GitHubPRMetrics{
		linkWriteFailures: counter("link_write_failures_total",
			"GitHub pull request issue-link upserts that failed."),
		snapshotDisabledTriggers: counter("snapshot_disabled_triggers_total",
			"GitHub pull request snapshot refresh triggers ignored because GitHub App API credentials are unavailable."),
		snapshotFetchFailures: counter("snapshot_fetch_failures_total",
			"GitHub pull request snapshot API fetches that failed, excluding rate limits."),
		snapshotWriteFailures: counter("snapshot_write_failures_total",
			"GitHub pull request snapshot database reads or writes that failed."),
		snapshotQueueDrops: counter("snapshot_queue_drops_total",
			"GitHub pull request snapshot refreshes dropped because the in-memory queue was full."),
	}
}

func (m *GitHubPRMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.linkWriteFailures,
		m.snapshotDisabledTriggers,
		m.snapshotFetchFailures,
		m.snapshotWriteFailures,
		m.snapshotQueueDrops,
	}
}

func (m *GitHubPRMetrics) RecordLinkWriteFailure()        { m.linkWriteFailures.Inc() }
func (m *GitHubPRMetrics) RecordSnapshotDisabledTrigger() { m.snapshotDisabledTriggers.Inc() }
func (m *GitHubPRMetrics) RecordSnapshotFetchFailure()    { m.snapshotFetchFailures.Inc() }
func (m *GitHubPRMetrics) RecordSnapshotWriteFailure()    { m.snapshotWriteFailures.Inc() }
func (m *GitHubPRMetrics) RecordSnapshotQueueDrop()       { m.snapshotQueueDrops.Inc() }
