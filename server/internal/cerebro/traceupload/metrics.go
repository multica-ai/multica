package traceupload

import "sync/atomic"

// Metrics are process-lifetime counters for trace uploads (F2.11). They are
// plain atomics so the health endpoint / logs can read a snapshot cheaply.
type Metrics struct {
	success    atomic.Int64
	failed     atomic.Int64
	deadLetter atomic.Int64
}

func (m *Metrics) incSuccess()    { m.success.Add(1) }
func (m *Metrics) incFailed()     { m.failed.Add(1) }
func (m *Metrics) incDeadLetter() { m.deadLetter.Add(1) }

// Snapshot is a point-in-time view of the counters.
type Snapshot struct {
	Success    int64 `json:"success"`
	Failed     int64 `json:"failed"`
	DeadLetter int64 `json:"dead_letter"`
}

// Snapshot returns the current counter values.
func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		Success:    m.success.Load(),
		Failed:     m.failed.Load(),
		DeadLetter: m.deadLetter.Load(),
	}
}
