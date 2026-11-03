package timing

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Caller is who asked for a turn, as plain values so the service needs no
// transport knowledge. Zero values read as unknown.
type Caller struct {
	Origin     string
	ClientKind string
	Connection string
}

// TraceOptions identifies one turn's records.
type TraceOptions struct {
	// Now defaults to time.Now.
	Now func() time.Time
	// Start is when the request was received; defaults to now.
	Start          time.Time
	ConversationID string
	RequestID      string
	TurnID         string
	Provider       string
	TurnKind       string
	// TurnIndex is nil when unknown.
	TurnIndex *int64
	Attempt   int64
	Caller    Caller
	// PrepareID ties a record to the prepare task it waited for or ran.
	PrepareID string
}

// Trace writes one turn's marks. A nil Trace does nothing, so runners and
// tests that have no timing need no checks.
type Trace struct {
	log   *Log
	now   func() time.Time
	start time.Time

	mu      sync.Mutex
	base    Record
	seen    map[string]bool
	summary outputSummary
}

type outputSummary struct {
	events, bytes int64
	last          time.Time
	maxGap        time.Duration
}

// NewTrace returns a trace writing to log, or nil when log is nil.
func NewTrace(log *Log, opts TraceOptions) *Trace {
	if log == nil {
		return nil
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Start.IsZero() {
		opts.Start = opts.Now()
	}
	base := Record{
		ConversationID: validID(opts.ConversationID), RequestID: validID(opts.RequestID), TurnID: validID(opts.TurnID),
		Provider: opts.Provider, TurnKind: opts.TurnKind, Origin: closedOr(origins, opts.Caller.Origin, OriginUnknown), ClientKind: closedOr(clientKinds, opts.Caller.ClientKind, ClientUnknown),
		Connection: closedOr(connections, opts.Caller.Connection, ConnectionUnknown),
		Background: opts.TurnKind == TurnBackground || opts.TurnKind == TurnNative, PrepareID: validID(opts.PrepareID),
	}
	base.TurnIndex = opts.TurnIndex
	if opts.Attempt > 0 {
		base.Attempt = &opts.Attempt
	}
	return &Trace{log: log, now: opts.Now, start: opts.Start, base: base, seen: map[string]bool{}}
}

// ValidID reports whether id can go on a record; anything else would reject it.
func ValidID(id string) bool { return identifier.MatchString(id) }

func validID(id string) string {
	if identifier.MatchString(id) {
		return id
	}
	return ""
}

// closedOr keeps an unrecognized caller value out of a record: the caller is
// then unknown, which is what the dimension means, rather than absent.
func closedOr(set map[string]bool, value, fallback string) string {
	if set[value] {
		return value
	}
	return fallback
}

// Now reads the trace's clock; a nil trace reads the wall clock.
func (t *Trace) Now() time.Time {
	if t == nil {
		return time.Now()
	}
	return t.now()
}

// Started is when the trace's request was received.
func (t *Trace) Started() time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.start
}

// SetPrepareKind records how later marks of this turn got ready.
func (t *Trace) SetPrepareKind(kind string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.base.PrepareKind = kind
	t.mu.Unlock()
}

// SetPrepare records how later marks of this turn got ready and which prepare
// task they relate to; an empty id keeps the current one.
func (t *Trace) SetPrepare(kind, id string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.base.PrepareKind = kind
	if id != "" {
		t.base.PrepareID = validID(id)
	}
	t.mu.Unlock()
}

func (t *Trace) write(at time.Time, stage, event string, fill func(*Record)) {
	t.mu.Lock()
	r := t.base
	t.mu.Unlock()
	r.Stage, r.Event, r.At = stage, event, formatAt(at)
	if elapsed := at.Sub(t.start).Milliseconds(); elapsed > 0 {
		r.ElapsedMs = elapsed
	}
	if fill != nil {
		fill(&r)
	}
	t.log.Write(r)
}

// Mark records that stage was reached.
func (t *Trace) Mark(stage string) { t.MarkReason(stage, "") }

// MarkReason records that stage was reached, with a closed-enum reason.
func (t *Trace) MarkReason(stage, reason string) {
	if t == nil {
		return
	}
	t.write(t.now(), stage, EventMark, func(r *Record) { r.Reason = reason })
}

// MarkAt records that stage was reached earlier, at at.
func (t *Trace) MarkAt(stage string, at time.Time) {
	if t == nil {
		return
	}
	t.write(at, stage, EventMark, nil)
}

// MarkDuration records a stage with a closed-enum reason and a duration that
// is not the time since the trace began, such as how long a process sat idle.
func (t *Trace) MarkDuration(stage, reason string, d time.Duration) {
	if t == nil {
		return
	}
	t.write(t.now(), stage, EventMark, func(r *Record) {
		r.Reason = reason
		fillDuration(r, d)
	})
}

// MarkCount records a stage with a closed-enum reason and a count.
func (t *Trace) MarkCount(stage, reason string, count int64) {
	if t == nil {
		return
	}
	t.write(t.now(), stage, EventMark, func(r *Record) { r.Reason, r.Count = reason, &count })
}

// MarkBytes records a stage with a closed-enum reason and a size.
func (t *Trace) MarkBytes(stage, reason string, bytes int64) {
	if t == nil {
		return
	}
	t.write(t.now(), stage, EventMark, func(r *Record) { r.Reason, r.Bytes = reason, &bytes })
}

// ObserveOutput adds one committed event to the turn's summary. Tokens arrive far
// too often for a record each, so a turn writes OutputSummary once instead.
func (t *Trace) ObserveOutput(bytes int64) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.summary.events++
	t.summary.bytes += bytes
	if !t.summary.last.IsZero() {
		t.summary.maxGap = max(t.summary.maxGap, now.Sub(t.summary.last))
	}
	t.summary.last = now
}

// OutputSummary records the turn's event count, bytes and largest gap, once.
func (t *Trace) OutputSummary() {
	if t == nil || !t.claim(StageTurnOutputSummary) {
		return
	}
	t.mu.Lock()
	summary := t.summary
	t.mu.Unlock()
	t.write(t.now(), StageTurnOutputSummary, EventMark, func(r *Record) {
		gap := summary.maxGap.Milliseconds()
		r.Count, r.Bytes, r.MaxGapMs = &summary.events, &summary.bytes, &gap
	})
}

// MarkFailed records a stage that failed at once.
func (t *Trace) MarkFailed(stage, reason string) {
	if t == nil {
		return
	}
	t.write(t.now(), stage, EventMark, func(r *Record) { r.Reason, r.Outcome, r.Failure = reason, "failed", "other" })
}

// Once records stage the first time it is reached and reports whether it did.
func (t *Trace) Once(stage string) bool {
	if t == nil || !t.claim(stage) {
		return false
	}
	t.Mark(stage)
	return true
}

// OnceFor is Once per key, for marks recorded per subscriber. It carries the
// subscriber's identity: a delivery belongs to whoever it reached, not to the
// caller who started the turn.
func (t *Trace) OnceFor(stage, key, reason string, caller Caller) bool {
	if t == nil || !t.claim(stage+"\x00"+key) {
		return false
	}
	t.write(t.now(), stage, EventMark, func(r *Record) {
		r.Reason = reason
		r.Origin = closedOr(origins, caller.Origin, OriginUnknown)
		r.ClientKind = closedOr(clientKinds, caller.ClientKind, ClientUnknown)
		r.Connection = closedOr(connections, caller.Connection, ConnectionUnknown)
	})
	return true
}

// Reached reports whether Once or OnceFor already recorded this mark, so a
// caller can skip the work of deciding whether to record it again.
func (t *Trace) Reached(stage, key string) bool {
	if t == nil {
		return true
	}
	if key != "" {
		stage += "\x00" + key
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seen[stage]
}

func (t *Trace) claim(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen[key] {
		return false
	}
	t.seen[key] = true
	return true
}

// Begin records the start of stage and returns the function recording its
// end; a slow stage that never ends still shows where a turn stopped.
func (t *Trace) Begin(stage string) func(error) { return t.BeginReason(stage, "") }

// BeginReason is Begin with a closed-enum reason on both records.
func (t *Trace) BeginReason(stage, reason string) func(error) {
	if t == nil {
		return func(error) {}
	}
	began := t.now()
	t.write(began, stage, EventStart, func(r *Record) { r.Reason = reason })
	return func(err error) {
		end := t.now()
		t.write(end, stage, EventEnd, func(r *Record) {
			r.Reason = reason
			fillDuration(r, end.Sub(began))
			fillOutcome(r, err)
		})
	}
}

// Completed records a stage measured elsewhere that ended at end.
func (t *Trace) Completed(stage string, end time.Time, d time.Duration, err error) {
	if t == nil {
		return
	}
	t.write(end, stage, EventEnd, func(r *Record) {
		fillDuration(r, d)
		fillOutcome(r, err)
	})
}

// Finish records the turn's end. message is classified and never stored.
func (t *Trace) Finish(status, kind, message string) {
	if t == nil || !t.claim(StageTurnFinished) {
		return
	}
	t.write(t.now(), StageTurnFinished, EventEnd, func(r *Record) {
		r.Outcome, r.Reason = status, kind
		if status != "completed" {
			r.Failure = Classify(message)
			if status == "cancelled" {
				r.Failure = "cancelled"
			}
		}
	})
}

func fillDuration(r *Record, d time.Duration) {
	ms := max(d.Milliseconds(), 0)
	r.DurationMs = &ms
}

func fillOutcome(r *Record, err error) {
	switch {
	case err == nil:
		r.Outcome = "completed"
	case errors.Is(err, context.Canceled):
		r.Outcome, r.Failure = "cancelled", "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		r.Outcome, r.Failure = "failed", "transport"
	default:
		r.Outcome, r.Failure = "failed", Classify(err.Error())
	}
}
