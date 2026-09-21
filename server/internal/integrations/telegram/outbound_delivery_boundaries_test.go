package telegram

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Boundary cases for reply delivery: concurrency, lost responses, retries and
// takeover, each one a way a reply could still arrive twice, be overwritten,
// or block the session behind it.
//
// These came out of the review of the first attempt at this fix, which passed
// its own suite while the final answer never reached Telegram — the in-memory
// stand-in for the ownership table disagreed with the SQL. So routing is the
// only thing faked here: every ownership operation runs the real generated
// query against the test database.
type review8545Queries struct {
	*db.Queries
	routing *fakeTelegramOutboundQueries
}

func (q *review8545Queries) GetChannelTaskDelivery(ctx context.Context, id pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return q.routing.GetChannelTaskDelivery(ctx, id)
}

func (q *review8545Queries) GetChannelInstallation(ctx context.Context, p db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return q.routing.GetChannelInstallation(ctx, p)
}

func review8545ID() pgtype.UUID {
	id := pgtype.UUID{Valid: true}
	_, _ = rand.Read(id.Bytes[:])
	return id
}

func review8545Setup(t *testing.T, bot *auditBot) (*Outbound, *review8545Queries, *auditClock, events.Event) {
	t.Helper()
	o, routing, c, _ := auditSetup(t, bot)
	pool := testPool
	routing.binding.ID = review8545ID()
	routing.binding.InstallationID = review8545ID()
	routing.installation.ID = routing.binding.InstallationID
	q := &review8545Queries{Queries: db.New(pool), routing: routing}
	o.q = q
	testutil.New(pool, "", "").Cleanup(t, "DELETE FROM channel_reply_delivery WHERE installation_id = $1", routing.installation.ID)
	e := telegramTestEvent()
	e.TaskID = util.UUIDToString(review8545ID())
	e.Payload = protocol.ChatDonePayload{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID, Content: "complete final answer"}
	return o, q, c, e
}

func review8545Second(o *Outbound) *Outbound {
	b := NewOutbound(o.q, nil, o.apiBase, o.client, nil)
	b.now, b.wait = o.now, o.wait
	return b
}

func review8545MessageCount(t *testing.T, bot *auditBot, want int) {
	t.Helper()
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.messages) != want {
		t.Fatalf("visible messages=%d want=%d methods=%v content=%v", len(bot.messages), want, bot.methods, bot.messages)
	}
}

func TestReview8545PostgresFinalTextReplacesPartial(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "short prefix"))
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if got := bot.messages[1]; got != chatDoneContent(e.Payload) {
		t.Fatalf("Telegram kept %q, want final %q; methods=%v", got, chatDoneContent(e.Payload), bot.methods)
	}
}

func TestReview8545PostgresTerminalClaimIsExclusive(t *testing.T) {
	bot := &auditBot{}
	a, _, c, e := review8545Setup(t, bot)
	b := review8545Second(a)
	ra, rb := &terminalReply{event: e}, &terminalReply{event: e}
	for _, pair := range []struct {
		o *Outbound
		r *terminalReply
	}{{a, ra}, {b, rb}} {
		result := pair.o.sendNextTerminalRequest(context.Background(), pair.r)
		if result.err != nil {
			t.Fatal(result.err)
		}
	}
	for _, pair := range []struct {
		o *Outbound
		r *terminalReply
	}{{a, ra}, {b, rb}} {
		for i := 0; i < 5; i++ {
			result := pair.o.sendNextTerminalRequest(context.Background(), pair.r)
			if result.done {
				break
			}
			if d := result.retryAt.Sub(c.now()); d > 0 {
				c.advance(d)
			}
		}
	}
	review8545MessageCount(t, bot, 1)
}

func TestReview8545PostgresUnknownTerminalSendIsNotReplayed(t *testing.T) {
	bot := &auditBot{loseFirstSendResponse: true}
	a, _, c, e := review8545Setup(t, bot)
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), a, e); err == nil {
		t.Fatal("lost response did not fail")
	}
	b := review8545Second(a)
	b.enqueueTerminalReply(e)
	auditDrain(t, b, c, e.ChatSessionID)
	review8545MessageCount(t, bot, 1)
}

func TestReview8545PostgresAbandonedInFlightDoesNotBlockForever(t *testing.T) {
	bot := &auditBot{}
	o, q, c, e := review8545Setup(t, bot)
	// Expiry is decided by database time, which the test clock cannot move, so
	// the lease is shortened and really waited out.
	o.leaseTTL = 100 * time.Millisecond
	ctx := context.Background()
	target, err := o.resolveTarget(ctx, e, false)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := o.turnFor(ctx, target.taskID)
	if err != nil {
		t.Fatal(err)
	}
	lease, status, err := o.acquireDelivery(ctx, target, turn, deliveryPhaseStreaming)
	if err != nil || status != deliveryAcquired {
		t.Fatalf("delivery not acquired: status=%v err=%v", status, err)
	}
	if !o.claimSend(ctx, lease) {
		t.Fatal("send not claimed")
	}
	// The owner process dies after claiming: it never records an outcome and
	// never releases the turn. Only the lease expiring can free it.
	time.Sleep(150 * time.Millisecond)
	c.advance(24 * time.Hour)
	_ = q
	reply := &terminalReply{event: e}
	for i := 0; i < 25; i++ {
		result := o.sendNextTerminalRequest(context.Background(), reply)
		if result.done {
			return
		}
		if d := result.retryAt.Sub(c.now()); d > 0 {
			c.advance(d)
		}
	}
	t.Fatal("orphaned in_flight still waits after 24 hours and 25 attempts; later replies in the session remain blocked")
}

func TestReview8545PostgresCloseBeforeFirstFrame(t *testing.T) {
	for _, reason := range []string{"cancelled", "empty"} {
		t.Run(reason, func(t *testing.T) {
			bot := &auditBot{}
			o, _, c, e := review8545Setup(t, bot)
			if reason == "cancelled" {
				o.handleTaskCancelled(e)
			} else {
				empty := e
				empty.Payload = protocol.ChatDonePayload{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID}
				o.enqueueTerminalReply(empty)
			}
			auditDrain(t, o, c, e.ChatSessionID)
			o.handleTaskMessage(telegramPartialEvent(e.TaskID, "late text after closed task"))
			review8545MessageCount(t, bot, 0)
		})
	}
}

func TestReview8545PostgresRetryDoesNotAdoptUnrelatedTurn(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "first request"))
	o.handleTaskFailed(events.Event{TaskID: e.TaskID, Payload: map[string]any{"retry_pending": true}})
	c.advance(editInterval + time.Millisecond)
	// Different ordinary task on the same binding; no retry relationship.
	otherID := util.UUIDToString(review8545ID())
	o.handleTaskMessage(telegramPartialEvent(otherID, "unrelated second request"))
	review8545MessageCount(t, bot, 2)
}

func TestReview8545PostgresOldAttemptCannotReopenAfterAdoption(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "first attempt"))
	o.handleTaskFailed(events.Event{TaskID: e.TaskID, Payload: map[string]any{"retry_pending": true}})
	c.advance(editInterval + time.Millisecond)
	retryTask := util.UUIDToString(review8545ID())
	seedRetryChain(t, e.TaskID, retryTask)
	o.handleTaskMessage(telegramPartialEvent(retryTask, "retry attempt"))
	c.advance(editInterval + time.Millisecond)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "late old attempt"))
	review8545MessageCount(t, bot, 1)
}

func TestReview8545PostgresRetryWithoutStreamKeepsExistingReply(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "first attempt"))
	o.handleTaskFailed(events.Event{TaskID: e.TaskID, Payload: map[string]any{"retry_pending": true}})
	retryTask := util.UUIDToString(review8545ID())
	seedRetryChain(t, e.TaskID, retryTask)
	e.TaskID = retryTask
	e.Payload = protocol.ChatDonePayload{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID, Content: "retry complete answer"}
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	review8545MessageCount(t, bot, 1)
}

func TestReview8545PostgresFailureNoticeRespectsUnknownSend(t *testing.T) {
	bot := &auditBot{loseFirstSendResponse: true}
	a, _, c, e := review8545Setup(t, bot)
	a.handleTaskMessage(telegramPartialEvent(e.TaskID, "accepted before response loss"))
	b := review8545Second(a)
	b.handleTaskFailed(events.Event{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID,
		Payload: map[string]any{"retry_pending": false}})
	auditDrain(t, b, c, e.ChatSessionID)
	review8545MessageCount(t, bot, 1)
}

type review8545FailClaim struct{ *review8545Queries }

func (q *review8545FailClaim) AcquireChannelReplyDelivery(context.Context, db.AcquireChannelReplyDeliveryParams) (db.ChannelReplyDelivery, error) {
	return db.ChannelReplyDelivery{}, errors.New("injected ownership write failure")
}

func TestReview8545PostgresClaimFailureMustNotSendUnowned(t *testing.T) {
	bot := &auditBot{}
	o, q, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "original visible reply"))
	o.q = &review8545FailClaim{q}
	o.enqueueTerminalReply(e)

	// Delivery must end on a bounded budget and report the failure rather than
	// send without owning the reply, so this drives it directly: auditDrain
	// treats any reported failure as fatal, and here the report is the point.
	reply := o.terminalSessions[e.ChatSessionID].queue[0]
	var result terminalRequestResult
	settled := false
	for i := 0; i < 25; i++ {
		result = o.sendNextTerminalRequest(context.Background(), reply)
		if result.done {
			settled = true
			break
		}
		if d := result.retryAt.Sub(c.now()); d > 0 {
			c.advance(d)
		}
	}
	if !settled {
		t.Fatal("delivery never gave up; the session's queue stays blocked")
	}
	if result.err == nil {
		t.Fatal("delivery reported success without ever owning the reply")
	}
	review8545MessageCount(t, bot, 1)
}

type review8545PauseStreamRead struct {
	*review8545Queries
	pause   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (q *review8545PauseStreamRead) AcquireChannelReplyDelivery(ctx context.Context, p db.AcquireChannelReplyDeliveryParams) (db.ChannelReplyDelivery, error) {
	row, err := q.Queries.AcquireChannelReplyDelivery(ctx, p)
	if p.Phase == deliveryPhaseStreaming && q.pause.CompareAndSwap(true, false) {
		close(q.entered)
		select {
		case <-q.release:
		case <-ctx.Done():
			return row, ctx.Err()
		}
	}
	return row, err
}

func TestReview8545PostgresStaleStreamCannotOverwriteSettledAnswer(t *testing.T) {
	bot := &auditBot{}
	a, base, c, e := review8545Setup(t, bot)
	a.handleTaskMessage(telegramPartialEvent(e.TaskID, chatDoneContent(e.Payload)))
	c.advance(editInterval + time.Millisecond)
	q := &review8545PauseStreamRead{review8545Queries: base, entered: make(chan struct{}), release: make(chan struct{})}
	q.pause.Store(true)
	a.q = q
	a.leaseTTL = 100 * time.Millisecond
	b := review8545Second(a)
	b.leaseTTL = 100 * time.Millisecond
	done := make(chan struct{})
	go func() { defer close(done); a.handleTaskMessage(telegramPartialEvent(e.TaskID, " stale tail")) }()
	defer func() { close(q.release); <-done }()
	<-q.entered
	// The paused frame still owns the turn; the final answer takes over only
	// once that lease expires.
	time.Sleep(150 * time.Millisecond)
	b.enqueueTerminalReply(e)
	auditDrain(t, b, c, e.ChatSessionID)
	q.release <- struct{}{}
	<-done
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.messages[1] != chatDoneContent(e.Payload) {
		t.Fatalf("settled final answer was overwritten: %q; methods=%v", bot.messages[1], bot.methods)
	}
}

// A 5xx is not proof that nothing was posted. Telegram can accept a
// sendMessage and still fail on the way back, so treating it as a definite
// rejection puts the same answer in the chat twice.
func TestReview8545PostgresServerErrorAfterAcceptIsNotResent(t *testing.T) {
	bot := &auditBot{serverErrorAfterAccept: true}
	o, _, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "accepted then 500"))
	o.enqueueTerminalReply(e)
	auditDrain(t, o, c, e.ChatSessionID)
	review8545MessageCount(t, bot, 1)
}

type review8545FailTurnLookup struct {
	*review8545Queries
	fail atomic.Bool
}

func (q *review8545FailTurnLookup) GetChannelReplyTurn(ctx context.Context, id pgtype.UUID) (db.GetChannelReplyTurnRow, error) {
	if q.fail.Load() {
		return db.GetChannelReplyTurnRow{}, errors.New("injected lineage lookup failure")
	}
	return q.review8545Queries.GetChannelReplyTurn(ctx, id)
}

// A lineage lookup that fails must not fall back to "this task is its own
// turn": that opens a second reply beside the one the previous attempt holds.
func TestReview8545PostgresTurnLookupFailureDoesNotOpenSecondReply(t *testing.T) {
	bot := &auditBot{}
	o, base, c, e := review8545Setup(t, bot)
	retryTask := util.UUIDToString(review8545ID())
	seedRetryChain(t, e.TaskID, retryTask)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "first attempt"))

	q := &review8545FailTurnLookup{review8545Queries: base}
	q.fail.Store(true)
	o.q = q
	o.handleTaskMessage(telegramPartialEvent(retryTask, "retry attempt"))
	review8545MessageCount(t, bot, 1)

	// Once the lookup recovers the retry finishes the reply it inherited.
	q.fail.Store(false)
	retry := e
	retry.TaskID = retryTask
	retry.Payload = protocol.ChatDonePayload{TaskID: retryTask, ChatSessionID: e.ChatSessionID, Content: "retry complete answer"}
	o.enqueueTerminalReply(retry)
	auditDrain(t, o, c, retry.ChatSessionID)
	review8545MessageCount(t, bot, 1)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.messages[1] != "retry complete answer" {
		t.Fatalf("inherited reply not finished by the retry: %q; methods=%v", bot.messages[1], bot.methods)
	}
}

// Once a retry has taken the turn, a late frame from the attempt it superseded
// must not take it back and rewrite what the user is reading.
func TestReview8545PostgresSupersededAttemptCannotRewriteReply(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	retryTask := util.UUIDToString(review8545ID())
	seedRetryChain(t, e.TaskID, retryTask)

	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "first attempt"))
	c.advance(editInterval + time.Millisecond)
	o.handleTaskMessage(telegramPartialEvent(retryTask, "retry attempt"))
	c.advance(editInterval + time.Millisecond)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, " stale tail from the old attempt"))

	review8545MessageCount(t, bot, 1)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if strings.Contains(bot.messages[1], "stale tail") {
		t.Fatalf("superseded attempt rewrote the reply: %q; methods=%v", bot.messages[1], bot.methods)
	}
}

// A reply settled by another replica leaves this one holding a stream and a
// chat-scheduler reference. Nothing else can release them, so the next frame
// for that turn must.
func TestReview8545PostgresSettledElsewhereReleasesLocalState(t *testing.T) {
	bot := &auditBot{}
	a, _, c, e := review8545Setup(t, bot)
	a.handleTaskMessage(telegramPartialEvent(e.TaskID, "streamed on this replica"))
	b := review8545Second(a)
	b.enqueueTerminalReply(e)
	auditDrain(t, b, c, e.ChatSessionID)

	a.mu.Lock()
	held := len(a.streams)
	a.mu.Unlock()
	if held != 1 {
		t.Fatalf("setup expected one local stream, found %d", held)
	}
	c.advance(editInterval + time.Millisecond)
	a.handleTaskMessage(telegramPartialEvent(e.TaskID, " late tail"))

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.streams) != 0 {
		t.Fatal("local stream survived a reply another replica settled")
	}
	for key, schedule := range a.chats {
		if schedule.refs != 0 {
			t.Fatalf("chat schedule %v still referenced after the reply settled", key)
		}
	}
}

// A failure notice must wait for the turn like any other terminal work. Losing
// it because a text frame held the turn at that instant leaves the placeholder
// as the last thing the user ever sees.
func TestReview8545PostgresFailureNoticeWaitsForALiveLease(t *testing.T) {
	bot := &auditBot{}
	a, _, c, e := review8545Setup(t, bot)
	a.leaseTTL = 100 * time.Millisecond
	a.handleTaskMessage(telegramPartialEvent(e.TaskID, "streamed reply"))

	ctx := context.Background()
	target, err := a.resolveTarget(ctx, e, false)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := a.turnFor(ctx, target.taskID)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the turn the way a frame mid-request would.
	if _, status, err := a.acquireDelivery(ctx, target, turn, deliveryPhaseStreaming); err != nil || status != deliveryAcquired {
		t.Fatalf("could not hold the turn: status=%v err=%v", status, err)
	}

	b := review8545Second(a)
	b.leaseTTL = 100 * time.Millisecond
	b.handleTaskFailed(events.Event{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID,
		Payload: map[string]any{"retry_pending": false}})
	time.Sleep(150 * time.Millisecond)
	auditDrain(t, b, c, e.ChatSessionID)

	review8545MessageCount(t, bot, 1)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.messages[1] != taskFailedText {
		t.Fatalf("failure notice was lost: reply still reads %q; methods=%v", bot.messages[1], bot.methods)
	}
}

// A cancellation or empty completion belonging to an attempt the retry chain
// has moved past must not end the turn: the attempt that superseded it is
// still delivering, and its answer would be dropped as already settled.
func TestReview8545PostgresSupersededAttemptCannotCloseTheTurn(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	oldTask := e.TaskID
	retryTask := util.UUIDToString(review8545ID())
	seedRetryChain(t, oldTask, retryTask)

	// The retry takes the turn and starts streaming.
	o.handleTaskMessage(telegramPartialEvent(retryTask, "retry streaming"))

	// The attempt it superseded completes empty, late.
	stale := e
	stale.TaskID = oldTask
	stale.Payload = protocol.ChatDonePayload{TaskID: oldTask, ChatSessionID: e.ChatSessionID}
	o.enqueueTerminalReply(stale)
	auditDrain(t, o, c, stale.ChatSessionID)

	// The retry's answer must still be delivered.
	retry := e
	retry.TaskID = retryTask
	retry.Payload = protocol.ChatDonePayload{TaskID: retryTask, ChatSessionID: e.ChatSessionID, Content: "retry complete answer"}
	o.enqueueTerminalReply(retry)
	auditDrain(t, o, c, retry.ChatSessionID)

	review8545MessageCount(t, bot, 1)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.messages[1] != "retry complete answer" {
		t.Fatalf("superseded attempt closed the turn; reply reads %q, methods=%v", bot.messages[1], bot.methods)
	}
}

// Losing the lease mid-notice must stop the notice, not merely change what it
// does next. One provider call per step, and the next step re-proves the turn.
func TestReview8545PostgresFailureNoticeStopsAfterLosingTheLease(t *testing.T) {
	bot := &auditBot{failFirstEdit: true}
	o, _, c, e := review8545Setup(t, bot)
	o.leaseTTL = 100 * time.Millisecond
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "streamed reply"))
	o.handleTaskFailed(events.Event{TaskID: e.TaskID, ChatSessionID: e.ChatSessionID,
		Payload: map[string]any{"retry_pending": false}})

	o.terminalMu.Lock()
	reply := o.terminalSessions[e.ChatSessionID].queue[0]
	o.terminalMu.Unlock()

	// First step: initialize and take the turn.
	if result := o.sendNextTerminalRequest(context.Background(), reply); result.done {
		t.Fatalf("notice finished before making a request: %+v", result)
	}
	// Second step: the ambiguous edit. It must come back for another step
	// rather than retrying inside this one.
	result := o.sendNextTerminalRequest(context.Background(), reply)
	if result.done {
		t.Fatalf("ambiguous edit settled inside one step: %+v", result)
	}
	callsBefore, _ := func() (int, bool) {
		bot.mu.Lock()
		defer bot.mu.Unlock()
		return len(bot.methods), true
	}()

	// Another replica takes the turn over while this one waits.
	other := review8545Second(o)
	other.leaseTTL = 100 * time.Millisecond
	time.Sleep(150 * time.Millisecond)
	target, err := other.resolveTarget(context.Background(), e, false)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := other.turnFor(context.Background(), target.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, status, err := other.acquireDelivery(context.Background(), target, turn, deliveryPhaseTerminal); err != nil || status != deliveryAcquired {
		t.Fatalf("second replica could not take the turn: status=%v err=%v", status, err)
	}

	c.advance(time.Minute)
	if result := o.sendNextTerminalRequest(context.Background(), reply); !result.done {
		t.Fatalf("delivery continued without the turn: %+v", result)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.methods) != callsBefore {
		t.Fatalf("called Telegram after losing the turn: %v", bot.methods)
	}
}

// Quiet is not abandoned. A run can sit in a tool call far longer than any
// sweep interval, and reclaiming its stream would restart the reply's text
// from whatever arrives next.
func TestReview8545PostgresQuietStreamSurvivesTheSweep(t *testing.T) {
	bot := &auditBot{}
	o, _, c, e := review8545Setup(t, bot)
	o.handleTaskMessage(telegramPartialEvent(e.TaskID, "first half"))

	// Hours of silence, the shape of a long tool call.
	c.advance(3 * time.Hour)
	o.sweepSettledStreams(context.Background())

	o.mu.Lock()
	_, held := o.streams[e.TaskID]
	o.mu.Unlock()
	if !held {
		t.Fatal("sweep reclaimed a live reply that was merely quiet")
	}

	o.handleTaskMessage(telegramPartialEvent(e.TaskID, " second half"))
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.messages[1] != "first half second half" {
		t.Fatalf("quiet stream lost its prefix: %q; methods=%v", bot.messages[1], bot.methods)
	}
}

// The other half of the same rule: once the turn really is over, the sweep
// does release the local state nothing else can.
func TestReview8545PostgresSweepReleasesTurnsSettledElsewhere(t *testing.T) {
	bot := &auditBot{}
	a, _, c, e := review8545Setup(t, bot)
	a.handleTaskMessage(telegramPartialEvent(e.TaskID, "streamed on this replica"))
	b := review8545Second(a)
	b.enqueueTerminalReply(e)
	auditDrain(t, b, c, e.ChatSessionID)

	c.advance(streamSweepGrace + time.Second)
	a.sweepSettledStreams(context.Background())

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.streams) != 0 {
		t.Fatal("sweep left local state for a turn another replica settled")
	}
	for key, schedule := range a.chats {
		if schedule.refs != 0 {
			t.Fatalf("chat schedule %v still referenced after the sweep", key)
		}
	}
}

// The lease only means anything if a call made under it cannot outlive it.
// The shared Bot API client allows 65s — fine for getUpdates long polling,
// fatal for a delivery call, because a request that outlives its lease can
// land after another process has taken the turn over and finished it.
func TestDeliveryCallBudgetFitsTheLease(t *testing.T) {
	if deliveryCallTimeout+deliveryRecordTimeout >= deliveryLeaseTTL {
		t.Fatalf("one call (%s) plus recording its outcome (%s) does not fit inside the lease (%s): "+
			"a delivery can still be talking to Telegram after losing the turn",
			deliveryCallTimeout, deliveryRecordTimeout, deliveryLeaseTTL)
	}
}

// A call that hangs must be cut off by its own budget rather than by the
// client's, so the turn is released close to the lease instead of minutes past
// it. Exercised with a short lease and a bot that never answers.
func TestReview8545PostgresSlowCallIsCutOffInsideTheLease(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	// Release the handler before closing the server: Close waits for handlers
	// to return, and deferred calls run in reverse.
	defer srv.Close()
	defer close(block)

	bot := &auditBot{}
	o, _, _, e := review8545Setup(t, bot)
	o.apiBase = srv.URL
	o.client = srv.Client()
	o.leaseTTL = 600 * time.Millisecond

	done := make(chan time.Duration, 1)
	go func() {
		started := time.Now()
		o.handleTaskMessage(telegramPartialEvent(e.TaskID, "a send that never comes back"))
		done <- time.Since(started)
	}()

	select {
	case took := <-done:
		// The call budget is a third of the lease, so the frame has to give up
		// well before the lease lapses — never at the client's 65s.
		if took >= o.leaseTTL {
			t.Fatalf("send held the turn for %s, past its %s lease", took, o.leaseTTL)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send was still running long after its lease; the call budget is not bounding it")
	}
}
