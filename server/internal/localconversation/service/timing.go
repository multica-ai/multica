package service

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// maxTimedEntries bounds the in-memory request and turn maps: delivery marks
// can follow a turn's end, so entries age out by count rather than on finish.
const maxTimedEntries = 512

// requestTiming is what is known about a request before it becomes a turn.
// It lives only in memory, so after a restart a queued item's caller is unknown.
type requestTiming struct {
	caller   timing.Caller
	received time.Time
	attempts int64
	// waited is set when a queued item had to wait for its turn.
	waited bool
}

// turnTraces holds each recent turn's trace, so marks recorded far from the
// runner (commit, publish, delivery) can find it by turn id.
type turnTraces struct {
	log *timing.Log
	now func() time.Time

	mu           sync.Mutex
	requests     map[string]*requestTiming
	requestOrder []string
	turns        map[string]*timing.Trace
	turnOrder    []string
}

func newTurnTraces(log *timing.Log, now func() time.Time) *turnTraces {
	return &turnTraces{log: log, now: now, requests: map[string]*requestTiming{}, turns: map[string]*timing.Trace{}}
}

// NoteRequest records that a client asked for requestID; a retry counts as
// another attempt. The transport passes its caller as plain values.
func (s *Service) NoteRequest(conversationID, requestID string, received time.Time, caller timing.Caller) {
	t := s.traces
	if t.log == nil || requestID == "" {
		return
	}
	t.mu.Lock()
	info := t.requests[requestID]
	if info == nil {
		info = &requestTiming{caller: caller, received: received}
		t.requests[requestID] = info
		t.requestOrder = append(t.requestOrder, requestID)
		if len(t.requestOrder) > maxTimedEntries {
			delete(t.requests, t.requestOrder[0])
			t.requestOrder = t.requestOrder[1:]
		}
	}
	info.attempts++
	attempts := info.attempts
	t.mu.Unlock()
	timing.NewTrace(t.log, timing.TraceOptions{Now: t.now, Start: received, ConversationID: conversationID, RequestID: requestID, Attempt: attempts, Caller: caller}).MarkAt(timing.StageDaemonRequestReceived, received)
}

// NoteDelivered records the first model output written to one subscriber of
// each turn in events, and whether it arrived live or in a catch-up.
func (s *Service) NoteDelivered(events []store.CommittedEvent, subscriber string, caller timing.Caller, live bool) {
	reason := timing.ReasonCatchUp
	if live {
		reason = timing.ReasonLive
	}
	s.traces.each(events, timing.StageWSFirstOutputWrite, subscriber, func(trace *timing.Trace) {
		trace.OnceFor(timing.StageWSFirstOutputWrite, subscriber, reason, caller)
	})
}

// waited notes that a queued request could not run at once.
func (t *turnTraces) waited(requestID string) {
	t.mu.Lock()
	if info := t.requests[requestID]; info != nil {
		info.waited = true
	}
	t.mu.Unlock()
}

// dispatching returns the trace of a queued item about to be sent. An item
// that never waited is an ordinary send that happened to use the queue, and
// one with no record survived a restart.
func (t *turnTraces) dispatching(conv sqlitedb.Conversation, item queueItem) *timing.Trace {
	if len(item.Agents) > 0 {
		return t.detached(conv, item.RequestID, "", timing.TurnAgent)
	}
	t.mu.Lock()
	info := t.requests[item.RequestID]
	t.mu.Unlock()
	kind := timing.TurnQueued
	if info != nil && !info.waited {
		kind = timing.TurnUser
	}
	return t.turn(conv, item.RequestID, "turn-"+item.RequestID, kind)
}

// turn returns the turn's trace, starting it on first use.
func (t *turnTraces) turn(conv sqlitedb.Conversation, requestID, turnID, kind string) *timing.Trace {
	if t.log == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if trace := t.turns[turnID]; trace != nil {
		return trace
	}
	trace := t.newTraceLocked(conv, requestID, turnID, kind)
	t.turns[turnID] = trace
	t.turnOrder = append(t.turnOrder, turnID)
	if len(t.turnOrder) > maxTimedEntries {
		delete(t.turns, t.turnOrder[0])
		t.turnOrder = t.turnOrder[1:]
	}
	return trace
}

// detached is a trace not found by turn id, for records that do not stand for
// the turn itself: steering a running turn, or a request for several agents.
func (t *turnTraces) detached(conv sqlitedb.Conversation, requestID, turnID, kind string) *timing.Trace {
	if t.log == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.newTraceLocked(conv, requestID, turnID, kind)
}

func (t *turnTraces) newTraceLocked(conv sqlitedb.Conversation, requestID, turnID, kind string) *timing.Trace {
	opts := timing.TraceOptions{Now: t.now, ConversationID: conv.ID, RequestID: requestID, TurnID: turnID, TurnKind: kind}
	// An agent turn runs in the cloud, not on the conversation's local provider.
	if kind != timing.TurnAgent {
		opts.Provider = conv.Provider
	}
	// A native turn's runner holds a stale conversation row, so its index is not known here.
	if kind != timing.TurnNative {
		index := conv.TurnCount
		opts.TurnIndex = &index
	}
	if info := t.requests[requestID]; info != nil {
		opts.Caller, opts.Start, opts.Attempt = info.caller, info.received, info.attempts
	}
	return timing.NewTrace(t.log, opts)
}

// conversation is a trace for a conversation's records that belong to no turn.
func (t *turnTraces) conversation(conv sqlitedb.Conversation, caller timing.Caller) *timing.Trace {
	return timing.NewTrace(t.log, timing.TraceOptions{Now: t.now, ConversationID: conv.ID, Provider: conv.Provider, Caller: caller})
}

// process is a trace for a conversation's process, known only by id and provider.
func (t *turnTraces) process(conversationID, provider string) *timing.Trace {
	return timing.NewTrace(t.log, timing.TraceOptions{Now: t.now, ConversationID: conversationID, Provider: provider})
}

func (t *turnTraces) lookup(turnID string) *timing.Trace {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.turns[turnID]
}

// reached records stage once per turn at the turn's first model output.
func (t *turnTraces) reached(events []store.CommittedEvent, stage string) {
	t.each(events, stage, "", func(trace *timing.Trace) { trace.Once(stage) })
}

// each calls mark for every turn in events with model output whose mark is
// still open. Payloads are only decoded for those, so a long turn's deltas
// cost one map lookup each.
func (t *turnTraces) each(events []store.CommittedEvent, stage, key string, mark func(*timing.Trace)) {
	if t.log == nil {
		return
	}
	for _, e := range events {
		if e.TurnID == "" {
			continue
		}
		trace := t.lookup(e.TurnID)
		if trace == nil || trace.Reached(stage, key) {
			continue
		}
		var payload struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(e.Payload, &payload)
		if timing.IsModelOutput(e.Type, payload.Kind) {
			mark(trace)
		}
	}
}

// observe adds committed events to their turns' summaries and writes each summary
// when its turn's last event commits, so a turn's final batch is counted.
func (t *turnTraces) observe(events []store.CommittedEvent) {
	if t.log == nil {
		return
	}
	for _, e := range events {
		if e.TurnID == "" {
			continue
		}
		trace := t.lookup(e.TurnID)
		trace.ObserveOutput(int64(len(e.Payload)))
		if e.Type == "turn.completed" {
			trace.OutputSummary()
		}
	}
}

// markProcessExit records a provider process ending, on the turn it interrupted
// or otherwise on the process itself.
func markProcessExit(process func() *timing.Trace, running *timing.Trace, reason string) {
	if running == nil && process != nil {
		running = process()
	}
	running.MarkReason(timing.StageProviderExit, reason)
}

// startDurations is implemented by provider processes that can tell launching
// apart from their handshake; a test's fake reports one span instead.
type startDurations interface {
	StartDurations() (spawn, handshake time.Duration)
}

// markProcessStart records a provider process start that began at began.
func markProcessStart(trace *timing.Trace, spawnStage, readyStage string, began time.Time, process any, err error) {
	if trace == nil {
		return
	}
	end := trace.Now()
	if timed, ok := process.(startDurations); ok && err == nil {
		spawn, handshake := timed.StartDurations()
		trace.Completed(spawnStage, began.Add(spawn), spawn, nil)
		trace.Completed(readyStage, end, handshake, nil)
		return
	}
	trace.Completed(spawnStage, end, end.Sub(began), err)
}

// markAdapterStart records what the adapter measured inside its start
// request. An older adapter's result has no timing, which is simply not
// recorded.
func markAdapterStart(trace *timing.Trace, began time.Time, raw json.RawMessage) {
	if trace == nil {
		return
	}
	end := trace.Now()
	timed := claude.ParseStartTiming(raw)
	if timed.SDKLoad != nil {
		trace.Completed(timing.StageSDKLoad, began.Add(*timed.SDKLoad), *timed.SDKLoad, nil)
	}
	if timed.QueryInit != nil {
		trace.Completed(timing.StageQueryInit, end, *timed.QueryInit, nil)
	}
}

// markProviderOutput records the first reasoning, text and tool of a turn.
func markProviderOutput(trace *timing.Trace, events []outputEvent) {
	if trace == nil {
		return
	}
	for _, e := range events {
		kind, _ := e.Payload["kind"].(string)
		if !timing.IsModelOutput(e.Type, kind) {
			continue
		}
		switch kind {
		case "reasoning":
			trace.Once(timing.StageProviderFirstReasoning)
		case "assistant_text":
			trace.Once(timing.StageProviderFirstText)
		case "command", "file_change", "tool_call":
			trace.Once(timing.StageProviderFirstTool)
		}
	}
}

func finishTrace(trace *timing.Trace, status string, failure *turnError) {
	if failure == nil {
		trace.Finish(status, "", "")
		return
	}
	trace.Finish(status, failure.kind, failure.message)
}
