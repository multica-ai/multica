package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

var localCaller = timing.Caller{Origin: timing.OriginLocal, ClientKind: timing.ClientDesktop, Connection: timing.ConnectionLoopback}

func timingPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "diagnostics", "daemon-timing.jsonl")
}

func timingRecords(t *testing.T, svc *Service, path string) []timing.Record {
	t.Helper()
	svc.traces.log.Flush()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []timing.Record
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r timing.Record
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func stagesOf(records []timing.Record) []string {
	var out []string
	for _, r := range records {
		if !slices.Contains(out, r.Stage) {
			out = append(out, r.Stage)
		}
	}
	return out
}

func findRecord(t *testing.T, records []timing.Record, stage, event string) timing.Record {
	t.Helper()
	for _, r := range records {
		if r.Stage == stage && (event == "" || r.Event == event) {
			return r
		}
	}
	t.Fatalf("no %s %s record in %v", stage, event, stagesOf(records))
	return timing.Record{}
}

func wantStages(t *testing.T, records []timing.Record, want ...string) {
	t.Helper()
	got := stagesOf(records)
	for _, stage := range want {
		if !slices.Contains(got, stage) {
			t.Errorf("no %s in %v", stage, got)
		}
	}
}

// timedCodexService runs Codex conversations on a fake app-server with timing on.
func timedCodexService(t *testing.T, path string, startErr error) (*serviceFixture, *fakeCodex) {
	t.Helper()
	server := &fakeCodex{params: map[string]map[string]any{}, done: make(chan struct{})}
	f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path, Codex: func(context.Context, sqlitedb.Conversation) (CodexSession, StartCodexServer, error) {
		return CodexSession{}, func(_ context.Context, h codex.Handler, _ []string) (CodexServer, error) {
			if startErr != nil {
				return nil, startErr
			}
			server.handler = h
			return server, nil
		}, nil
	}})
	return f, server
}

func TestTimingFollowsACodexTurnEndToEnd(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f, server := timedCodexService(t, path, nil)
	ctx := context.Background()
	f.create(t, "conv-1", "codex")
	secret := "PROMPT_TEXT_SECRET"
	f.svc.NoteRequest("conv-1", "r1", time.Now(), localCaller)
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: secret}); err != nil {
		t.Fatal(err)
	}

	// The first commit of a turn is the user's message, not model output.
	eventually(t, "the user message to commit", func() bool {
		events, _ := f.svc.Store().TurnEvents(ctx, "conv-1", "turn-r1")
		return len(events) > 0
	})
	time.Sleep(20 * time.Millisecond)
	for _, stage := range stagesOf(timingRecords(t, f.svc, path)) {
		if stage == timing.StageBatcherFirstOutput || stage == timing.StageHubFirstOutputPublish {
			t.Fatalf("%s recorded before any model output", stage)
		}
	}

	sub, _, err := f.svc.Subscribe(ctx, f.scopeID, "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	server.notify(t, "item/started", `{"threadId":"th-1","turnId":"tu-1","item":{"id":"r1","type":"reasoning"}}`)
	server.notify(t, "item/agentMessage/delta", `{"threadId":"th-1","turnId":"tu-1","itemId":"a1","delta":"`+secret+`"}`)
	server.notify(t, "item/started", `{"threadId":"th-1","turnId":"tu-1","item":{"id":"c1","type":"commandExecution","command":"ls"}}`)
	server.notify(t, "turn/completed", `{"threadId":"th-1","turn":{"id":"tu-1","status":"completed"}}`)
	eventually(t, "the turn to finish", func() bool {
		turn, err := f.svc.Store().Turn(ctx, "conv-1", "turn-r1")
		return err == nil && turn.Status == "completed"
	})
	eventually(t, "the output to commit", func() bool {
		return slices.Contains(stagesOf(timingRecords(t, f.svc, path)), timing.StageHubFirstOutputPublish)
	})

	records := timingRecords(t, f.svc, path)
	wantStages(t, records, timing.StageDaemonRequestReceived, timing.StageRunnerCreate, timing.StageRunnerLimiter, timing.StageTurnPersist, timing.StagePrepareWait,
		timing.StageProviderSpawn, timing.StageProviderConfig, timing.StageContextBind, timing.StageThreadPrepare, timing.StageProviderTurnStart,
		timing.StageProviderFirstReasoning, timing.StageProviderFirstText, timing.StageProviderFirstTool,
		timing.StageBatcherFirstOutput, timing.StageHubFirstOutputPublish, timing.StageTurnFinished)
	if r := findRecord(t, records, timing.StageDaemonRequestReceived, ""); r.Attempt == nil || *r.Attempt != 1 || r.Origin != timing.OriginLocal || r.ClientKind != timing.ClientDesktop {
		t.Fatalf("request = %+v", r)
	}
	if r := findRecord(t, records, timing.StageThreadPrepare, timing.EventEnd); r.Reason != timing.ReasonStart || r.Provider != "codex" || r.PrepareKind != timing.PrepareCold || r.Origin != timing.OriginLocal || r.TurnKind != timing.TurnUser || r.TurnIndex == nil || *r.TurnIndex != 0 || r.TurnID != "turn-r1" {
		t.Fatalf("thread.prepare = %+v", r)
	}
	if r := findRecord(t, records, timing.StageTurnFinished, ""); r.Outcome != "completed" {
		t.Fatalf("finished = %+v", r)
	}
	for _, stage := range []string{timing.StageProviderFirstText, timing.StageBatcherFirstOutput, timing.StageHubFirstOutputPublish} {
		count := 0
		for _, r := range records {
			if r.Stage == stage {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s recorded %d times", stage, count)
		}
	}
	raw, _ := os.ReadFile(path)
	for _, banned := range []string{secret, "/repo", "ls"} {
		if strings.Contains(string(raw), `"`+banned) || strings.Contains(string(raw), banned+`"`) {
			t.Fatalf("timing leaked %q:\n%s", banned, raw)
		}
	}

	// A second turn on the running process is hot and resumes.
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r2", Text: "again"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range timingRecords(t, f.svc, path) {
		if r.RequestID == "r2" && r.Stage == timing.StageThreadPrepare {
			if r.Reason != timing.ReasonResume || r.PrepareKind != timing.PrepareHot || r.Origin != timing.OriginUnknown || r.TurnIndex == nil || *r.TurnIndex != 1 {
				t.Fatalf("second turn = %+v", r)
			}
		}
	}
}

func TestTimingRecordsAFailedStartWithoutItsErrorText(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f, _ := timedCodexService(t, path, errors.New("exec /Users/me/secret: HTTP 429"))
	f.create(t, "conv-1", "codex")
	if _, err := f.svc.Send(context.Background(), SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err == nil {
		t.Fatal("send succeeded")
	}
	records := timingRecords(t, f.svc, path)
	if r := findRecord(t, records, timing.StageProviderSpawn, ""); r.Outcome != "failed" || r.Failure != "rate_limit" {
		t.Fatalf("spawn = %+v", r)
	}
	if r := findRecord(t, records, timing.StageTurnFinished, ""); r.Outcome != "failed" || r.Reason != "provider_unavailable" || r.Failure != "rate_limit" {
		t.Fatalf("finished = %+v", r)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "/Users") {
		t.Fatalf("timing leaked the error:\n%s", raw)
	}
}

func TestTimingReadsTheAdapterStartResultAndToleratesAnOlderAdapter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, result string
		measured     bool
	}{
		{"current", `{"models":[],"timing":{"sdk_load_ms":12,"query_init_ms":234.5}}`, true},
		{"older", `{"models":[]}`, false},
		{"malformed", `{"timing":{"sdk_load_ms":"soon","query_init_ms":-4}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := timingPath(t)
			f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path})
			f.reply = func(_ *fakeAdapter, typ string) (json.RawMessage, error) {
				if typ == "start" {
					return json.RawMessage(tc.result), nil
				}
				return json.RawMessage(`{}`), nil
			}
			f.create(t, "conv-1", "claude")
			ctx := context.Background()
			if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
				t.Fatal(err)
			}
			a := f.allAdapters()[0]
			a.emit(t, `{"type":"assistant","uuid":"u1","message":{"id":"m1","content":[{"type":"text","text":"hello"}]}}`, turnDone)
			eventually(t, "the turn to finish", func() bool {
				return slices.Contains(stagesOf(timingRecords(t, f.svc, path)), timing.StageTurnFinished)
			})
			records := timingRecords(t, f.svc, path)
			wantStages(t, records, timing.StagePrepareWait, timing.StageAdapterSpawn, timing.StageContextBind, timing.StageProviderInputQueued, timing.StageProviderFirstMessage, timing.StageProviderFirstText)
			got := stagesOf(records)
			if slices.Contains(got, timing.StageSDKLoad) != tc.measured || slices.Contains(got, timing.StageQueryInit) != tc.measured {
				t.Fatalf("adapter marks in %v, want measured=%v", got, tc.measured)
			}
			if tc.measured {
				if r := findRecord(t, records, timing.StageQueryInit, ""); r.DurationMs == nil || *r.DurationMs != 234 {
					t.Fatalf("query.init = %+v", r)
				}
				if r := findRecord(t, records, timing.StageSDKLoad, ""); r.DurationMs == nil || *r.DurationMs != 12 {
					t.Fatalf("sdk.load = %+v", r)
				}
			}
		})
	}
}

func TestTimingKeepsTheCallerOfAQueuedTurn(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path})
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	phone := timing.Caller{Origin: timing.OriginP2P, ClientKind: timing.ClientMobile, Connection: timing.ConnectionUnknown}

	// Nothing is running: the queue is only how this send travelled.
	f.svc.NoteRequest("conv-1", "first", time.Now(), localCaller)
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("first", "first"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first turn to send", func() bool { return len(f.sends(t)) == 1 })

	// This one waits behind the running turn.
	f.svc.NoteRequest("conv-1", "waiting", time.Now(), phone)
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("waiting", "second"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	f.allAdapters()[0].emit(t, turnDone)
	eventually(t, "the queued turn to send", func() bool { return len(f.sends(t)) == 2 })
	eventually(t, "its dispatch mark", func() bool {
		for _, r := range timingRecords(t, f.svc, path) {
			if r.RequestID == "waiting" && r.Stage == timing.StageQueueDispatch {
				return true
			}
		}
		return false
	})

	for _, r := range timingRecords(t, f.svc, path) {
		switch {
		case r.RequestID == "first" && r.TurnKind != "" && r.TurnKind != timing.TurnUser:
			t.Errorf("unwaited send = %+v", r)
		case r.RequestID == "waiting" && r.Stage != timing.StageDaemonRequestReceived && (r.TurnKind != timing.TurnQueued || r.Origin != timing.OriginP2P || r.ClientKind != timing.ClientMobile):
			t.Errorf("queued turn lost its caller: %+v", r)
		}
	}
}

func TestTimingCallerIsUnknownForAQueueLeftByARestart(t *testing.T) {
	t.Parallel()

	dbPath, logPath := filepath.Join(t.TempDir(), "c.sqlite"), timingPath(t)
	f := newServiceFixtureWith(t, dbPath, Options{TimingPath: logPath})
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	f.svc.NoteRequest("conv-1", "r1", time.Now(), localCaller)
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("r1", "later"), true, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Close(ctx); err != nil {
		t.Fatal(err)
	}

	again := newServiceFixtureWith(t, dbPath, Options{TimingPath: logPath})
	if _, err := again.svc.UpdateQueue(ctx, again.scopeID, "conv-1", "resume", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the queued turn to send", func() bool { return len(again.sends(t)) == 1 })
	eventually(t, "its dispatch mark", func() bool {
		return slices.Contains(stagesOf(timingRecords(t, again.svc, logPath)), timing.StageQueueDispatch)
	})
	if r := findRecord(t, timingRecords(t, again.svc, logPath), timing.StageQueueDispatch, ""); r.Origin != timing.OriginUnknown || r.ClientKind != timing.ClientUnknown || r.TurnKind != timing.TurnQueued {
		t.Fatalf("after restart = %+v", r)
	}
}

func TestTimingMarksNativeTurnsAsBackground(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path})
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	a := f.allAdapters()[0]
	a.emit(t, turnDone)
	eventually(t, "the turn to finish", func() bool {
		turn, err := f.svc.Store().Turn(ctx, "conv-1", "turn-r1")
		return err == nil && turn.Status == "completed"
	})
	a.emit(t, `{"type":"assistant","uuid":"u2","message":{"id":"m2","content":[{"type":"text","text":"on my own"}]}}`, turnDone)
	eventually(t, "the native turn to finish", func() bool {
		for _, r := range timingRecords(t, f.svc, path) {
			if r.TurnKind == timing.TurnNative && r.Stage == timing.StageTurnFinished {
				return r.Background && r.Provider == "claude"
			}
		}
		return false
	})
}

func TestTimingRecordsDeliveryOncePerSubscriberWithItsIdentity(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path})
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	f.svc.NoteRequest("conv-1", "r1", time.Now(), localCaller)
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	user := store.CommittedEvent{TurnID: "turn-r1", Type: "user.message", Payload: json.RawMessage(`{"content":"hi"}`)}
	started := store.CommittedEvent{TurnID: "turn-r1", Type: "turn.started", Payload: json.RawMessage(`{}`)}
	output := store.CommittedEvent{TurnID: "turn-r1", Type: "item.delta", Payload: json.RawMessage(`{"kind":"assistant_text","text":"x"}`)}
	phone := timing.Caller{Origin: timing.OriginP2P, ClientKind: timing.ClientMobile}

	f.svc.NoteDelivered([]store.CommittedEvent{user, started}, "desktop", localCaller, true)
	if slices.Contains(stagesOf(timingRecords(t, f.svc, path)), timing.StageWSFirstOutputWrite) {
		t.Fatal("recorded a write before any model output")
	}
	f.svc.NoteDelivered([]store.CommittedEvent{output}, "desktop", localCaller, true)
	f.svc.NoteDelivered([]store.CommittedEvent{output}, "desktop", localCaller, true)
	f.svc.NoteDelivered([]store.CommittedEvent{output}, "phone", phone, false)
	f.svc.NoteDelivered([]store.CommittedEvent{{TurnID: "old-turn", Type: "item.delta", Payload: json.RawMessage(`{"kind":"assistant_text"}`)}}, "phone", phone, false)

	var writes []timing.Record
	for _, r := range timingRecords(t, f.svc, path) {
		if r.Stage == timing.StageWSFirstOutputWrite {
			writes = append(writes, r)
		}
	}
	if len(writes) != 2 || writes[0].Reason != timing.ReasonLive || writes[0].ClientKind != timing.ClientDesktop || writes[1].Reason != timing.ReasonCatchUp || writes[1].ClientKind != timing.ClientMobile || writes[1].Origin != timing.OriginP2P {
		t.Fatalf("writes = %+v", writes)
	}
}

func TestTimingAttemptsCountRetries(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path})
	f.create(t, "conv-1", "claude")
	f.svc.NoteRequest("conv-1", "r1", time.Now(), localCaller)
	f.svc.NoteRequest("conv-1", "r1", time.Now(), localCaller)
	records := timingRecords(t, f.svc, path)
	if len(records) != 2 || *records[1].Attempt != 2 {
		t.Fatalf("records = %+v", records)
	}
}

func TestTimingRunnerRecordsAFullLimiter(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	log := timing.Open(path, timing.Options{})
	limiter := NewLimiter(0)
	f := newRunnerFixture(t, limiter, nil)
	trace := timing.NewTrace(log, timing.TraceOptions{ConversationID: "conv-1", RequestID: "r", TurnID: "t"})
	err := f.runner.Send(context.Background(), SendRequest{TurnID: "t", RequestID: "r", MessageID: "m", Text: "x", Trace: trace})
	if !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("err = %v", err)
	}
	log.Close()
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"stage":"runner.limiter"`) || !strings.Contains(string(raw), `"reason":"concurrency_limit"`) {
		t.Fatalf("records = %s", raw)
	}
}

func TestTimingRecordsTheFirstFrameAndATurnSummaryOnce(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f, server := timedCodexService(t, path, nil)
	f.create(t, "conv-1", "codex")
	if _, err := f.svc.Send(context.Background(), SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	server.notify(t, "item/agentMessage/delta", `{"threadId":"th-1","turnId":"tu-1","itemId":"a1","delta":"one"}`)
	server.notify(t, "item/agentMessage/delta", `{"threadId":"th-1","turnId":"tu-1","itemId":"a1","delta":"two"}`)
	server.notify(t, "turn/completed", `{"threadId":"th-1","turn":{"id":"tu-1","status":"completed"}}`)
	eventually(t, "the summary", func() bool {
		return slices.Contains(stagesOf(timingRecords(t, f.svc, path)), timing.StageTurnOutputSummary)
	})
	records := timingRecords(t, f.svc, path)
	first, summaries := 0, 0
	for _, r := range records {
		switch r.Stage {
		case timing.StageProviderFirstFrame:
			first++
		case timing.StageTurnOutputSummary:
			summaries++
			if r.TurnID != "turn-r1" || r.Count == nil || *r.Count < 3 || r.Bytes == nil || *r.Bytes == 0 || r.MaxGapMs == nil {
				t.Fatalf("summary = %+v", r)
			}
		}
	}
	if first != 1 || summaries != 1 {
		t.Fatalf("first frames %d, summaries %d in %v", first, summaries, stagesOf(records))
	}
}

func TestTimingRecordsWhyACodexProcessEnded(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f, server := timedCodexService(t, path, nil)
	f.create(t, "conv-1", "codex")
	if _, err := f.svc.Send(context.Background(), SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	server.Stop()
	eventually(t, "the exit", func() bool {
		return slices.Contains(stagesOf(timingRecords(t, f.svc, path)), timing.StageProviderExit)
	})
	if r := findRecord(t, timingRecords(t, f.svc, path), timing.StageProviderExit, ""); r.Reason != timing.ReasonExited || r.TurnID != "turn-r1" {
		t.Fatalf("exit during a turn = %+v", r)
	}
}

func TestTimingRecordsClaudeSessionAndResumeTranscriptSize(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	f := newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), Options{TimingPath: path})
	f.create(t, "conv-1", "claude")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	a := f.allAdapters()[0]
	a.emit(t, `{"type":"system","subtype":"init","session_id":"sess-1"}`, `{"type":"assistant","uuid":"u1","session_id":"sess-1","message":{"id":"m1","content":[{"type":"text","text":"hello"}]}}`, turnDone)
	eventually(t, "the turn to finish", func() bool {
		return slices.Contains(stagesOf(timingRecords(t, f.svc, path)), timing.StageTurnFinished)
	})
	records := timingRecords(t, f.svc, path)
	if r := findRecord(t, records, timing.StageProviderSessionReady, ""); r.TurnID != "turn-r1" {
		t.Fatalf("session ready = %+v", r)
	}

}

func TestTimingRecordsTheTranscriptSizeOfAResume(t *testing.T) {
	t.Parallel()

	path := timingPath(t)
	log := timing.Open(path, timing.Options{})
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	r := NewClaudeRunner(ClaudeRunnerOptions{
		ConversationID: "conv-1", ScopeID: f.scopeID, Files: f.store, Session: ClaudeSession{ProviderSessionID: "sess-1", Fields: map[string]any{}}, Batcher: f.batcher, Limiter: f.limiter,
		TranscriptSize: func(id string) (int64, bool) { return int64(len(id)) * 100, id == "sess-1" },
		Start: func(_ context.Context, h claude.Handler) (ClaudeAdapter, error) {
			return &fakeAdapter{handler: h, done: make(chan struct{})}, nil
		},
	})
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	if err := r.Prepare(context.Background(), timing.NewTrace(log, timing.TraceOptions{ConversationID: "conv-1"})); err != nil {
		t.Fatal(err)
	}
	var found *timing.Record
	eventually(t, "the size", func() bool {
		log.Flush()
		raw, _ := os.ReadFile(path)
		for _, line := range bytes.Split(raw, []byte("\n")) {
			var record timing.Record
			if json.Unmarshal(line, &record) == nil && record.Stage == timing.StageThreadPrepare && record.Bytes != nil {
				found = &record
				return true
			}
		}
		return false
	})
	if *found.Bytes != 600 || found.Reason != timing.ReasonResume {
		t.Fatalf("record = %+v", found)
	}
	log.Close()
}
