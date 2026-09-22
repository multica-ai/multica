package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The delivery contract for a task's reply, probe by probe: one user turn
// gets one answer, whichever path, process or failure mode produces it.
//
// These cases were written as an audit of the duplicate reports (GH #8049,
// #7750) and failed on main; they run by default now that they pass, because
// each one is a way the reply used to arrive twice. Everything here uses a
// local fake Bot API, a controllable clock and a shared fake database — two
// Outbound values sharing one of those stand in for two backend replicas.
type auditBot struct {
	mu                     sync.Mutex
	messages               map[int64]string
	methods                []string
	loseFirstSendResponse  bool
	serverErrorAfterAccept bool
	failFirstEdit          bool
	forbidEdits            bool
	sends                  int64
	edits                  int
}

func (a *auditBot) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	a.mu.Lock()
	defer a.mu.Unlock()
	a.methods = append(a.methods, method)
	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "sendMessage":
		a.sends++
		a.messages[a.sends] = body["text"].(string)
		if a.serverErrorAfterAccept && a.sends == 1 {
			// Accepted, then failed on the way back. The message is in the
			// chat; the caller has no way to know that.
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"Internal Server Error"}`))
			return
		}
		if a.loseFirstSendResponse && a.sends == 1 {
			// The provider accepted the message, but the caller gets no ID.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, a.sends)
	case "editMessageText":
		a.edits++
		if a.forbidEdits {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message can't be edited"}`))
			return
		}
		a.messages[int64(body["message_id"].(float64))] = body["text"].(string)
		if a.failFirstEdit && a.edits == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"Internal Server Error"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}
}

func (a *auditBot) wantOne(t *testing.T) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) != 1 {
		t.Fatalf("visible replies = %d, want 1; methods=%v messages=%v", len(a.messages), a.methods, a.messages)
	}
}

// Lease expiry is database time, which auditClock cannot move, so cases that
// exercise a takeover shorten the lease and really wait it out. The lease also
// sizes every provider call's budget (a third of it, see callContext), and that
// budget covers real Postgres writes: the 33ms a 100ms lease left timed out a
// send claim on a slow -race CI runner and failed the case (MUL-7547).
const (
	testLeaseTTL = 600 * time.Millisecond
	// testLeaseLapse outlasts testLeaseTTL, so any lease taken before it has expired.
	testLeaseLapse = testLeaseTTL + 150*time.Millisecond
)

type auditClock struct{ nanos atomic.Int64 }

func (c *auditClock) now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *auditClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func auditSetup(t *testing.T, bot *auditBot) (*Outbound, *fakeTelegramOutboundQueries, *auditClock, *httptest.Server) {
	t.Helper()
	bot.messages = make(map[int64]string)
	srv := httptest.NewServer(http.HandlerFunc(bot.serve))
	t.Cleanup(srv.Close)
	q := newTelegramOutboundQueries(t)
	q.channelOrigin = true
	o := NewOutbound(q, nil, srv.URL, srv.Client(), nil)
	c := &auditClock{}
	c.nanos.Store(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC).UnixNano())
	o.now = c.now
	o.wait = func(_ context.Context, d time.Duration) error { c.advance(d); return nil }
	return o, q, c, srv
}

func auditPartial(e events.Event) events.Event {
	return events.Event{Type: protocol.EventTaskMessage, TaskID: e.TaskID,
		Payload: protocol.TaskMessagePayload{TaskID: e.TaskID, Type: "text", Content: chatDoneContent(e.Payload)}}
}

// Drive the real terminal queue without wall-clock sleeps or worker races.
func auditDrain(t *testing.T, o *Outbound, c *auditClock, sessionID string) {
	t.Helper()
	for {
		o.terminalMu.Lock()
		s := o.terminalSessions[sessionID]
		if s == nil || len(s.queue) == 0 {
			o.terminalMu.Unlock()
			return
		}
		reply := s.queue[0]
		s.running = true
		s.ready = false
		o.terminalInFlight++
		o.terminalMu.Unlock()
		finished := false
		for i := 0; i < 25; i++ {
			result := o.sendNextTerminalRequest(context.Background(), reply)
			if result.done {
				o.cleanupTerminalReply(reply)
				o.updateQueueAfterTerminalRequest(terminalResult{
					terminalWork: terminalWork{sessionID: sessionID, reply: reply}, done: true, err: result.err})
				if result.err != nil {
					t.Fatalf("terminal delivery failed: %v", result.err)
				}
				finished = true
				break
			}
			if d := result.retryAt.Sub(c.now()); d > 0 {
				c.advance(d)
			}
		}
		if !finished {
			t.Fatal("terminal reply did not settle after 25 requests")
		}
	}
}

// seedRetryChain records that retryTask is an automatic retry of firstTask.
// Delivery ownership keys on that lineage rather than guessing which pending
// reply looked closest, so the relationship has to be real for the retry to
// inherit anything — which is the whole point of the case below.
func seedRetryChain(t *testing.T, firstTask, retryTask string) {
	t.Helper()
	unique := time.Now().UnixNano()
	fx := testutil.New(testPool, "", "")
	fx.UserID = fx.User(t, "telegram-delivery", fmt.Sprintf("telegram-delivery-%d@example.test", unique))
	fx.WorkspaceID = fx.Workspace(t, "telegram-delivery", fmt.Sprintf("telegram-delivery-%d", unique))
	agentID := fx.Agent(t, "telegram-delivery", "")
	finished := testutil.Cols{"status": "completed", "completed_at": testutil.Raw("now()")}
	fx.Task(t, agentID, merged(finished, testutil.Cols{"id": firstTask}))
	fx.Task(t, agentID, merged(finished, testutil.Cols{"id": retryTask, "retry_of_task_id": firstTask}))
}

func merged(base, extra testutil.Cols) testutil.Cols {
	out := testutil.Cols{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestAuditLatePartialAfterCompletion(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	c.advance(editInterval + time.Millisecond)
	o.handleTaskMessage(auditPartial(e))
	bot.wantOne(t)
}

func TestAuditDistinctReplicasForPartialAndCompletion(t *testing.T) {
	bot := &auditBot{}
	a, q, c, srv := auditSetup(t, bot)
	b := NewOutbound(q, nil, srv.URL, srv.Client(), nil)
	b.now = c.now
	e := telegramTestEvent()
	a.handleTaskMessage(auditPartial(e))
	b.enqueueTerminalReply(e)
	auditDrain(t, b, c, e.ChatSessionID)
	bot.wantOne(t)
}

func TestAuditAcceptedPlaceholderWithLostResponse(t *testing.T) {
	bot := &auditBot{loseFirstSendResponse: true}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.handleTaskMessage(auditPartial(e))
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	bot.wantOne(t)
}

func TestAuditAmbiguousFinalEdit(t *testing.T) {
	bot := &auditBot{failFirstEdit: true}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.handleTaskMessage(auditPartial(e))
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	bot.wantOne(t)
}

func TestAuditAmbiguousFailureNoticeEdit(t *testing.T) {
	bot := &auditBot{failFirstEdit: true}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.handleTaskMessage(auditPartial(e))
	o.handleTaskFailed(events.Event{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID,
		Type: protocol.EventTaskFailed, Payload: map[string]any{"retry_pending": false}})
	auditDrain(t, o, c, e.ChatSessionID)
	bot.wantOne(t)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.messages[1] != taskFailedText {
		t.Fatalf("placeholder was not turned into the failure notice: %q; methods=%v", bot.messages[1], bot.methods)
	}
}

func TestAuditDuplicateTerminalEvent(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.enqueueTerminalReply(e)
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	bot.wantOne(t)
}

func TestAuditLatePartialAfterCancellationOrEmptyCompletion(t *testing.T) {
	for _, terminal := range []string{"cancelled", "empty"} {
		t.Run(terminal, func(t *testing.T) {
			bot := &auditBot{}
			o, _, c, _ := auditSetup(t, bot)
			e := telegramTestEvent()
			o.handleTaskMessage(auditPartial(e))
			if terminal == "cancelled" {
				o.handleTaskCancelled(e)
			} else {
				empty := e
				empty.Payload = protocol.ChatDonePayload{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID}
				o.enqueueTerminalReply(empty)
			}
			c.advance(editInterval + time.Millisecond)
			o.handleTaskMessage(auditPartial(e))
			bot.wantOne(t)
		})
	}
}

type auditGatedQueries struct {
	*fakeTelegramOutboundQueries
	first   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (q *auditGatedQueries) GetChannelTaskDelivery(ctx context.Context, id pgtype.UUID) (db.ChannelTaskDelivery, error) {
	if q.first.CompareAndSwap(false, true) {
		close(q.entered)
		select {
		case <-q.release:
		case <-ctx.Done():
			return db.ChannelTaskDelivery{}, ctx.Err()
		}
	}
	return q.fakeTelegramOutboundQueries.GetChannelTaskDelivery(ctx, id)
}

func TestAuditPartialResolutionOverlapsCompletion(t *testing.T) {
	bot := &auditBot{}
	o, base, c, _ := auditSetup(t, bot)
	q := &auditGatedQueries{fakeTelegramOutboundQueries: base, entered: make(chan struct{}), release: make(chan struct{})}
	o.q = q
	e := telegramTestEvent()
	finished := make(chan struct{})
	go func() { defer close(finished); o.handleTaskMessage(auditPartial(e)) }()
	defer func() { close(q.release); <-finished }()
	<-q.entered
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	c.advance(editInterval + time.Millisecond)
	q.release <- struct{}{}
	<-finished
	bot.wantOne(t)
}

func TestAuditAutomaticRetryPreservesSingleReply(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	first := telegramTestEvent()
	retryTask := telegramTestEventFor(3, 5, "").TaskID
	seedRetryChain(t, first.TaskID, retryTask)
	o.handleTaskMessage(auditPartial(first))
	o.handleTaskFailed(events.Event{TaskID: first.TaskID, Type: protocol.EventTaskFailed,
		Payload: map[string]any{"retry_pending": true}})
	c.advance(editInterval + time.Millisecond)
	retry := telegramTestEventFor(3, 5, chatDoneContent(first.Payload))
	_ = retryTask
	o.handleTaskMessage(auditPartial(retry))
	o.enqueueTerminalReply(retry)
	auditDrain(t, o, c, retry.ChatSessionID)
	bot.wantOne(t)
}

func TestAuditPermanentEditFailureDoesNotBlockSession(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.handleTaskMessage(auditPartial(e))
	bot.mu.Lock()
	bot.forbidEdits = true
	bot.mu.Unlock()
	o.enqueueTerminalReply(e)
	reply := o.terminalSessions[e.ChatSessionID].queue[0]
	for i := 0; i < 25; i++ {
		result := o.sendNextTerminalRequest(context.Background(), reply)
		if result.done {
			bot.wantOne(t)
			return
		}
		if d := result.retryAt.Sub(c.now()); d > 0 {
			c.advance(d)
		}
	}
	t.Fatal("25 terminal steps did not settle a permanent edit rejection; the same session's next reply remains blocked")
}

func TestAuditLatePartialAfterDedupTTL(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	c.advance(11 * time.Minute)
	o.handleTaskMessage(auditPartial(e))
	bot.wantOne(t)
}

type auditLedgerQueries struct {
	*fakeTelegramOutboundQueries
	row db.ChatMessage
}

func (q *auditLedgerQueries) GetChatMessageByTaskAssistant(context.Context, pgtype.UUID) (db.ChatMessage, error) {
	return q.row, nil
}

func (q *auditLedgerQueries) SetChatMessageChannelOutboundProvenanceByTask(_ context.Context, p db.SetChatMessageChannelOutboundProvenanceByTaskParams) (int64, error) {
	q.row.ChannelOutboundType = p.ChannelType
	q.row.ChannelOutboundInstallationID = p.InstallationID
	q.row.ChannelOutboundChatID = p.ChannelChatID
	q.row.ChannelOutboundMessageIds = append([]string(nil), p.MessageIds...)
	return 1, nil
}

func (q *auditLedgerQueries) RecordChannelOutboundMessage(context.Context, db.RecordChannelOutboundMessageParams) error {
	return nil
}

func TestAuditReplayAfterInterruptionBetweenChunks(t *testing.T) {
	bot := &auditBot{}
	a, base, c, srv := auditSetup(t, bot)
	q := &auditLedgerQueries{fakeTelegramOutboundQueries: base, row: db.ChatMessage{ID: telegramTestUUID(9)}}
	a.q = q
	// The interrupted process never releases its lease — it stops existing.
	// Expiry is what frees the turn, and expiry is database time, so the test
	// shortens the lease and really waits for it.
	a.leaseTTL = testLeaseTTL
	e := telegramTestEventFor(3, 2, strings.Repeat("A", maxMessageUnits)+"tail")
	a.enqueueTerminalReply(e)
	reply := a.terminalSessions[e.ChatSessionID].queue[0]
	for i := 0; reply.chunkIndex < 1 && i < 10; i++ {
		result := a.sendNextTerminalRequest(context.Background(), reply)
		if result.done {
			t.Fatalf("unexpected completion before first chunk: %+v", result)
		}
		if d := result.retryAt.Sub(c.now()); d > 0 {
			c.advance(d)
		}
	}
	if reply.chunkIndex != 1 {
		t.Fatal("first chunk was never sent")
	}
	// Simulate losing process memory before the next chunk. A replay-capable
	// caller delivers the same completion against the same durable database.
	time.Sleep(testLeaseLapse)
	b := NewOutbound(q, nil, srv.URL, srv.Client(), nil)
	b.now = c.now
	b.leaseTTL = testLeaseTTL
	b.enqueueTerminalReply(e)
	auditDrain(t, b, c, e.ChatSessionID)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.messages) != 2 {
		t.Fatalf("2-chunk reply produced %d visible messages after interruption; methods=%v", len(bot.messages), bot.methods)
	}
}

func TestAuditControlNormalStreamAndFinalEdit(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	e := telegramTestEvent()
	o.handleTaskMessage(auditPartial(e))
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	bot.wantOne(t)
}

func TestAuditControlSeparateTurnsWithIdenticalText(t *testing.T) {
	bot := &auditBot{}
	o, _, c, _ := auditSetup(t, bot)
	for _, id := range []byte{2, 5} {
		e := telegramTestEventFor(3, id, "same legitimate answer")
		o.enqueueTerminalReply(e)
		auditDrain(t, o, c, e.ChatSessionID)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.messages) != 2 {
		t.Fatalf("distinct turns produced %d replies, want 2", len(bot.messages))
	}
}
