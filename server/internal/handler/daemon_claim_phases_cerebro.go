package handler

// FIR-3781 claim-build phase timing.
//
// claim_endpoint slow reports build_ms as one number, and the build spans ~840
// lines: response shell, agent + skills, chat history, effective tools, task
// mandate, memory auto-recall, and the JSON write. When #2687 took build_ms
// from a stable ~3s to 50-65s — past the daemon's 30s timeout, so no task was
// delivered for eleven hours — that single number could not say which of those
// phases moved, and the phase was never identified.
//
// Marks are wall-clock only: no queries, no allocation beyond a small slice.
// The log line is emitted by the same threshold that already governs
// claim_endpoint slow, so a healthy claim stays silent.

import (
	"log/slog"
	"time"
)

// claimPhases records elapsed time at named points in the claim response build.
type claimPhases struct {
	start time.Time
	last  time.Time
	marks []any
}

func newClaimPhases() *claimPhases {
	now := time.Now()
	return &claimPhases{start: now, last: now, marks: make([]any, 0, 16)}
}

// mark records the milliseconds spent since the previous mark. Deltas rather
// than cumulative totals: the point is to find the one phase that owns the
// time, and a delta says that directly.
func (p *claimPhases) mark(name string) {
	if p == nil {
		return
	}
	now := time.Now()
	p.marks = append(p.marks, name+"_ms", now.Sub(p.last).Milliseconds())
	p.last = now
}

// log emits the breakdown when the build exceeded threshold.
func (p *claimPhases) log(runtimeID string, threshold time.Duration) {
	if p == nil || time.Since(p.start) < threshold {
		return
	}
	attrs := make([]any, 0, len(p.marks)+4)
	attrs = append(attrs, "runtime_id", runtimeID, "total_ms", time.Since(p.start).Milliseconds())
	attrs = append(attrs, p.marks...)
	slog.Info("claim build phases", attrs...)
}
