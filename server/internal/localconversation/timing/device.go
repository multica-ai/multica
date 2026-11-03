package timing

import (
	"net/http"
	"time"
)

// HeaderRequestID ties a proxied request to the turn it starts, so its marks join
// that turn's waterfall. Only a client's own commands carry it.
const HeaderRequestID = "X-Multica-Request-ID"

// ReportsPath is the route a phone reports its marks on, under /local/v2.
const ReportsPath = "/timing-reports"

// RequestIDOf is the request's correlation id, or "" unless it could be one.
func RequestIDOf(h http.Header) string {
	if id := h.Get(HeaderRequestID); ValidID(id) {
		return id
	}
	return ""
}

// FormatAt is t in a record's time format.
func FormatAt(t time.Time) string { return formatAt(t) }

// Sink takes records: a Log for a device with a file, a Ring for a phone.
type Sink interface{ Write(Record) }

// Marks writes records that belong to a device rather than a turn. A nil Marks
// does nothing, so transport code needs no checks.
type Marks struct {
	sink   Sink
	source string
	now    func() time.Time
}

// NewMarks writes to sink as source; a nil sink returns nil.
func NewMarks(sink Sink, source string) *Marks {
	if sink == nil {
		return nil
	}
	return &Marks{sink: sink, source: source, now: time.Now}
}

// Now reads the clock the marks use; nil marks read the wall clock.
func (m *Marks) Now() time.Time {
	if m == nil {
		return time.Now()
	}
	return m.now()
}

// Write records r at now unless it names its own time, as a mark by default.
func (m *Marks) Write(r Record) {
	if m == nil {
		return
	}
	r.Source = m.source
	if r.At == "" {
		r.At = formatAt(m.now())
	}
	if r.Event == "" {
		r.Event = EventMark
	}
	m.sink.Write(r)
}

// Since fills r's duration from began and its outcome from err, as an end record.
func (m *Marks) Since(r Record, began time.Time, err error) {
	if m == nil {
		return
	}
	r.Event = EventEnd
	fillDuration(&r, m.now().Sub(began))
	fillOutcome(&r, err)
	m.Write(r)
}

// Ms is a record's duration field for an interval measured elsewhere.
func Ms(d time.Duration) *int64 {
	ms := max(d.Milliseconds(), 0)
	return &ms
}

// N is a record's count or size field.
func N(n int64) *int64 { return &n }
