package providerfailover

import "time"

// Liveness watchdog (td-836aa9). The subsystem's original trigger was purely
// error-driven: it fired only when a run returned a usage/rate-limit error. A
// SILENT hang — the exact incident that motivated this work, where the GPT/Codex
// run went dark for ~60 minutes and the Claude smokes stalled at ~180s — returns
// NO error, so it never entered the failover pipeline at all. These helpers are
// the pure core of the wall-clock watchdog: given how long a run has been going
// and how stale its runtime's heartbeat is, decide whether it is a wedged
// silent-hang that should synthesize a terminal, failover-eligible failure
// (ReasonProviderLivenessTimeout).
//
// Pure and clock-free by design: the caller supplies the two durations (run age,
// heartbeat age) it measured against a single now(), so the decision is
// deterministic and unit-testable.

const (
	// codexLivenessDeadline bounds a GPT/Codex run's silence. The motivating
	// incident hung ~60 minutes before anyone noticed; the deadline is set at
	// that observed ceiling so a genuinely long-but-live run (which keeps its
	// heartbeat fresh and is excluded by the liveness signal regardless) is not
	// cut short, while a truly wedged run is caught at the boundary.
	codexLivenessDeadline = 60 * time.Minute

	// claudeLivenessDeadline bounds a Claude run's silence. Claude runs in this
	// fleet are short and interactive; the earlier stalls surfaced at ~180s, so
	// a Claude run that has been "running" far past that with a dead heartbeat
	// is wedged, not working.
	claudeLivenessDeadline = 180 * time.Second

	// defaultLivenessDeadline is the fail-safe ceiling for any provider without
	// a specific deadline. Deliberately generous so the watchdog never
	// prematurely reaps an unmodeled provider; the heartbeat-staleness signal
	// (see IsSilentHang) is what actually distinguishes wedged from busy.
	defaultLivenessDeadline = 60 * time.Minute
)

// livenessDeadlines is the per-provider wall-clock silence budget. A provider
// absent here uses defaultLivenessDeadline.
var livenessDeadlines = map[string]time.Duration{
	"codex":  codexLivenessDeadline,
	"claude": claudeLivenessDeadline,
}

// LivenessDeadline returns the wall-clock silence budget for a provider — how
// long a run of that provider may be in flight before the watchdog treats
// continued unresponsiveness as a hang. Unknown/empty providers get the
// conservative defaultLivenessDeadline.
func LivenessDeadline(provider string) time.Duration {
	if d, ok := livenessDeadlines[provider]; ok {
		return d
	}
	return defaultLivenessDeadline
}

// IsSilentHang reports whether a still-running task should be treated as a
// wedged silent hang and have a terminal, failover-eligible failure synthesized
// for it. Both conditions must hold:
//
//   - runningFor >= LivenessDeadline(provider): the run has exceeded its
//     provider's wall-clock silence budget, AND
//   - heartbeatAlive is false: the owning runtime is no longer proving liveness
//     (stale daemon heartbeat / expired per-task lease). A run whose runtime is
//     still heartbeating is BUSY, not hung — it is deliberately preserved (this
//     is what lets legitimate multi-hour runs survive, mirroring the server
//     FailStaleTasks backstop), so the watchdog never fights a healthy long run.
//
// Pairing the wall clock with the liveness signal is the whole point: a bare
// wall-clock cap would reap healthy long runs, and a bare liveness signal is
// already the daemon-dead path (FailTasksForOfflineRuntimes). The silent-hang
// case this targets is specifically runtime-online-but-wedged past the deadline.
func IsSilentHang(provider string, runningFor time.Duration, heartbeatAlive bool) bool {
	if heartbeatAlive {
		return false
	}
	return runningFor >= LivenessDeadline(provider)
}
