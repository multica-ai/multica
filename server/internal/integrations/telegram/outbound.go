package telegram

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Outbound delivers an agent's chat reply back to Telegram — the outbound
// half of the round trip, mirroring lark.Patcher / slack.Outbound on the
// shared event bus.
//
// Streaming: Telegram has no stream-update protocol, so the "stream 帧" UX is
// simulated with the platform's canonical pattern — post one placeholder
// message on the first partial, then throttled editMessageText calls as the
// agent's transcript grows (EventTaskMessage text frames), and a final edit /
// send on EventChatDone. Edits are throttled per chat to stay inside
// Telegram's editMessageText rate budget; on a 429 the streamer backs off and
// the final content always lands via the EventChatDone path.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	logger  *slog.Logger
	apiBase string
	client  *http.Client

	// leaseTTL is how long a delivery lease holds a turn. Injectable because
	// expiry is decided by database time, which a test cannot fast-forward:
	// exercising takeover after an owner dies means really waiting one out.
	leaseTTL time.Duration

	mu                 sync.Mutex
	streams            map[string]*streamState // key = task_id
	chats              map[chatScheduleKey]*chatSchedule
	botFallbackBackoff map[string]time.Time
	now                func() time.Time
	wait               func(context.Context, time.Duration) error

	terminalMu               sync.Mutex
	terminalSessions         map[string]*terminalSession
	terminalReady            []string
	terminalRetries          terminalRetryHeap
	terminalWake             chan struct{}
	terminalWork             chan terminalWork
	terminalResults          chan terminalResult
	terminalStopped          bool
	terminalInFlight         int
	queuedTerminalReplyCount int
	queuedTerminalReplyBytes int
	workerOnce               sync.Once
	workerWG                 sync.WaitGroup
	terminalWorkerWG         sync.WaitGroup

	// objects is the deployment's object storage, or nil when there is none.
	// Non-nil is what turns file delivery on (outbound_media.go).
	objects objectStore
	// spawn runs an attachment delivery. A field so a test can run it inline.
	spawn func(func())
}

// outboundQueries is the slice of generated queries the subscriber needs.
// *db.Queries satisfies it.
type outboundQueries interface {
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	// ListAttachmentsByChatMessage is the files the agent bound to its reply,
	// read by the attachment hop (outbound_media.go).
	ListAttachmentsByChatMessage(ctx context.Context, arg db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error)

	// Reply-delivery ownership. The streamed placeholder, the final answer and
	// the failure notice agree on who owns a reply through these and nothing
	// else — see delivery.go.
	GetChannelReplyTurn(ctx context.Context, taskID pgtype.UUID) (db.GetChannelReplyTurnRow, error)
	AcquireChannelReplyDelivery(ctx context.Context, arg db.AcquireChannelReplyDeliveryParams) (db.ChannelReplyDelivery, error)
	GetChannelReplyDelivery(ctx context.Context, turnID pgtype.UUID) (db.ChannelReplyDelivery, error)
	RenewChannelReplyDelivery(ctx context.Context, arg db.RenewChannelReplyDeliveryParams) (int64, error)
	ReleaseChannelReplyDelivery(ctx context.Context, arg db.ReleaseChannelReplyDeliveryParams) (int64, error)
	MarkChannelReplyDeliverySending(ctx context.Context, arg db.MarkChannelReplyDeliverySendingParams) (int64, error)
	RecordChannelReplyDeliveryPlaceholder(ctx context.Context, arg db.RecordChannelReplyDeliveryPlaceholderParams) (int64, error)
	RecordChannelReplyDeliveryChunk(ctx context.Context, arg db.RecordChannelReplyDeliveryChunkParams) (int64, error)
	ResetChannelReplyDeliverySend(ctx context.Context, arg db.ResetChannelReplyDeliverySendParams) (int64, error)
	MarkChannelReplyDeliverySendUnknown(ctx context.Context, turnID pgtype.UUID) (int64, error)
	SettleChannelReplyDelivery(ctx context.Context, arg db.SettleChannelReplyDeliveryParams) (int64, error)
	CloseChannelReplyDeliveryTurn(ctx context.Context, arg db.CloseChannelReplyDeliveryTurnParams) (db.ChannelReplyDelivery, error)
}

// streamState tracks one in-flight streamed reply.
type streamState struct {
	chatID    int64
	threadID  int64
	replyTo   int64
	messageID int64 // placeholder message being edited; 0 until first send
	// turn is the user turn this stream belongs to, cached after the first
	// frame resolves it: an automatic retry's lineage cannot change mid-run.
	turn replyTurn
	// lastFrameAt is when a frame last touched this stream. A reply settled by
	// another replica leaves this one holding local state nothing will come
	// back for, so idle streams are reclaimed rather than kept forever.
	lastFrameAt time.Time
	accumulated string
	schedule    *chatSchedule
}

// chatSchedule serializes Telegram delivery and owns rate-limit state shared
// by every task targeting the same chat. refs and idleSince are protected by
// Outbound.mu; lastEdit and backoffTill are protected by mu.
type chatSchedule struct {
	mu          sync.Mutex
	key         chatScheduleKey
	refs        int
	lastEdit    time.Time
	backoffTill time.Time
	backoffUnix atomic.Int64
	idleSince   time.Time
}

type chatScheduleKey struct {
	botKey string
	chatID int64
}

type terminalSession struct {
	queue        []*terminalReply
	running      bool
	ready        bool
	retryWaiting bool
}

type terminalReply struct {
	event             events.Event
	byteSize          int
	initialized       bool
	target            *replyTarget
	schedule          *chatSchedule
	chunks            []string
	chunkIndex        int
	streamedMessageID int64
	placeholderEdited bool
	fallbackFreshSend bool
	plainTextFallback bool
	plainTextEdit     bool
	kind            terminalKind
	settleReason    string
	turn            replyTurn
	turnResolved    bool
	lease           *deliveryLease
	acquireAttempts int
	editAttempts    int
	cleanupOnce     sync.Once
}

// terminalKind is what a queued item delivers. All three take the turn's lease
// the same way; they differ only in what they do once they hold it.
type terminalKind int

const (
	// terminalKindAnswer delivers the agent's reply.
	terminalKindAnswer terminalKind = iota
	// terminalKindClose ends a turn with no answer — cancelled, or completed
	// empty — so a text frame still in flight cannot reopen it.
	terminalKindClose
	// terminalKindNotice posts the run-failed notice.
	terminalKindNotice
)

type terminalWork struct {
	sessionID string
	reply     *terminalReply
}

type terminalResult struct {
	terminalWork
	done    bool
	retryAt time.Time
	err     error
}

type terminalRetry struct {
	sessionID string
	retryAt   time.Time
}

type terminalRetryHeap []terminalRetry

func (h terminalRetryHeap) Len() int           { return len(h) }
func (h terminalRetryHeap) Less(i, j int) bool { return h[i].retryAt.Before(h[j].retryAt) }
func (h terminalRetryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *terminalRetryHeap) Push(x any)        { *h = append(*h, x.(terminalRetry)) }
func (h *terminalRetryHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// editInterval is the minimum spacing between editMessageText calls per chat.
// Telegram tolerates roughly one edit per second per chat, with a much
// stricter per-group budget (~20 messages/min); 2.5s keeps a long generation
// well inside both without feeling static.
const editInterval = 2500 * time.Millisecond

// placeholderSettleRetry re-checks a stream whose placeholder sendMessage is
// still in flight. A check costs one map lookup — the delivery target is
// resolved once and cached — so the spacing only trades added latency against
// wasted wakeups: 250ms is shorter than a typical Telegram round trip, so a
// settled placeholder is picked up within a check or two, and the wait can
// never outlast the partial's own 10s send context.
const placeholderSettleRetry = 250 * time.Millisecond

// streamSweepGrace is how quiet a streamed reply must be before this process
// asks the database whether its turn is over.
//
// Time alone never decides that. A run's silence budget is hours — a task can
// sit in a tool call or a test run far longer than any timeout worth picking —
// so "no text for a while" says nothing about whether the reply is finished.
// Only the delivery row does: settled, or held by an attempt that superseded
// this one. The grace period exists to keep the sweep cheap, not to judge.
const streamSweepGrace = 2 * time.Minute

// streamSweepInterval paces the sweep. Terminal delivery hands local state back
// on its own; this exists for the turn another replica settled, which cannot
// reach into this process to tidy up. Left alone those entries and their
// chat-scheduler references are held for good, and once enough distinct chats
// accumulate the scheduler refuses new ones — streaming then stops for chats
// with nothing wrong with them.
const streamSweepInterval = time.Minute

// Idle schedules remain briefly reusable so sequential tasks and cancellation
// cannot discard a chat's edit cooldown or Telegram retry_after window. The
// schedule and compressed-fallback maps both have hard capacity limits.
const (
	chatScheduleIdleTTL                = 10 * time.Minute
	maxChatSchedules                   = 1024
	terminalWorkerCount                = 4
	maxBotFallbacks                    = 1024
	chatCapacityRetry                  = time.Second
	maxQueuedTerminalReplies           = 64
	maxQueuedTerminalRepliesPerSession = 8
	maxQueuedTerminalReplyBytes        = 16 << 20

	// terminalEditRetryDelay spaces retries of an edit whose outcome Telegram
	// left ambiguous; maxAmbiguousEditAttempts bounds them, because the
	// terminal queue is per session and a reply that never settles blocks
	// every later answer in the same chat.
	terminalEditRetryDelay   = time.Second
	maxAmbiguousEditAttempts = 3
	// The failure notice runs on the synchronous event bus, so its retries are
	// tighter than the answer's: latency here delays realtime fanout.
	maxNoticeEditAttempts = 2
)

// streamPlaceholder is the first frame's text while the first tokens arrive.
const streamPlaceholder = "…"

// taskFailedText is sent when the agent run fails outright.
const taskFailedText = "❌ The agent run failed. Please try again."

// NewOutbound builds the Telegram outbound subscriber.
func NewOutbound(q outboundQueries, decrypt Decrypter, apiBase string, client *http.Client, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{
		q:                  q,
		decrypt:            decrypt,
		logger:             logger,
		apiBase:            apiBase,
		client:             client,
		leaseTTL:           deliveryLeaseTTL,
		streams:            make(map[string]*streamState),
		chats:              make(map[chatScheduleKey]*chatSchedule),
		botFallbackBackoff: make(map[string]time.Time),
		now:                time.Now,
		wait:               waitForOutbound,
		terminalSessions:   make(map[string]*terminalSession),
		terminalWake:       make(chan struct{}, 1),
		terminalWork:       make(chan terminalWork, terminalWorkerCount),
		terminalResults:    make(chan terminalResult, terminalWorkerCount),
		spawn:              func(f func()) { go f() },
	}
	return o
}

// Register subscribes to the transcript / completion / failure events.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskMessage, o.handleTaskMessage)
	bus.Subscribe(protocol.EventChatDone, o.enqueueTerminalReply)
	bus.Subscribe(protocol.EventTaskFailed, o.handleTaskFailed)
	bus.Subscribe(protocol.EventTaskCancelled, o.handleTaskCancelled)
}

// Start owns the asynchronous terminal-delivery workers. EventChatDone is
// published on the synchronous process bus, so its handler only enqueues work;
// Telegram rate limits and network latency must never delay realtime fanout.
func (o *Outbound) Start(ctx context.Context) {
	o.workerOnce.Do(func() {
		o.workerWG.Add(2)
		go o.runStreamSweeper(ctx)
		o.terminalWorkerWG.Add(terminalWorkerCount)
		for range terminalWorkerCount {
			go o.sendTerminalReplies(ctx)
		}
		go o.dispatchTerminalReplies(ctx)
		o.wakeTerminalDispatcher()
	})
}

// WaitWithTimeout bounds graceful shutdown without coupling the event bus to
// Telegram delivery latency.
func (o *Outbound) WaitWithTimeout(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		o.workerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// handleTaskMessage streams a partial: on each agent text frame, update the
// placeholder message (throttled). Bus delivery is synchronous, so all work
// runs under a tight timeout and never propagates errors.
func (o *Outbound) handleTaskMessage(e events.Event) {
	payload, ok := e.Payload.(protocol.TaskMessagePayload)
	if !ok || payload.Type != "text" || payload.Content == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := o.resolveTarget(ctx, e, true)
	if err != nil || target == nil {
		return
	}

	o.mu.Lock()
	st, exists := o.streams[target.streamKey]
	turn := replyTurn{}
	if exists {
		turn = st.turn
	}
	o.mu.Unlock()
	if !turn.id.Valid {
		resolved, err := o.turnFor(ctx, target.taskID)
		if err != nil {
			// Treating the task as its own turn here would open a second reply
			// beside the one a previous attempt is still holding.
			o.logger.WarnContext(ctx, "telegram outbound: skipping stream frame; reply turn unresolved", "error", err)
			return
		}
		turn = resolved
	}

	// Hold the turn for as long as this frame is talking to Telegram. A frame
	// that checked the state and then sent would still race the final answer
	// taking over mid-request — that gap is what put a second copy of the
	// reply in the chat (GH #8049, #7750).
	lease, status, acquireErr := o.acquireDelivery(ctx, target, turn, deliveryPhaseStreaming)
	if acquireErr != nil {
		o.logger.WarnContext(ctx, "telegram outbound: reply ownership unavailable; skipping stream frame", "error", acquireErr)
		return
	}
	if status == deliveryClosed {
		// The reply is finished — possibly by another replica, which cannot
		// reach into this process to tidy up. Release the local stream and its
		// chat-scheduler reference here, or they are held for good and the
		// schedule table eventually refuses new chats.
		o.clearStream(e)
		return
	}
	if status != deliveryAcquired {
		// Another path owns the turn right now. A stream frame is decoration —
		// the answer still lands — so it steps aside.
		return
	}
	defer o.releaseDelivery(ctx, lease)
	if o.inheritedSend(ctx, lease) {
		// A send this process did not make may or may not be in the chat.
		return
	}

	o.mu.Lock()
	st, exists = o.streams[target.streamKey]
	if !exists {
		schedule := o.retainChatLocked(target.botKey, target.chatID)
		if schedule == nil {
			o.mu.Unlock()
			return
		}
		st = &streamState{
			chatID: target.chatID, threadID: target.threadID, replyTo: target.replyTo,
			turn: turn, schedule: schedule,
		}
		o.streams[target.streamKey] = st
	}
	st.lastFrameAt = o.now()
	// A reply this process did not start — the placeholder came from another
	// replica, or from the attempt this automatic retry inherited — still
	// edits that message instead of opening a second one. Its text is whatever
	// this process has seen; the final answer overwrites it either way.
	if msgID := lease.messageID(); msgID != 0 {
		st.messageID = msgID
	}
	st.accumulated += payload.Content
	snapshot := st.accumulated
	msgID := st.messageID
	o.mu.Unlock()

	o.pushPartial(ctx, target, st, lease, msgID, snapshot)
}

// pushPartial sends the placeholder on the first flush and edits it after. It
// runs under the turn's lease, so the final answer cannot take the reply over
// between the decision made here and the call that acts on it.
func (o *Outbound) pushPartial(ctx context.Context, target *replyTarget, st *streamState, lease *deliveryLease, msgID int64, snapshot string) {
	if !st.schedule.mu.TryLock() {
		return
	}
	defer st.schedule.mu.Unlock()
	now := o.now()
	if o.terminalAvailableAt(st.schedule, target.botKey, now).After(now) {
		return
	}

	api := newBotAPI(o.apiBase, target.botToken, o.client)
	text := snapshot
	if utf16Units(text) > maxMessageUnits {
		// Mid-stream overflow: freeze the streamed message at the cap; the full
		// reply is delivered in chunks by the final EventChatDone send.
		text = chunkMessage(text, maxMessageUnits)[0]
	}
	o.mu.Lock()
	if o.streams[target.streamKey] != st {
		o.mu.Unlock()
		return
	}
	msgID = st.messageID
	o.mu.Unlock()

	if msgID != 0 {
		// Re-prove the turn immediately before the edit. Acquiring it and then
		// stalling — a slow query, a descheduled goroutine — can outlive the
		// lease, and an edit made afterwards overwrites whatever the process
		// that took over has already published as the final answer.
		if !o.renewDelivery(ctx, lease) {
			return
		}
		ctx, cancel := o.callContext(ctx)
		defer cancel()
		err := api.EditMessageText(ctx, editMessageTextParams{
			ChatID:    st.chatID,
			MessageID: msgID,
			Text:      formatHTML(text),
			ParseMode: "HTML",
		})
		if err != nil && !isNotModified(err) {
			o.noteEditFailure(st.schedule, err)
			return
		}
		st.schedule.lastEdit = o.now()
		return
	}

	// Exactly one placeholder per turn, published before the call goes out so
	// that any other process reads "a send is outstanding" rather than
	// "nothing has been sent".
	if !o.claimSend(ctx, lease) {
		return
	}
	ctx, cancel := o.callContext(ctx)
	defer cancel()
	var reply *replyParameters
	if st.replyTo != 0 {
		reply = &replyParameters{MessageID: st.replyTo, AllowSendingWithoutReply: true}
	}
	m, err := api.SendMessage(ctx, sendMessageParams{
		ChatID:          st.chatID,
		Text:            firstNonEmpty(formatHTML(text), streamPlaceholder),
		ParseMode:       "HTML",
		MessageThreadID: st.threadID,
		ReplyParameters: reply,
	})
	if o.recordSend(ctx, lease, true, m.MessageID, 0, err) != deliveryAccepted {
		if err != nil {
			o.noteEditFailure(st.schedule, err)
		}
		return
	}
	st.schedule.lastEdit = o.now()
	o.mu.Lock()
	st.messageID = m.MessageID
	o.mu.Unlock()
}

// noteEditFailure applies the 429-mandated backoff to the stream; other
// failures are logged and the stream simply stops editing (the final content
// still lands via EventChatDone).
func (o *Outbound) noteEditFailure(schedule *chatSchedule, err error) {
	if wait, ok := retryAfter(err); ok {
		schedule.setBackoffTill(o.now().Add(wait))
		return
	}
	o.logger.Warn("telegram outbound: stream edit failed", "error", err)
}

// enqueueTerminalReply only appends to a bounded keyed FIFO. events.Bus is synchronous;
// doing Telegram I/O here would stall SubscribeAll realtime fanout behind
// retry_after waits and slow network requests.
func (o *Outbound) enqueueTerminalReply(e events.Event) {
	// An empty completion still closes the reply: a text frame arriving after
	// it must not reopen a stream nobody is going to finish. Local state goes
	// now — it costs nothing and frees the chat's scheduler slot — while the
	// close itself is a database write and belongs on a worker, not on the
	// synchronous bus.
	if settleOnly := chatDoneContent(e.Payload) == ""; settleOnly {
		o.clearStream(e)
		o.enqueueTerminal(e, terminalKindClose, "empty_reply")
		return
	}
	o.enqueueTerminal(e, terminalKindAnswer, "")
}

// enqueueTerminal appends to a bounded keyed FIFO. events.Bus is synchronous,
// so nothing here may touch Telegram or the database — that work belongs to
// the terminal workers.
func (o *Outbound) enqueueTerminal(e events.Event, kind terminalKind, settleReason string) {
	_, hasTaskID := eventTaskID(e)
	sessionID, sessionErr := util.ParseUUID(e.ChatSessionID)
	if !hasTaskID || sessionErr != nil || !sessionID.Valid {
		o.logger.Error("telegram outbound: terminal reply has invalid identity",
			"task_id", e.TaskID, "chat_session_id", e.ChatSessionID)
		o.clearStream(e)
		return
	}
	sessionKey := e.ChatSessionID
	replyBytes := len(chatDoneContent(e.Payload))
	o.terminalMu.Lock()
	if o.terminalStopped {
		o.terminalMu.Unlock()
		o.clearStream(e)
		return
	}
	session := o.terminalSessions[sessionKey]
	if session == nil {
		session = &terminalSession{}
		o.terminalSessions[sessionKey] = session
	}
	if o.queuedTerminalReplyCount >= maxQueuedTerminalReplies ||
		len(session.queue) >= maxQueuedTerminalRepliesPerSession ||
		replyBytes > maxQueuedTerminalReplyBytes-o.queuedTerminalReplyBytes {
		count := o.queuedTerminalReplyCount
		bytes := o.queuedTerminalReplyBytes
		perSession := len(session.queue)
		if len(session.queue) == 0 {
			delete(o.terminalSessions, sessionKey)
		}
		o.terminalMu.Unlock()
		o.clearStream(e)
		o.logger.Error("telegram outbound: terminal reply queue capacity exceeded",
			"task_id", e.TaskID, "chat_session_id", e.ChatSessionID,
			"queued_count", count, "queued_count_limit", maxQueuedTerminalReplies,
			"session_queued_count", perSession, "session_queued_count_limit", maxQueuedTerminalRepliesPerSession,
			"queued_bytes", bytes, "reply_bytes", replyBytes, "queued_bytes_limit", maxQueuedTerminalReplyBytes)
		return
	}
	session.queue = append(session.queue, &terminalReply{
		event: e, byteSize: replyBytes, kind: kind, settleReason: settleReason,
	})
	o.queuedTerminalReplyCount++
	o.queuedTerminalReplyBytes += replyBytes
	if !session.running && !session.ready && !session.retryWaiting {
		o.queueTerminalReadyLocked(sessionKey, session)
	}
	o.terminalMu.Unlock()
	o.wakeTerminalDispatcher()
}

func (o *Outbound) queueTerminalReadyLocked(sessionID string, session *terminalSession) {
	if session == nil || len(session.queue) == 0 || session.ready || session.running || session.retryWaiting {
		return
	}
	session.ready = true
	o.terminalReady = append(o.terminalReady, sessionID)
}

func (o *Outbound) wakeTerminalDispatcher() {
	select {
	case o.terminalWake <- struct{}{}:
	default:
	}
}

func (o *Outbound) dispatchTerminalReplies(ctx context.Context) {
	defer o.workerWG.Done()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		o.terminalMu.Lock()
		now := o.now()
		for o.terminalRetries.Len() > 0 && !o.terminalRetries[0].retryAt.After(now) {
			retry := heap.Pop(&o.terminalRetries).(terminalRetry)
			session := o.terminalSessions[retry.sessionID]
			if session == nil || !session.retryWaiting {
				continue
			}
			session.retryWaiting = false
			o.queueTerminalReadyLocked(retry.sessionID, session)
		}
		for o.terminalInFlight < terminalWorkerCount && len(o.terminalReady) > 0 {
			sessionID := o.terminalReady[0]
			o.terminalReady[0] = ""
			o.terminalReady = o.terminalReady[1:]
			session := o.terminalSessions[sessionID]
			if session == nil || len(session.queue) == 0 || !session.ready {
				continue
			}
			session.ready = false
			session.running = true
			o.terminalInFlight++
			o.terminalWork <- terminalWork{sessionID: sessionID, reply: session.queue[0]}
		}
		var timerC <-chan time.Time
		if o.terminalRetries.Len() > 0 {
			delay := o.terminalRetries[0].retryAt.Sub(o.now())
			if delay < 0 {
				delay = 0
			}
			timer.Reset(delay)
			timerC = timer.C
		}
		o.terminalMu.Unlock()

		select {
		case <-ctx.Done():
			o.stopTerminalScheduler()
			return
		case result := <-o.terminalResults:
			o.updateQueueAfterTerminalRequest(result)
		case <-o.terminalWake:
		case <-timerC:
		}
		if timerC != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

func (o *Outbound) updateQueueAfterTerminalRequest(result terminalResult) {
	o.terminalMu.Lock()
	defer o.terminalMu.Unlock()
	session := o.terminalSessions[result.sessionID]
	if session == nil || len(session.queue) == 0 || session.queue[0] != result.reply {
		return
	}
	session.running = false
	o.terminalInFlight--
	if result.done {
		if result.err != nil {
			o.logger.Warn("telegram outbound: reply delivery failed",
				"error", result.err, "chat_session_id", result.reply.event.ChatSessionID)
		}
		o.queuedTerminalReplyCount--
		o.queuedTerminalReplyBytes -= result.reply.byteSize
		session.queue[0] = nil
		session.queue = session.queue[1:]
		if len(session.queue) == 0 {
			delete(o.terminalSessions, result.sessionID)
			return
		}
		o.queueTerminalReadyLocked(result.sessionID, session)
		return
	}
	if result.retryAt.After(o.now()) {
		session.retryWaiting = true
		heap.Push(&o.terminalRetries, terminalRetry{sessionID: result.sessionID, retryAt: result.retryAt})
		return
	}
	o.queueTerminalReadyLocked(result.sessionID, session)
}

func (o *Outbound) stopTerminalScheduler() {
	o.terminalMu.Lock()
	o.terminalStopped = true
	close(o.terminalWork)
	o.terminalMu.Unlock()
	o.terminalWorkerWG.Wait()

	o.terminalMu.Lock()
	replies := make([]*terminalReply, 0)
	for _, session := range o.terminalSessions {
		replies = append(replies, session.queue...)
	}
	o.terminalSessions = make(map[string]*terminalSession)
	o.terminalReady = nil
	o.terminalRetries = nil
	o.terminalInFlight = 0
	o.queuedTerminalReplyCount = 0
	o.queuedTerminalReplyBytes = 0
	o.terminalMu.Unlock()
	for _, reply := range replies {
		o.cleanupTerminalReply(reply)
	}
}

func (o *Outbound) sendTerminalReplies(ctx context.Context) {
	defer o.terminalWorkerWG.Done()
	for work := range o.terminalWork {
		request := o.sendNextTerminalRequest(ctx, work.reply)
		if request.done {
			o.cleanupTerminalReply(work.reply)
		}
		result := terminalResult{terminalWork: work, done: request.done, retryAt: request.retryAt, err: request.err}
		select {
		case o.terminalResults <- result:
		case <-ctx.Done():
			return
		}
	}
}

type terminalRequestResult struct {
	done    bool
	retryAt time.Time
	err     error
}

// sendNextTerminalRequest performs at most one Telegram API request. Throttling and
// retry_after return a future retryAt to the keyed dispatcher, leaving the
// fixed worker available for another session.
func (o *Outbound) sendNextTerminalRequest(ctx context.Context, reply *terminalReply) terminalRequestResult {
	if !reply.initialized {
		return o.initializeTerminalReply(ctx, reply)
	}

	// Re-prove ownership before every call. A turn is held across several
	// scheduler rounds — edit pacing, a 429 backoff — and a lease taken
	// minutes ago is not evidence that this process still owns the reply.
	if !o.renewDelivery(ctx, reply.lease) {
		o.logger.WarnContext(ctx, "telegram outbound: lost the reply to another process mid-delivery",
			"turn_id", uuidText(reply.turn.id))
		return terminalRequestResult{done: true}
	}

	schedule := reply.schedule
	if reply.kind == terminalKindNotice {
		return o.deliverFailureNotice(ctx, reply)
	}
	schedule.mu.Lock()
	defer schedule.mu.Unlock()
	now := o.now()
	available := o.terminalAvailableAt(schedule, reply.target.botKey, now)
	if available.After(now) {
		return terminalRequestResult{retryAt: available}
	}
	// Re-prove the turn here, not only at the top: waiting for schedule.mu can
	// take arbitrarily long, and the call below must be authorised by a lease
	// that is still ours at the moment it goes out.
	if !o.renewDelivery(ctx, reply.lease) {
		return terminalRequestResult{done: true}
	}
	ctx, cancel := o.callContext(ctx)
	defer cancel()
	api := newBotAPI(o.apiBase, reply.target.botToken, o.client)

	if reply.streamedMessageID != 0 && !reply.placeholderEdited && !reply.fallbackFreshSend {
		return o.editStreamedReply(ctx, api, reply, schedule)
	}
	return o.sendReplyChunk(ctx, api, reply, schedule)
}

// initializeTerminalReply resolves the destination, takes the turn, and works
// out what is left to deliver.
func (o *Outbound) initializeTerminalReply(ctx context.Context, reply *terminalReply) terminalRequestResult {
	if reply.target == nil {
		target, err := o.resolveTarget(ctx, reply.event, false)
		if err != nil {
			return terminalRequestResult{done: true, err: err}
		}
		if target == nil {
			return terminalRequestResult{done: true}
		}
		reply.target = target
	}
	target := reply.target
	if !reply.turnResolved {
		turn, err := o.turnFor(ctx, target.taskID)
		if err != nil {
			if retryAt, ok := o.retryDeliveryAcquire(reply, maxDeliveryClaimErrorAttempts); ok {
				return terminalRequestResult{retryAt: retryAt}
			}
			// Delivering under a guessed turn would answer beside the reply a
			// previous attempt is holding, so this stops instead.
			return terminalRequestResult{done: true, err: err}
		}
		reply.turn = turn
		reply.turnResolved = true
	}

	if reply.kind == terminalKindClose {
		// Nothing to deliver — a cancelled run, or a completion with no answer.
		// The close still has to land: it is what stops a text frame arriving
		// afterwards from opening a placeholder nothing will ever finish.
		outcome, err := o.closeTurn(ctx, target, reply.turn, reply.settleReason)
		if err != nil || outcome == closeHeld {
			budget := maxDeliveryAcquireAttempts
			if err != nil {
				budget = maxDeliveryClaimErrorAttempts
			}
			if retryAt, ok := o.retryDeliveryAcquire(reply, budget); ok {
				return terminalRequestResult{retryAt: retryAt}
			}
			o.logger.ErrorContext(ctx, "telegram outbound: could not close the turn; a late frame may reopen it",
				"turn_id", uuidText(reply.turn.id), "reason", reply.settleReason, "error", err)
			return terminalRequestResult{done: true}
		}
		o.clearStream(reply.event)
		if outcome == closedNow && reply.settleReason == "empty_reply" {
			// The agent may have said nothing and bound a file instead; the
			// files are then the whole reply. Only the call that ended the
			// turn sends them: a duplicate chat:done, or a completion an
			// automatic retry has superseded, finds the turn closed already.
			o.deliverAttachments(ctx, reply)
		}
		return terminalRequestResult{done: true}
	}

	lease, status, err := o.acquireDelivery(ctx, target, reply.turn, deliveryPhaseTerminal)
	switch {
	case err != nil:
		if retryAt, ok := o.retryDeliveryAcquire(reply, maxDeliveryClaimErrorAttempts); ok {
			return terminalRequestResult{retryAt: retryAt}
		}
		// Never deliver a reply this process does not own: an unowned send is
		// the duplicate this whole mechanism exists to remove. The run's
		// outcome is still in Multica, and the row shows nothing was sent.
		return terminalRequestResult{done: true, err: fmt.Errorf("claim telegram reply delivery: %w", err)}
	case status == deliveryClosed:
		// Already delivered and settled — a second completion event for this
		// turn, or a replay of one.
		o.clearStream(reply.event)
		return terminalRequestResult{done: true}
	case status == deliveryBusy:
		if retryAt, ok := o.retryDeliveryAcquire(reply, maxDeliveryAcquireAttempts); ok {
			return terminalRequestResult{retryAt: retryAt}
		}
		return terminalRequestResult{done: true, err: errors.New("telegram reply stayed owned by another delivery")}
	}
	reply.lease = lease

	if o.inheritedSend(ctx, lease) {
		// A send nobody can account for. Telegram offers no idempotency key,
		// so re-sending may duplicate and not re-sending may truncate; the row
		// keeps which one happened here.
		o.settleDelivery(ctx, lease, "send_result_unknown")
		o.clearStream(reply.event)
		return terminalRequestResult{done: true}
	}

	o.mu.Lock()
	st := o.streams[target.streamKey]
	var schedule *chatSchedule
	if st != nil {
		schedule = st.schedule
	} else {
		schedule = o.retainChatLocked(target.botKey, target.chatID)
		if schedule == nil {
			o.mu.Unlock()
			// Hand the turn back before waiting: holding a lease through an
			// unbounded capacity wait would block every other path on it, and
			// this reply's own next attempt as well. Re-resolve too, so an
			// installation revoked during the wait stops the delivery.
			o.releaseDelivery(ctx, lease)
			reply.lease = nil
			reply.target = nil
			return terminalRequestResult{retryAt: o.now().Add(chatCapacityRetry)}
		}
	}
	delete(o.streams, target.streamKey)
	o.mu.Unlock()

	reply.initialized = true
	reply.schedule = schedule
	reply.streamedMessageID = lease.messageID()
	if reply.kind == terminalKindNotice {
		return terminalRequestResult{retryAt: o.now()}
	}
	reply.chunks = chunkMessage(chatDoneContent(reply.event.Payload), maxMessageUnits)
	if len(reply.chunks) == 0 {
		o.settleDelivery(ctx, lease, "empty_reply")
		return terminalRequestResult{done: true}
	}
	// Resume after the last part that landed. A placeholder is not a part: it
	// gives the turn something to edit and delivers none of the final answer.
	if sent := lease.chunksSent(); sent > 0 {
		reply.placeholderEdited = reply.streamedMessageID != 0
		reply.chunkIndex = min(sent, len(reply.chunks))
		if reply.chunkIndex == len(reply.chunks) {
			o.settleDelivery(ctx, lease, "delivered")
			return terminalRequestResult{done: true}
		}
	}
	return terminalRequestResult{retryAt: o.now()}
}

// retryDeliveryAcquire spaces attempts on a turn this process could not take,
// and gives up rather than holding the session's queue forever. Waiting out a
// live owner is worth a lease's length; a failing ownership write is not, so
// the two have different budgets.
func (o *Outbound) retryDeliveryAcquire(reply *terminalReply, budget int) (time.Time, bool) {
	reply.acquireAttempts++
	if reply.acquireAttempts >= budget {
		return time.Time{}, false
	}
	return o.now().Add(deliveryBusyRetry), true
}

// editStreamedReply turns the placeholder into the final answer's first part.
// Every branch keeps operating on that message: posting the answer again
// beside one that may well have been edited is the duplicate (GH #8049).
func (o *Outbound) editStreamedReply(ctx context.Context, api *botAPI, reply *terminalReply, schedule *chatSchedule) terminalRequestResult {
	params := editMessageTextParams{
		ChatID: reply.target.chatID, MessageID: reply.streamedMessageID,
		Text: formatHTML(reply.chunks[0]), ParseMode: "HTML",
	}
	if reply.plainTextEdit {
		params.Text = reply.chunks[0]
		params.ParseMode = ""
	}
	err := api.EditMessageText(ctx, params)
	if retry, ok := retryAfter(err); ok {
		retryAt := o.now().Add(retry)
		schedule.setBackoffTill(retryAt)
		return terminalRequestResult{retryAt: retryAt}
	}
	switch {
	case err == nil || isNotModified(err):
	case isHTMLParseError(err) && !reply.plainTextEdit:
		// Telegram refused the markup, not the message: same target, plain.
		reply.plainTextEdit = true
		return terminalRequestResult{retryAt: o.now()}
	case isEditTargetMissing(err):
		// Confirmed gone — the only case where a fresh message is not a second
		// copy of one the user can already see.
		reply.fallbackFreshSend = true
		reply.chunkIndex = 0
		return terminalRequestResult{retryAt: o.now()}
	case isPermanentEditRejection(err):
		// Telegram will keep refusing. Stop rather than re-send or spin: the
		// queue is per session, and a reply that never settles blocks every
		// later answer in the same chat.
		o.logger.WarnContext(ctx, "telegram outbound: final edit permanently rejected; reply left as streamed",
			"turn_id", uuidText(reply.turn.id), "error", err)
		o.settleDelivery(ctx, reply.lease, "edit_rejected")
		return terminalRequestResult{done: true}
	default:
		// Ambiguous: the edit may have applied. Retry the same message on a
		// budget, then give up for the same reason as above.
		reply.editAttempts++
		if reply.editAttempts >= maxAmbiguousEditAttempts {
			o.logger.WarnContext(ctx, "telegram outbound: final edit kept failing; reply left as streamed",
				"turn_id", uuidText(reply.turn.id), "error", err)
			o.settleDelivery(ctx, reply.lease, "edit_failed")
			return terminalRequestResult{done: true}
		}
		return terminalRequestResult{retryAt: o.now().Add(terminalEditRetryDelay)}
	}
	reply.placeholderEdited = true
	reply.chunkIndex = 1
	schedule.lastEdit = o.now()
	schedule.setBackoffTill(time.Time{})
	o.recordSend(ctx, reply.lease, false, reply.streamedMessageID, reply.chunkIndex, nil)
	if reply.chunkIndex == len(reply.chunks) {
		o.settleDelivery(ctx, reply.lease, "delivered")
		o.deliverAttachments(ctx, reply)
		return terminalRequestResult{done: true}
	}
	return terminalRequestResult{retryAt: schedule.lastEdit.Add(editInterval)}
}

// sendReplyChunk posts one part of the final answer. The send is published
// before it is made and its outcome recorded after, so a delivery resumed in
// another process continues after this part rather than repeating it — and a
// part whose result was lost stops delivery instead of being sent twice.
func (o *Outbound) sendReplyChunk(ctx context.Context, api *botAPI, reply *terminalReply, schedule *chatSchedule) terminalRequestResult {
	chunk := reply.chunks[reply.chunkIndex]
	params := sendMessageParams{
		ChatID: reply.target.chatID, Text: formatHTML(chunk), ParseMode: "HTML",
		MessageThreadID: reply.target.threadID,
	}
	if reply.chunkIndex == 0 {
		params.ReplyParameters = optionalReplyParameters(reply.target.replyTo)
	}
	if reply.plainTextFallback {
		params.Text = chunk
		params.ParseMode = ""
	}
	if !o.claimSend(ctx, reply.lease) {
		// Either the turn moved on, or an earlier send is still unaccounted
		// for. Neither is a reason to put another message in the chat.
		return terminalRequestResult{done: true}
	}
	sent, err := api.SendMessage(ctx, params)
	if o.recordSend(ctx, reply.lease, false, sent.MessageID, reply.chunkIndex+1, err) == deliveryUnknown {
		// Report it rather than ending quietly: nobody can tell from the chat
		// whether this part arrived, so the failure has to be visible to the
		// operator as well as recorded on the row.
		o.settleDelivery(ctx, reply.lease, "send_result_unknown")
		return terminalRequestResult{done: true, err: fmt.Errorf("send final chunk: outcome unknown: %w", err)}
	}
	if retry, ok := retryAfter(err); ok {
		retryAt := o.now().Add(retry)
		schedule.setBackoffTill(retryAt)
		return terminalRequestResult{retryAt: retryAt}
	}
	if err != nil && !reply.plainTextFallback && isHTMLParseError(err) {
		reply.plainTextFallback = true
		return terminalRequestResult{retryAt: o.now()}
	}
	if err != nil {
		o.settleDelivery(ctx, reply.lease, "send_failed")
		return terminalRequestResult{done: true, err: fmt.Errorf("send final chunk: %w", err)}
	}
	reply.chunkIndex++
	reply.plainTextFallback = false
	schedule.lastEdit = o.now()
	schedule.setBackoffTill(time.Time{})
	if reply.chunkIndex == len(reply.chunks) {
		o.settleDelivery(ctx, reply.lease, "delivered")
		// The text is in the chat; the files the agent bound to it follow as
		// their own messages, off this worker.
		o.deliverAttachments(ctx, reply)
		return terminalRequestResult{done: true}
	}
	return terminalRequestResult{retryAt: schedule.lastEdit.Add(editInterval)}
}

func (o *Outbound) cleanupTerminalReply(reply *terminalReply) {
	reply.cleanupOnce.Do(func() {
		if reply.lease != nil {
			// Settling already cleared the lease; this covers every path that
			// ended without settling, so the turn is not held until it expires.
			o.releaseDelivery(context.Background(), reply.lease)
		}
		if reply.initialized && reply.schedule != nil {
			o.releaseChat(reply.schedule, reply.target.chatID)
			return
		}
		o.clearStream(reply.event)
	})
}

// handleTaskFailed hands the run-failed notice to the terminal queue, which
// owns the turn's lease and its retries. Posting it inline meant a notice was
// simply dropped whenever another path — a text frame still talking to
// Telegram, often on another replica — held the turn at that instant, leaving
// the placeholder as the last thing the user ever saw.
func (o *Outbound) handleTaskFailed(e events.Event) {
	if taskFailureRetryPending(e.Payload) {
		// The platform runs this turn again under a new task id. Nothing to
		// close and nothing to say: the turn keeps its placeholder, and the
		// retry resolves to the same turn and finishes it rather than posting
		// its answer beside an abandoned one.
		o.clearStream(e)
		return
	}
	o.enqueueTerminal(e, terminalKindNotice, "failure_notice")
}

// deliverFailureNotice posts the run-failed notice, one Telegram call per
// step. Deliberately the same shape as the answer's delivery: make at most one
// request, hand back a retryAt, and let the next step re-prove the lease
// first. Looping inside a step — retrying an edit, or waiting out a
// retry_after — means the call after the wait can land well past the lease, on
// a turn another replica has since taken over and possibly finished.
func (o *Outbound) deliverFailureNotice(ctx context.Context, reply *terminalReply) terminalRequestResult {
	lease := reply.lease
	target := reply.target
	if o.inheritedSend(ctx, lease) {
		// A send nobody can account for: the user may already be looking at a
		// message about this turn.
		o.settleDelivery(ctx, lease, "send_result_unknown")
		return terminalRequestResult{done: true}
	}

	schedule := reply.schedule
	schedule.mu.Lock()
	defer schedule.mu.Unlock()
	now := o.now()
	if available := o.terminalAvailableAt(schedule, target.botKey, now); available.After(now) {
		return terminalRequestResult{retryAt: available}
	}
	if !o.renewDelivery(ctx, lease) {
		return terminalRequestResult{done: true}
	}
	ctx, cancel := o.callContext(ctx)
	defer cancel()
	api := newBotAPI(o.apiBase, target.botToken, o.client)

	if messageID := lease.messageID(); messageID != 0 && !reply.fallbackFreshSend {
		return o.editNoticeOntoPlaceholder(ctx, api, reply, schedule, messageID)
	}
	if !o.claimSend(ctx, lease) {
		return terminalRequestResult{done: true}
	}
	sent, sendErr := api.SendMessage(ctx, sendMessageParams{
		ChatID: target.chatID, Text: taskFailedText, MessageThreadID: target.threadID,
		ReplyParameters: optionalReplyParameters(target.replyTo),
	})
	if o.recordSend(ctx, lease, true, sent.MessageID, 0, sendErr) == deliveryUnknown {
		o.settleDelivery(ctx, lease, "send_result_unknown")
		return terminalRequestResult{done: true, err: fmt.Errorf("failure notice: outcome unknown: %w", sendErr)}
	}
	if retry, ok := retryAfter(sendErr); ok {
		retryAt := o.now().Add(retry)
		schedule.setBackoffTill(retryAt)
		return terminalRequestResult{retryAt: retryAt}
	}
	schedule.lastEdit = o.now()
	schedule.setBackoffTill(time.Time{})
	o.settleDelivery(ctx, lease, "failure_notice")
	if sendErr != nil {
		return terminalRequestResult{done: true, err: fmt.Errorf("failure notice: %w", sendErr)}
	}
	return terminalRequestResult{done: true}
}

// editNoticeOntoPlaceholder turns the streamed placeholder into the notice,
// with the same failure classification the final answer uses: only a target
// Telegram confirms is gone justifies posting a second message.
func (o *Outbound) editNoticeOntoPlaceholder(ctx context.Context, api *botAPI, reply *terminalReply, schedule *chatSchedule, messageID int64) terminalRequestResult {
	err := api.EditMessageText(ctx, editMessageTextParams{
		ChatID: reply.target.chatID, MessageID: messageID, Text: taskFailedText,
	})
	if retry, ok := retryAfter(err); ok {
		retryAt := o.now().Add(retry)
		schedule.setBackoffTill(retryAt)
		return terminalRequestResult{retryAt: retryAt}
	}
	switch {
	case err == nil || isNotModified(err):
		schedule.lastEdit = o.now()
		schedule.setBackoffTill(time.Time{})
		o.settleDelivery(ctx, reply.lease, "failure_notice")
		return terminalRequestResult{done: true}
	case isEditTargetMissing(err):
		reply.fallbackFreshSend = true
		return terminalRequestResult{retryAt: o.now()}
	case isPermanentEditRejection(err):
		// Keep the placeholder rather than duplicating the turn: the run's
		// outcome is still visible in Multica.
		o.logger.WarnContext(ctx, "telegram outbound: failure notice edit permanently rejected",
			"turn_id", uuidText(reply.turn.id), "error", err)
		o.settleDelivery(ctx, reply.lease, "edit_rejected")
		return terminalRequestResult{done: true}
	default:
		reply.editAttempts++
		if reply.editAttempts >= maxNoticeEditAttempts {
			o.logger.WarnContext(ctx, "telegram outbound: failure notice edit kept failing",
				"turn_id", uuidText(reply.turn.id), "error", err)
			o.settleDelivery(ctx, reply.lease, "edit_failed")
			return terminalRequestResult{done: true}
		}
		return terminalRequestResult{retryAt: o.now().Add(terminalEditRetryDelay)}
	}
}

// handleTaskCancelled leaves the partial Telegram message the user can already
// see — cancellation has no final answer, and replacing visible content with a
// synthetic notice is worse than keeping it — but closes the reply so a text
// frame still in flight cannot open a second message beside it.
func (o *Outbound) handleTaskCancelled(e events.Event) {
	o.clearStream(e)
	o.enqueueTerminal(e, terminalKindClose, "cancelled")
}

func taskFailureRetryPending(payload any) bool {
	fields, ok := payload.(map[string]any)
	if !ok {
		return false
	}
	retryPending, _ := fields["retry_pending"].(bool)
	return retryPending
}

func (o *Outbound) clearStream(e events.Event) {
	taskID, ok := eventTaskID(e)
	if !ok {
		return
	}
	key := util.UUIDToString(taskID)
	o.mu.Lock()
	st := o.streams[key]
	delete(o.streams, key)
	if st != nil {
		o.releaseChatLocked(st.schedule, st.chatID)
	}
	o.mu.Unlock()
}

func (o *Outbound) retainChatLocked(botKey string, chatID int64) *chatSchedule {
	now := o.now()
	o.pruneIdleChatsLocked(now)
	key := chatScheduleKey{botKey: botKey, chatID: chatID}
	schedule := o.chats[key]
	if schedule != nil && schedule.refs == 0 && !schedule.idleSince.IsZero() &&
		now.Sub(schedule.idleSince) >= chatScheduleIdleTTL && !schedule.hasActiveBackoff(now) {
		delete(o.chats, key)
		schedule = nil
	}
	if schedule == nil {
		if !o.makeChatScheduleRoomLocked(now) {
			return nil
		}
		schedule = &chatSchedule{key: key}
		o.chats[key] = schedule
	}
	schedule.refs++
	schedule.idleSince = time.Time{}
	return schedule
}

func (o *Outbound) releaseChat(schedule *chatSchedule, chatID int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.releaseChatLocked(schedule, chatID)
}

func (o *Outbound) releaseChatLocked(schedule *chatSchedule, chatID int64) {
	if schedule == nil {
		return
	}
	schedule.refs--
	if schedule.refs == 0 && o.chats[schedule.key] == schedule {
		schedule.idleSince = o.now()
		o.pruneIdleChatsLocked(schedule.idleSince)
	}
}

// sweepSettledStreams releases local state for turns that are over, and only
// for those. A stream is a candidate once it has been quiet for a while, but
// what releases it is the delivery row saying the turn is settled or that a
// later attempt has taken it over — never the quiet itself, which is normal
// for a run that is busy rather than finished.
func (o *Outbound) sweepSettledStreams(ctx context.Context) {
	type candidate struct {
		key    string
		stream *streamState
		turn   replyTurn
		seenAt time.Time
	}
	now := o.now()
	var candidates []candidate
	o.mu.Lock()
	for key, st := range o.streams {
		if !st.turn.id.Valid || st.lastFrameAt.IsZero() || now.Sub(st.lastFrameAt) < streamSweepGrace {
			continue
		}
		candidates = append(candidates, candidate{key: key, stream: st, turn: st.turn, seenAt: st.lastFrameAt})
	}
	o.mu.Unlock()

	for _, c := range candidates {
		row, err := o.q.GetChannelReplyDelivery(ctx, c.turn.id)
		if err != nil {
			// No row, or the lookup failed: no evidence the turn is over, so
			// the stream stays. Reclaiming on a failed read would take a live
			// reply away from a run that is still producing text.
			continue
		}
		superseded := row.AttemptDepth > c.turn.depth
		if row.Phase != deliveryPhaseSettled && !superseded {
			continue
		}
		o.mu.Lock()
		// Re-check under the lock: a frame may have arrived while we were
		// asking, which both revives the stream and makes the answer stale.
		if current, ok := o.streams[c.key]; ok && current == c.stream && current.lastFrameAt.Equal(c.seenAt) {
			delete(o.streams, c.key)
			o.releaseChatLocked(current.schedule, current.chatID)
			o.logger.Info("telegram outbound: released local state for a turn that ended elsewhere",
				"task_id", c.key, "settled_reason", row.SettledReason, "superseded", superseded)
		}
		o.mu.Unlock()
	}
}

// runStreamSweeper reclaims local state for turns finished by another replica.
func (o *Outbound) runStreamSweeper(ctx context.Context) {
	defer o.workerWG.Done()
	ticker := time.NewTicker(streamSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.sweepSettledStreams(ctx)
		}
	}
}

func (o *Outbound) pruneIdleChatsLocked(now time.Time) {
	o.pruneBotFallbacksLocked(now)
	for key, schedule := range o.chats {
		if schedule.refs != 0 || schedule.idleSince.IsZero() {
			continue
		}
		if now.Sub(schedule.idleSince) >= chatScheduleIdleTTL && !schedule.hasActiveBackoff(now) {
			delete(o.chats, key)
		}
	}
}

// makeChatScheduleRoomLocked enforces a hard cap over the entire schedule
// map. Inactive schedules are evicted first. If every idle candidate has an
// active retry_after, its exact deadline is conservatively merged into the
// installation fallback before eviction. A fully in-use cache refuses a new
// entry; terminal jobs retry later instead of bypassing rate-limit state.
func (o *Outbound) makeChatScheduleRoomLocked(now time.Time) bool {
	if len(o.chats) < maxChatSchedules {
		return true
	}
	var inactiveKey chatScheduleKey
	var inactive *chatSchedule
	for key, schedule := range o.chats {
		if schedule.refs != 0 || schedule.idleSince.IsZero() ||
			now.Sub(schedule.idleSince) < editInterval || schedule.hasActiveBackoff(now) {
			continue
		}
		if inactive == nil || schedule.idleSince.Before(inactive.idleSince) {
			inactiveKey, inactive = key, schedule
		}
	}
	if inactive != nil {
		delete(o.chats, inactiveKey)
		return true
	}

	var activeKey chatScheduleKey
	var active *chatSchedule
	for key, schedule := range o.chats {
		if schedule.refs != 0 || schedule.idleSince.IsZero() ||
			now.Sub(schedule.idleSince) < editInterval || !schedule.hasActiveBackoff(now) {
			continue
		}
		if !o.canMergeBotFallbackLocked(schedule.key.botKey, now) {
			continue
		}
		if active == nil || schedule.idleSince.Before(active.idleSince) {
			activeKey, active = key, schedule
		}
	}
	if active == nil {
		return false
	}
	o.mergeBotFallbackLocked(active.key.botKey, active.backoffSnapshot(), now)
	delete(o.chats, activeKey)
	return true
}

func (o *Outbound) canMergeBotFallbackLocked(botKey string, now time.Time) bool {
	o.pruneBotFallbacksLocked(now)
	_, exists := o.botFallbackBackoff[botKey]
	return exists || len(o.botFallbackBackoff) < maxBotFallbacks
}

func (o *Outbound) mergeBotFallbackLocked(botKey string, until, now time.Time) {
	if !until.After(now) {
		return
	}
	if current := o.botFallbackBackoff[botKey]; until.After(current) {
		o.botFallbackBackoff[botKey] = until
	}
}

func (o *Outbound) pruneBotFallbacksLocked(now time.Time) {
	for botKey, until := range o.botFallbackBackoff {
		if !until.After(now) {
			delete(o.botFallbackBackoff, botKey)
		}
	}
}

func (o *Outbound) botFallbackTill(botKey string, now time.Time) time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneBotFallbacksLocked(now)
	return o.botFallbackBackoff[botKey]
}

func (o *Outbound) terminalAvailableAt(schedule *chatSchedule, botKey string, now time.Time) time.Time {
	available := schedule.backoffTill
	if editAvailable := schedule.lastEdit.Add(editInterval); editAvailable.After(available) {
		available = editAvailable
	}
	if fallback := o.botFallbackTill(botKey, now); fallback.After(available) {
		available = fallback
	}
	return available
}

// setBackoffTill is called while schedule.mu is held. backoffUnix gives cache
// pruning a lock-free snapshot, preserving the global Outbound.mu -> no
// schedule.mu lock order and avoiding the inverse of delivery's
// schedule.mu -> Outbound.mu path.
func (s *chatSchedule) setBackoffTill(t time.Time) {
	s.backoffTill = t
	if t.IsZero() {
		s.backoffUnix.Store(0)
		return
	}
	s.backoffUnix.Store(t.UnixNano())
}

func (s *chatSchedule) hasActiveBackoff(now time.Time) bool {
	return s.backoffUnix.Load() > now.UnixNano()
}

func (s *chatSchedule) backoffSnapshot() time.Time {
	nanos := s.backoffUnix.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// runScheduled waits for the chat's edit interval and any Telegram 429
// retry_after window. Terminal delivery retries 429 responses until the event
// context expires; other errors remain non-retriable to avoid duplicates.
func (o *Outbound) runScheduled(ctx context.Context, schedule *chatSchedule, operation func() error) error {
	for {
		now := o.now()
		available := o.terminalAvailableAt(schedule, schedule.key.botKey, now)
		if delay := available.Sub(now); delay > 0 {
			if err := o.wait(ctx, delay); err != nil {
				return err
			}
		}
		err := operation()
		if retry, ok := retryAfter(err); ok {
			schedule.setBackoffTill(o.now().Add(retry))
			continue
		}
		if err == nil {
			schedule.lastEdit = o.now()
			schedule.setBackoffTill(time.Time{})
		}
		return err
	}
}

func waitForOutbound(ctx context.Context, delay time.Duration) error {
	if sleepCtx(ctx, delay) {
		return nil
	}
	return ctx.Err()
}

// replyTarget is the resolved destination for one event.
type replyTarget struct {
	streamKey string
	botKey    string
	chatID    int64
	threadID  int64
	replyTo   int64
	botToken  string

	// Identity of the reply this target belongs to, for the ownership row.
	taskID         pgtype.UUID
	bindingID      pgtype.UUID
	installationID pgtype.UUID
	channelChatID  string
}

// resolveTarget maps an event's immutable task delivery snapshot to Telegram
// credentials. A missing snapshot means the task came from Web/Desktop/Mobile
// and must not reach an external conversation, even if its Chat once had a
// Telegram route.
func (o *Outbound) resolveTarget(ctx context.Context, e events.Event, _ bool) (*replyTarget, error) {
	taskID, hasTaskID := eventTaskID(e)
	if !hasTaskID {
		return nil, nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup telegram task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeTelegram) {
		return nil, nil
	}
	binding := telegramBindingFromTaskDelivery(delivery)
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeTelegram),
	})
	if err != nil {
		return nil, fmt.Errorf("load telegram installation: %w", err)
	}
	if inst.Status != "active" {
		return nil, nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return nil, fmt.Errorf("decode telegram credentials: %w", err)
	}
	chatID, threadID, replyTo := outboundTarget(binding)
	return &replyTarget{
		streamKey:      util.UUIDToString(taskID),
		botKey:         util.UUIDToString(inst.ID),
		chatID:         chatID,
		threadID:       threadID,
		replyTo:        replyTo,
		botToken:       creds.BotToken,
		taskID:         taskID,
		bindingID:      delivery.BindingID,
		installationID: inst.ID,
		channelChatID:  delivery.ChannelChatID,
	}, nil
}

func telegramBindingFromTaskDelivery(delivery db.ChannelTaskDelivery) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		ID: delivery.BindingID, InstallationID: delivery.InstallationID,
		ChannelType: delivery.ChannelType, ChannelChatID: delivery.ChannelChatID,
		ChatType: delivery.ChatType, LastMessageID: delivery.ChannelMessageID,
		LastThreadID: delivery.ChannelThreadID, RouteRevision: delivery.RouteRevision,
		Config: delivery.Config,
	}
}

// outboundTarget recovers the numeric chat id (from the binding config when
// the binding key is a composite "chat:thread") and the reply thread.
func outboundTarget(b db.ChannelChatSessionBinding) (chatID, threadID, replyTo int64) {
	raw := b.ChannelChatID
	if len(b.Config) > 0 {
		var cfg telegramBindingConfig
		if err := json.Unmarshal(b.Config, &cfg); err == nil && cfg.ChatID != "" {
			raw = cfg.ChatID
		}
	}
	chatID, _ = strconv.ParseInt(raw, 10, 64)
	if b.LastThreadID.Valid {
		threadID, _ = strconv.ParseInt(b.LastThreadID.String, 10, 64)
	}
	if b.LastMessageID.Valid {
		replyTo = parseMessageRef(b.LastMessageID.String)
	}
	return chatID, threadID, replyTo
}

func optionalReplyParameters(messageID int64) *replyParameters {
	if messageID == 0 {
		return nil
	}
	return &replyParameters{MessageID: messageID, AllowSendingWithoutReply: true}
}

// eventTaskID extracts the task id from the event envelope or payload.
func eventTaskID(e events.Event) (pgtype.UUID, bool) {
	raw := e.TaskID
	if raw == "" {
		switch p := e.Payload.(type) {
		case protocol.ChatDonePayload:
			raw = p.TaskID
		case map[string]any:
			raw, _ = p["task_id"].(string)
		}
	}
	id, err := util.ParseUUID(raw)
	return id, err == nil && id.Valid
}

// chatDoneContent extracts the reply text from an EventChatDone payload.
func chatDoneContent(payload any) string {
	switch p := payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

// isNotModified reports Telegram's "message is not modified" edit error, which
// is benign (identical snapshot).
// isEditTargetMissing is Telegram confirming the message is gone. It is the
// only edit failure that justifies posting a fresh message: every other one
// may leave the original in the chat, and posting beside it is the duplicate.
func isEditTargetMissing(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Code == http.StatusBadRequest &&
		containsFold(ae.Description, "message to edit not found")
}

// isPermanentEditRejection reports a rejection that retrying cannot fix — the
// bot was blocked, lost its rights, or the message is no longer editable.
// Callers must have ruled out the recoverable 400s (markup, missing target)
// first, since those share the status code.
func isPermanentEditRejection(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

func isNotModified(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Code == http.StatusBadRequest &&
		containsFold(ae.Description, "message is not modified")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
