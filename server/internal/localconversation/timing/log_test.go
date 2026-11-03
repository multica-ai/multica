package timing

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sample(stage string) Record {
	return Record{Stage: stage, Event: EventMark, ConversationID: "conv-1", RequestID: "req-1"}
}

func readRecords(t *testing.T, path string) []Record {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

func TestNilLogIsSafe(t *testing.T) {
	t.Parallel()
	var l *Log
	l.Write(sample(StageTurnPersist))
	l.Flush()
	l.Close()
	if Open("", Options{}) != nil {
		t.Fatal("an empty path disables the log")
	}
	var trace *Trace
	trace.Mark(StageTurnPersist)
	trace.Once(StageTurnPersist)
	trace.Begin(StageTurnPersist)(nil)
	trace.SetPrepareKind(PrepareCold)
	trace.Finish("completed", "", "")
	if NewTrace(nil, TraceOptions{}) != nil {
		t.Fatal("a disabled log needs no trace")
	}
}

func TestLogWritesPrivateCompleteRecords(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "local-conversations", "diagnostics", "daemon-timing.jsonl")
	l := Open(path, Options{})
	l.Write(Record{Stage: StageProviderFirstText, Event: EventMark, ConversationID: "conv-1", Origin: "/Users/me/secret"})
	l.Write(Record{Stage: "made.up", Event: EventMark})
	l.Close()
	l.Write(sample(StageTurnPersist)) // after Close: must not block or panic

	records := readRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	r := records[0]
	if r.Version != Version || r.Source != SourceDaemon || r.Origin != "" || r.At == "" {
		t.Fatalf("record = %+v", r)
	}
	if _, err := time.Parse(atLayout, r.At); err != nil {
		t.Fatalf("at = %q: %v", r.At, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v", info.Mode())
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", info.Mode())
	}
}

func TestLogRotatesKeepingOnePreviousFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon-timing.jsonl")
	l := Open(path, Options{MaxBytes: 400})
	for range 20 {
		l.Write(sample(StageTurnPersist))
	}
	l.Close()
	for _, p := range []string{path, path + ".1"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 400 {
			t.Fatalf("%s is %d bytes", p, info.Size())
		}
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatalf(".2 exists: %v", err)
	}
}

func TestWriteNeverBlocksAndRecordsDroppedWrites(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon-timing.jsonl")
	release := make(chan struct{})
	l := Open(path, Options{QueueSize: 2, beforeWrite: func() { <-release }})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			l.Write(sample(StageTurnPersist))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked on a stalled disk")
	}
	close(release)
	l.Close()

	var dropped int64
	for _, r := range readRecords(t, path) {
		if r.Stage == StageTimingDropped {
			if r.ConversationID != "" || r.Count == nil {
				t.Fatalf("drop record = %+v", r)
			}
			dropped += *r.Count
		}
	}
	if dropped == 0 {
		t.Fatal("dropped writes were not recorded")
	}
}

func TestLogRetriesAfterAWriteFailureAndReportsTheGap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blocked := filepath.Join(dir, "occupied")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocked, "daemon-timing.jsonl")
	l := Open(path, Options{})
	l.Write(sample(StageTurnPersist))
	l.Flush()
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	l.Write(sample(StageTurnFinished))
	l.Close()

	records := readRecords(t, path)
	if len(records) != 2 || records[0].Stage != StageTimingDropped || *records[0].Count != 1 || records[1].Stage != StageTurnFinished {
		t.Fatalf("records = %+v", records)
	}
}

func TestLogIsSafeForConcurrentWriters(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon-timing.jsonl")
	l := Open(path, Options{MaxBytes: 2048})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				l.Write(sample(StageTurnPersist))
			}
		}()
	}
	wg.Wait()
	l.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatalf("torn line: %q", line)
		}
	}
}

func TestTraceRecordsOnlyIdentifiersTimingsAndClassifications(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon-timing.jsonl")
	l := Open(path, Options{})
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { now = now.Add(5 * time.Millisecond); return now }
	turnIndex := int64(3)
	trace := NewTrace(l, TraceOptions{Now: clock, ConversationID: "conv-1", RequestID: "req-1", TurnID: "turn-req-1", Provider: "codex", TurnKind: TurnUser, TurnIndex: &turnIndex, Attempt: 2, Caller: Caller{Origin: OriginLocal, ClientKind: ClientDesktop, Connection: ConnectionLoopback}})
	trace.SetPrepareKind(PrepareCold)
	end := trace.BeginReason(StageThreadPrepare, ReasonResume)
	end(errString("dial tcp 10.0.0.1:443: HTTP 429 at /Users/me/secret"))
	if !trace.Once(StageProviderFirstText) || trace.Once(StageProviderFirstText) {
		t.Fatal("Once records one mark")
	}
	trace.Finish("failed", "provider_error", "HTTP 429 prompt text SECRET")
	l.Close()

	raw, _ := os.ReadFile(path)
	for _, banned := range []string{"SECRET", "10.0.0.1", "/Users", "prompt"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("record leaked %q: %s", banned, raw)
		}
	}
	records := readRecords(t, path)
	if len(records) != 4 {
		t.Fatalf("records = %+v", records)
	}
	failed := records[1]
	if failed.Stage != StageThreadPrepare || failed.Event != EventEnd || failed.Outcome != "failed" || failed.Failure != "rate_limit" || failed.Reason != ReasonResume || failed.PrepareKind != PrepareCold || failed.TurnKind != TurnUser || *failed.TurnIndex != 3 || *failed.Attempt != 2 || failed.Origin != OriginLocal || failed.DurationMs == nil {
		t.Fatalf("failed span = %+v", failed)
	}
	finished := records[3]
	if finished.Stage != StageTurnFinished || finished.Outcome != "failed" || finished.Reason != "provider_error" || finished.Failure != "rate_limit" {
		t.Fatalf("finished = %+v", finished)
	}
}

func TestBackgroundAndNativeTracesAreExcludedFromForegroundSummaries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon-timing.jsonl")
	l := Open(path, Options{})
	for _, kind := range []string{TurnBackground, TurnNative} {
		NewTrace(l, TraceOptions{TurnKind: kind, TurnID: "t"}).Mark(StageProviderFirstText)
	}
	NewTrace(l, TraceOptions{TurnKind: TurnUser, TurnID: "t"}).Mark(StageProviderFirstText)
	l.Close()
	records := readRecords(t, path)
	if !records[0].Background || !records[1].Background || records[2].Background {
		t.Fatalf("records = %+v", records)
	}
}

func TestTraceDropsInvalidIdentifiersInsteadOfTheMark(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon-timing.jsonl")
	l := Open(path, Options{})
	NewTrace(l, TraceOptions{ConversationID: "conv-1", RequestID: "has space/and path"}).Mark(StageTurnPersist)
	l.Close()
	records := readRecords(t, path)
	if len(records) != 1 || records[0].RequestID != "" || records[0].ConversationID != "conv-1" {
		t.Fatalf("records = %+v", records)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
