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
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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
}

// outboundQueries is the slice of generated queries the subscriber needs.
// *db.Queries satisfies it.
type outboundQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// streamState tracks one in-flight streamed reply.
type streamState struct {
	chatID      int64
	threadID    int64
	replyTo     int64
	messageID   int64 // placeholder message being edited; 0 until first send
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
	cleanupOnce       sync.Once
}

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
)

// streamPlaceholder is the first frame's text while the first tokens arrive.
const streamPlaceholder = "…"

// taskFailedText is sent when the agent run fails outright.
const taskFailedText = "❌ 智能体处理失败，请稍后重试。"

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
		streams:            make(map[string]*streamState),
		chats:              make(map[chatScheduleKey]*chatSchedule),
		botFallbackBackoff: make(map[string]time.Time),
		now:                time.Now,
		wait:               waitForOutbound,
		terminalSessions:   make(map[string]*terminalSession),
		terminalWake:       make(chan struct{}, 1),
		terminalWork:       make(chan terminalWork, terminalWorkerCount),
		terminalResults:    make(chan terminalResult, terminalWorkerCount),
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
		o.workerWG.Add(1)
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
	if !exists {
		schedule := o.retainChatLocked(target.botKey, target.chatID)
		if schedule == nil {
			o.mu.Unlock()
			return
		}
		st = &streamState{
			chatID: target.chatID, threadID: target.threadID, replyTo: target.replyTo,
			schedule: schedule,
		}
		o.streams[target.streamKey] = st
	}
	st.accumulated += payload.Content
	snapshot := st.accumulated
	msgID := st.messageID
	o.mu.Unlock()

	o.pushPartial(ctx, target, st, msgID, snapshot)
}

// pushPartial sends the placeholder on the first flush and edits it after.
func (o *Outbound) pushPartial(ctx context.Context, target *replyTarget, st *streamState, msgID int64, snapshot string) {
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
	if msgID == 0 {
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
		if err != nil {
			o.noteEditFailure(st.schedule, err)
			return
		}
		st.schedule.lastEdit = o.now()
		o.mu.Lock()
		st.messageID = m.MessageID
		o.mu.Unlock()
		return
	}
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
	_, hasTaskID := eventTaskID(e)
	sessionID, sessionErr := util.ParseUUID(e.ChatSessionID)
	if !hasTaskID || sessionErr != nil || !sessionID.Valid {
		o.logger.Error("telegram outbound: terminal reply has invalid identity",
			"task_id", e.TaskID, "chat_session_id", e.ChatSessionID)
		o.clearStream(e)
		return
	}
	content := chatDoneContent(e.Payload)
	if content == "" {
		o.clearStream(e)
		return
	}
	sessionKey := e.ChatSessionID
	replyBytes := len(content)
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
	session.queue = append(session.queue, &terminalReply{event: e, byteSize: replyBytes})
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
		target, err := o.resolveTarget(ctx, reply.event, false)
		if err != nil {
			return terminalRequestResult{done: true, err: err}
		}
		if target == nil {
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
				return terminalRequestResult{retryAt: o.now().Add(chatCapacityRetry)}
			}
		}
		delete(o.streams, target.streamKey)
		o.mu.Unlock()

		reply.initialized = true
		reply.target = target
		reply.schedule = schedule
		reply.chunks = chunkMessage(chatDoneContent(reply.event.Payload), maxMessageUnits)
		if st != nil {
			reply.streamedMessageID = st.messageID
		}
		if len(reply.chunks) == 0 {
			return terminalRequestResult{done: true}
		}
		return terminalRequestResult{retryAt: o.now()}
	}

	schedule := reply.schedule
	schedule.mu.Lock()
	defer schedule.mu.Unlock()
	now := o.now()
	available := o.terminalAvailableAt(schedule, reply.target.botKey, now)
	if available.After(now) {
		return terminalRequestResult{retryAt: available}
	}
	api := newBotAPI(o.apiBase, reply.target.botToken, o.client)

	if reply.streamedMessageID != 0 && !reply.placeholderEdited && !reply.fallbackFreshSend {
		err := api.EditMessageText(ctx, editMessageTextParams{
			ChatID: reply.target.chatID, MessageID: reply.streamedMessageID,
			Text: formatHTML(reply.chunks[0]), ParseMode: "HTML",
		})
		if retry, ok := retryAfter(err); ok {
			retryAt := o.now().Add(retry)
			schedule.setBackoffTill(retryAt)
			return terminalRequestResult{retryAt: retryAt}
		}
		if err != nil && !isNotModified(err) {
			reply.fallbackFreshSend = true
			reply.chunkIndex = 0
			return terminalRequestResult{retryAt: o.now()}
		}
		reply.placeholderEdited = true
		reply.chunkIndex = 1
		schedule.lastEdit = o.now()
		schedule.setBackoffTill(time.Time{})
		if reply.chunkIndex == len(reply.chunks) {
			return terminalRequestResult{done: true}
		}
		return terminalRequestResult{retryAt: schedule.lastEdit.Add(editInterval)}
	}

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
	_, err := api.SendMessage(ctx, params)
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
		return terminalRequestResult{done: true, err: fmt.Errorf("send final chunk: %w", err)}
	}
	reply.chunkIndex++
	reply.plainTextFallback = false
	schedule.lastEdit = o.now()
	schedule.setBackoffTill(time.Time{})
	if reply.chunkIndex == len(reply.chunks) {
		return terminalRequestResult{done: true}
	}
	return terminalRequestResult{retryAt: schedule.lastEdit.Add(editInterval)}
}

func (o *Outbound) cleanupTerminalReply(reply *terminalReply) {
	reply.cleanupOnce.Do(func() {
		if reply.initialized && reply.schedule != nil {
			o.releaseChat(reply.schedule, reply.target.chatID)
			return
		}
		o.clearStream(reply.event)
	})
}

// handleTaskFailed clears any stream state and posts a failure notice.
func (o *Outbound) handleTaskFailed(e events.Event) {
	if taskFailureRetryPending(e.Payload) {
		o.clearStream(e)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := o.resolveTarget(ctx, e, false)
	if err != nil || target == nil {
		return
	}
	o.mu.Lock()
	st := o.streams[target.streamKey]
	delete(o.streams, target.streamKey)
	var schedule *chatSchedule
	if st != nil {
		schedule = st.schedule
	} else {
		schedule = o.retainChatLocked(target.botKey, target.chatID)
	}
	o.mu.Unlock()
	if schedule == nil {
		o.logger.WarnContext(ctx, "telegram outbound: failure notice skipped; chat scheduler is saturated")
		return
	}
	defer o.releaseChat(schedule, target.chatID)
	schedule.mu.Lock()
	defer schedule.mu.Unlock()

	api := newBotAPI(o.apiBase, target.botToken, o.client)
	if st != nil && st.messageID != 0 {
		if err := o.runScheduled(ctx, schedule, func() error {
			return api.EditMessageText(ctx, editMessageTextParams{
				ChatID: st.chatID, MessageID: st.messageID, Text: taskFailedText,
			})
		}); err == nil || isNotModified(err) {
			return
		}
	}
	if err := o.runScheduled(ctx, schedule, func() error {
		_, err := api.SendMessage(ctx, sendMessageParams{
			ChatID: target.chatID, Text: taskFailedText, MessageThreadID: target.threadID,
			ReplyParameters: optionalReplyParameters(target.replyTo),
		})
		return err
	}); err != nil {
		o.logger.WarnContext(ctx, "telegram outbound: failure notice failed", "error", err)
	}
}

// handleTaskCancelled drops local stream state without changing the partial
// Telegram message already visible to the user. Cancellation has no final
// agent answer; retaining the partial avoids replacing user-visible content
// with a synthetic notice while ensuring no task state remains resident.
func (o *Outbound) handleTaskCancelled(e events.Event) {
	o.clearStream(e)
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
}

// resolveTarget maps an event to its Telegram binding + credentials. Returns
// (nil, nil) when the session is not Telegram-bound. When viaTask is true the
// chat session id is recovered from the task row (EventTaskMessage carries
// only TaskID).
func (o *Outbound) resolveTarget(ctx context.Context, e events.Event, viaTask bool) (*replyTarget, error) {
	var task *db.AgentTaskQueue
	taskID, hasTaskID := eventTaskID(e)
	sessionID, err := util.ParseUUID(e.ChatSessionID)
	if err != nil || !sessionID.Valid {
		if !viaTask || !hasTaskID {
			return nil, nil
		}
		taskRow, terr := o.q.GetAgentTask(ctx, taskID)
		if terr != nil {
			return nil, fmt.Errorf("load agent task: %w", terr)
		}
		if !taskRow.ChatSessionID.Valid {
			return nil, nil
		}
		task = &taskRow
		sessionID = taskRow.ChatSessionID
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeTelegram),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // not a Telegram session
		}
		return nil, fmt.Errorf("lookup telegram chat binding: %w", err)
	}
	// A bound session can be reused by web/mobile tasks. Only a task whose
	// immutable input provenance came from a channel may reply to Telegram;
	// chat_input_task_id alone cannot distinguish direct tasks from channel
	// tasks. Fail closed when the task id or provenance lookup is unavailable.
	if !hasTaskID {
		return nil, nil
	}
	if task == nil {
		taskRow, terr := o.q.GetAgentTask(ctx, taskID)
		if terr != nil {
			return nil, fmt.Errorf("load agent task: %w", terr)
		}
		task = &taskRow
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, *task)
	if err != nil {
		return nil, fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil, nil
	}
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
		streamKey: util.UUIDToString(taskID),
		botKey:    util.UUIDToString(inst.ID),
		chatID:    chatID,
		threadID:  threadID,
		replyTo:   replyTo,
		botToken:  creds.BotToken,
	}, nil
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
