package agent

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// streamEventCadence accumulates per-type counts and inter-event gaps for a
// backend's raw stream. It answers two questions the aggregate event_count
// cannot (MUL-5042):
//
//   - What a run's traffic is actually made of. A run hung on a dead provider
//     stream can still emit a steady cadence of content-free events, which is
//     indistinguishable from a healthy run when only the total is logged. Two
//     hangs were investigated with nothing but a total to go on, and the
//     breakdown had to be inferred from arithmetic.
//   - How long a healthy run legitimately goes between events. The idle
//     watchdog threshold has to sit above the real inter-event gap of working
//     runs, and that distribution has never been measured — one run showed a
//     ~7 minute pure-reasoning gap, so lowering the window blind risks killing
//     real work.
//
// It records event types and timings only, never payloads, so it is safe to
// log alongside the other protocol metadata.
type streamEventCadence struct {
	typeCounts map[string]int

	last         time.Time
	lastProgress time.Time

	maxGap         time.Duration
	maxGapEndedBy  string
	maxProgressGap time.Duration
}

func newStreamEventCadence(start time.Time) *streamEventCadence {
	return &streamEventCadence{
		typeCounts:   make(map[string]int),
		last:         start,
		lastProgress: start,
	}
}

// observe records one raw stream event. progress marks events that carry
// forward work (assistant content, tool traffic, terminal result) as opposed
// to liveness-only chatter, and must match the daemon's own activity predicate
// so the logged gaps describe what the watchdog actually measures.
func (c *streamEventCadence) observe(now time.Time, eventType string, progress bool) {
	if c == nil {
		return
	}
	if eventType == "" {
		eventType = "unknown"
	}
	c.typeCounts[eventType]++

	if gap := now.Sub(c.last); gap > c.maxGap {
		c.maxGap = gap
		c.maxGapEndedBy = eventType
	}
	c.last = now

	if progress {
		if gap := now.Sub(c.lastProgress); gap > c.maxProgressGap {
			c.maxProgressGap = gap
		}
		c.lastProgress = now
	}
}

// streamCadenceSnapshot is the finalized view of a stream's cadence, safe to
// log directly.
type streamCadenceSnapshot struct {
	typeCounts     string
	maxGap         time.Duration
	maxGapEndedBy  string
	maxProgressGap time.Duration
}

// streamEndedLabel marks a gap that was closed by the stream ending rather
// than by an event arriving.
const streamEndedLabel = "(stream-end)"

// snapshot finalizes the cadence at end, the moment the stream closed.
//
// The trailing silence — last event to end — is folded into both gaps, and
// that is the point of the whole type. A run that dies mid-turn keeps its
// entire stall in that trailing window: the two investigated hangs sat silent
// for 17 and 22 minutes after their last event, so a tracker that only
// measured gaps *between* events would have reported a healthy-looking
// cadence and hidden the stall completely. The same applies to a run that
// never makes progress at all, where the progress gap would otherwise be
// reported as zero (MUL-5042).
func (c *streamEventCadence) snapshot(end time.Time) streamCadenceSnapshot {
	if c == nil {
		return streamCadenceSnapshot{}
	}
	s := streamCadenceSnapshot{
		typeCounts:     c.typeSummary(),
		maxGap:         c.maxGap,
		maxGapEndedBy:  c.maxGapEndedBy,
		maxProgressGap: c.maxProgressGap,
	}
	if trailing := end.Sub(c.last); trailing > s.maxGap {
		s.maxGap = trailing
		s.maxGapEndedBy = streamEndedLabel
	}
	if trailing := end.Sub(c.lastProgress); trailing > s.maxProgressGap {
		s.maxProgressGap = trailing
	}
	return s
}

// typeSummary renders the per-type counts as a stable compact string ordered
// by descending count then type name, e.g. "system=613 assistant=22 user=9".
// Ordering is deterministic so the field is greppable across runs.
func (c *streamEventCadence) typeSummary() string {
	if c == nil || len(c.typeCounts) == 0 {
		return ""
	}
	types := make([]string, 0, len(c.typeCounts))
	for t := range c.typeCounts {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool {
		if c.typeCounts[types[i]] != c.typeCounts[types[j]] {
			return c.typeCounts[types[i]] > c.typeCounts[types[j]]
		}
		return types[i] < types[j]
	})

	var b strings.Builder
	for i, t := range types {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(c.typeCounts[t]))
	}
	return b.String()
}
