package timing

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stagesOf(records []Record) string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.Stage
	}
	return strings.Join(out, ",")
}

func TestRingDrainsWithACursorAndKeepsTheNewest(t *testing.T) {
	t.Parallel()

	r := OpenRing("", 2)
	r.Write(Record{Stage: StageLinkStart, Event: EventMark})
	records, next, dropped := r.After(0, 0)
	if stagesOf(records) != StageLinkStart || next != 1 || dropped != 0 {
		t.Fatalf("first read = %v, %d, %d", stagesOf(records), next, dropped)
	}
	if records[0].Source != SourceMobile {
		t.Fatalf("source = %q", records[0].Source)
	}
	if again, cursor, _ := r.After(next, 0); len(again) != 0 || cursor != 1 {
		t.Fatalf("a drained ring returned %v, %d", stagesOf(again), cursor)
	}
	r.Write(Record{Stage: StageLinkReady, Event: EventMark})
	r.Write(Record{Stage: StageLinkStop, Event: EventMark})
	r.Write(Record{Stage: StagePeerKnown, Event: EventMark})
	records, next, dropped = r.After(1, 0)
	if stagesOf(records) != StageLinkStop+","+StagePeerKnown || next != 4 || dropped != 2 {
		t.Fatalf("after overflow = %v, %d, %d", stagesOf(records), next, dropped)
	}
	if limited, cursor, _ := r.After(0, 1); len(limited) != 1 || cursor != 3 {
		t.Fatalf("limited read = %v, %d", stagesOf(limited), cursor)
	}
	if fresh, cursor, _ := r.After(99, 0); len(fresh) != 2 || cursor != 4 {
		t.Fatalf("a cursor from ahead must read everything: %v, %d", stagesOf(fresh), cursor)
	}
}

func TestRingRefusesFreeTextAndUnknownStages(t *testing.T) {
	t.Parallel()

	r := OpenRing("", 4)
	r.Write(Record{Stage: "nope", Event: EventMark})
	r.Write(Record{Stage: StageRelayRefused, Event: EventMark, Reason: "dial tcp 10.0.0.1 refused", AddrType: "10.0.0.1"})
	records, _, _ := r.After(0, 0)
	if len(records) != 1 || records[0].Reason != "" || records[0].AddrType != "" {
		t.Fatalf("records = %+v", records)
	}
}

func TestRingSurvivesAStoppedLink(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "ring.json")
	r := OpenRing(path, 2)
	r.Write(Record{Stage: StageLinkStart, Event: EventMark})
	r.Write(Record{Stage: StageLinkStop, Event: EventMark})
	_, cursor, _ := r.After(1, 0)
	r.Save()

	resumed := OpenRing(path, 2)
	resumed.Write(Record{Stage: StageLinkStart, Event: EventMark})
	records, next, dropped := resumed.After(cursor, 0)
	if stagesOf(records) != StageLinkStart || next != 3 || dropped != 1 {
		t.Fatalf("resumed = %v, %d, %d", stagesOf(records), next, dropped)
	}
	raw, err := json.Marshal(records)
	if err != nil || strings.Contains(string(raw), "receivedAt") {
		t.Fatalf("a phone's own record has no executor stamp: %s, %v", raw, err)
	}
}

func TestRingForgetsWhatTheAppAcknowledgedAcrossAColdStart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ring.json")
	r := OpenRing(path, 3)
	for _, stage := range []string{StageLinkStart, StageLinkReady, StageLinkStop, StagePeerKnown} {
		r.Write(Record{Stage: stage, Event: EventMark})
	}
	if _, next, dropped := r.After(0, 0); next != 4 || dropped != 1 {
		t.Fatalf("next = %d, dropped = %d", next, dropped)
	}
	r.Acknowledge(3, 1)

	// A new process's app starts its cursor at 0, without a Close having saved.
	reopened := OpenRing(path, 3)
	records, next, dropped := reopened.After(0, 0)
	if stagesOf(records) != StagePeerKnown || next != 4 || dropped != 0 {
		t.Fatalf("after a cold start = %v, %d, %d", stagesOf(records), next, dropped)
	}
	reopened.Write(Record{Stage: StageLinkReady, Event: EventMark})
	if records, next, _ := reopened.After(4, 0); stagesOf(records) != StageLinkReady || next != 5 {
		t.Fatalf("numbering moved: %v, %d", stagesOf(records), next)
	}

	// An acknowledgement from a lost file's numbering forgets nothing.
	reopened.Acknowledge(99, 0)
	if records, _, _ := reopened.After(0, 0); len(records) != 2 {
		t.Fatalf("records = %v", stagesOf(records))
	}
	// Records acknowledged earlier cannot come back, and dropped cannot go negative.
	reopened.Acknowledge(1, 5)
	if records, _, dropped := reopened.After(0, 0); len(records) != 2 || dropped != 0 {
		t.Fatalf("records = %v, dropped = %d", stagesOf(records), dropped)
	}
}

func TestRingStartsEmptyFromAnUnreadableFile(t *testing.T) {
	t.Parallel()

	if records, next, _ := OpenRing(filepath.Join(t.TempDir(), "missing"), 2).After(0, 0); len(records) != 0 || next != 0 {
		t.Fatalf("records = %v, next = %d", records, next)
	}
}

func TestMarksStampAndAreSafeWithoutASink(t *testing.T) {
	t.Parallel()

	var none *Marks
	none.Write(Record{Stage: StageLinkStart})
	none.Since(Record{Stage: StageLinkStart}, time.Now(), nil)
	if NewMarks(nil, SourceDaemon) != nil {
		t.Fatal("no sink needs no marks")
	}
	ring := OpenRing("", 4)
	m := NewMarks(ring, SourceDaemon)
	m.Since(Record{Stage: StagePeerConnect}, m.Now().Add(-time.Second), nil)
	records, _, _ := ring.After(0, 0)
	if len(records) != 1 || records[0].Source != SourceDaemon || records[0].Event != EventEnd || records[0].Outcome != "completed" || *records[0].DurationMs < 1000 {
		t.Fatalf("record = %+v", records)
	}
}

func TestTraceSummarizesOutputOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "t.jsonl")
	log := Open(path, Options{})
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	trace := NewTrace(log, TraceOptions{Now: func() time.Time { return now }, TurnID: "t1"})
	trace.ObserveOutput(10)
	now = now.Add(3 * time.Second)
	trace.ObserveOutput(5)
	now = now.Add(time.Second)
	trace.ObserveOutput(1)
	trace.OutputSummary()
	trace.OutputSummary()
	log.Close()
	records := readRecords(t, path)
	if len(records) != 1 || *records[0].Count != 3 || *records[0].Bytes != 16 || *records[0].MaxGapMs != 3000 {
		t.Fatalf("records = %+v", records)
	}
}
