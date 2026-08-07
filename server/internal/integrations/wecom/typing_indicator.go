package wecom

// typing_indicator.go — WeCom's answer to "the bot heard you".
//
// Slack stamps 👀 on the user's message, Feishu a typing badge. WeCom has
// neither: the smart-bot protocol publishes no reaction, no read receipt and
// no typing signal at all. What it does have is the streaming message, so the
// same engine.TypingNotifier interface means something different here — the
// indicator IS the reply, opened early and filled in later. Two consequences
// follow from that, and both shape this file:
//
//   - The bubble MUST be closed. A reaction that never clears is untidy; a
//     stream that never finishes is a spinner sitting in the user's chat for
//     good. So every way a turn can end — answered, failed, never started,
//     outlived its window — writes a closing frame carrying visible text.
//   - OnSettled is not the normal ending. As on the other platforms the Router
//     only calls it when the flush produced no task run; the answer closes the
//     bubble from the chat-done subscriber in outbound.go, which is the only
//     place that has the answer to close it with.

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The four ways a streaming reply ends in something other than an answer. Each
// one closes the loading bubble the question opened, so each one has to carry
// visible text — WeCom discards a closing frame it considers empty and the
// bubble spins on forever (see hasVisibleChar in ws_frame.go).
//
// Chinese, unconditionally, the same way the rest of this adapter's
// user-facing strings are: WeCom deployments are China-only.
const (
	// streamCopyNoReply — the agent finished with nothing to say.
	streamCopyNoReply = "（这轮没有需要回复的内容）"
	// streamCopyMerged closes a QUEUED round's bubble whose run finished with
	// nothing of its own to say — the reply ahead of it already covered this
	// message. A first round's empty finish keeps streamCopyNoReply; this one
	// has an earlier answer to point at.
	streamCopyMerged = "✅ 这条已并入上一条回复一起处理了。"
	// streamCopyNotStarted — no run was triggered at all (agent offline or
	// archived, or the enqueue failed); the replier's own notice follows as a
	// separate message with the detail.
	streamCopyNotStarted = "已收到，但这条暂时没能开始处理。"
	// streamCopyFailed — the run failed.
	streamCopyFailed = "⚠️ 这次没跑通，请稍后再试一次。"
	// streamCopyStillWorking — the run outlived the protocol's stream window,
	// so we close the bubble ourselves and answer separately later.
	streamCopyStillWorking = "还在处理，完成后我再单独回复你。"
)

// streamCloseTimeout bounds a closing frame written from a timer or a bus
// subscriber, neither of which has a caller's context to inherit.
const streamCloseTimeout = 10 * time.Second

// taskLookup resolves a task id to the chat session it belongs to.
// task:failed's sweeper publisher carries no session, so it has to come off
// the task row. *db.Queries satisfies it.
type taskLookup interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
}

// chatBindingLookup finds which WeCom chat a session belongs to. It is the
// address of last resort for a failure notice: the handle is gone and this
// process has no note of the round, which is what a restart mid-run looks like.
// The two queries are the ones outbound.go already makes on the same path for
// the answer, so *db.Queries satisfies both interfaces without a new query.
type chatBindingLookup interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// TypingIndicatorManager opens a streaming bubble per round when messages are
// ingested and owns each one until something closes it.
type TypingIndicatorManager struct {
	senders  *sendersRegistry
	streams  *streamStore
	tasks    taskLookup
	bindings chatBindingLookup
	log      *slog.Logger

	// guardAfter is when the manager closes a bubble nobody else has. Zero
	// disables the guard (tests that drive the clock themselves).
	guardAfter time.Duration
}

// TypingIndicatorConfig wires the manager. Senders and Streams are the same
// two instances the outbound subscriber holds — the bubble is opened on one
// side of the process and closed on the other.
type TypingIndicatorConfig struct {
	Senders *sendersRegistry
	Streams *streamStore
	Logger  *slog.Logger

	// Tasks resolves a task id to its chat session, for the task:failed the
	// sweepers publish without one. Nil limits failure handling to the events
	// that carry a session.
	Tasks taskLookup

	// Bindings finds the chat behind a session when nothing else can — a run
	// that failed after this process was restarted, or after a bubble that was
	// never painted. Nil keeps the failure notice to the rounds this process
	// has a handle or a note for.
	Bindings chatBindingLookup

	// GuardAfter overrides streamGuardAfter. Test-only.
	GuardAfter time.Duration
}

var _ engine.TypingNotifier = (*TypingIndicatorManager)(nil)

// NewTypingIndicator builds the manager.
func NewTypingIndicator(cfg TypingIndicatorConfig) *TypingIndicatorManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	guard := cfg.GuardAfter
	if guard == 0 {
		guard = streamGuardAfter
	}
	return &TypingIndicatorManager{
		senders:    cfg.Senders,
		streams:    cfg.Streams,
		tasks:      cfg.Tasks,
		bindings:   cfg.Bindings,
		log:        logger,
		guardAfter: guard,
	}
}

// OnIngested paints a "working on it" bubble for the round this message
// starts and records what it takes to come back and fill it in. A message
// inside the debounce window joins the round already on screen; a message
// past it — one that will wait behind the run in flight — opens a bubble of
// its own immediately, because a wait with nothing on screen reads as a
// message that was lost. The bubble carries no words while it waits: the
// think tag renders as the client's own animated dots, which is the receipt,
// and words would need a language before there is anything to say.
//
// The Router calls this on a detached goroutine with its own deadline, so
// nothing here needs to be quick for the ACK's sake — but everything here is
// best-effort: a bubble that fails to open costs the user a few seconds of
// uncertainty, and the answer still arrives as a plain message.
func (m *TypingIndicatorManager) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	if m.senders == nil || m.streams == nil || !sessionID.Valid {
		return
	}
	// A standalone /issue is answered by the replier and deliberately never
	// triggers an agent run, so no chat-done event would ever arrive to close
	// a bubble opened for it.
	if msg.SkipAgentRun {
		return
	}
	// Stamp the arrival now, before the lookups below. Whether two messages
	// belong to one round is a question about when they were SENT, and the
	// engine's debouncer answers it from arrival. Reading the clock after a
	// decode and a database round trip answers a different question — one that
	// a loaded pool can move by seconds — so the two would disagree: a message
	// the debouncer gave its own run could be folded into the previous round
	// here, leaving it with no bubble and no receipt, or the reverse, leaving
	// one run with two spinners.
	arrivedAt := m.streams.clock()
	wm, err := wecomMsgFromRaw(msg)
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: cannot read the inbound envelope",
			"chat_session_id", util.UUIDToString(sessionID), "error", err)
		return
	}
	// Without the callback's req_id there is no stream to open: the server
	// refuses a frame carrying anything else, and an event callback's req_id
	// is refused outright (846605).
	if wm.ReqID == "" {
		return
	}
	chatID := msg.Source.ChatID
	if chatID == "" {
		chatID = wm.ChatID
	}
	if chatID == "" {
		return
	}

	h := streamHandle{
		ReqID:          wm.ReqID,
		StreamID:       newStreamID(),
		InstallationID: inst.ID,
		ChatID:         chatID,
		ChatType:       aibotChatTypeFromChannel(msg.Source.ChatType),
		CreatedAt:      arrivedAt,
	}
	if m.streams.open(sessionID, h) == roundJoined {
		// Inside the debounce window: the batcher is about to fold this
		// message into the round whose bubble is already on screen, and that
		// bubble is this message's receipt too.
		return
	}

	// Three ways the opening frame does not land, and only one of them is a
	// reason to give the bubble up.
	//
	// An ack that did not come back says nothing about whether the frame did:
	// re-sending the same stream id later creates the message if the opening
	// frame was lost, so the worst case is a user who waits without a spinner
	// rather than one who never gets an answer.
	//
	// Busy and superseded mean something stronger. Both say another frame on
	// this req_id got to the socket first, and that frame carried a stream id
	// of its own, which is exactly how a bubble is created. So the spinner is
	// on the user's screen. Dropping the handle there arms no guard and leaves
	// nothing that could ever close it.
	//
	// A verdict from the server is the one that ends it: 846605 and 846608 mean
	// this stream will never take a frame, so no bubble was painted and keeping
	// the handle would swallow the answer rather than deliver it.
	if err := m.senders.stream(ctx, h, streamThinkingPlaceholder, false); err != nil {
		switch {
		case errors.Is(err, errStreamAckTimeout),
			errors.Is(err, errStreamBusy),
			errors.Is(err, errStreamSuperseded):
			m.log.DebugContext(ctx, "wecom typing: opening frame did not land, keeping the handle",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
		default:
			m.streams.drop(sessionID, h.StreamID)
			m.log.WarnContext(ctx, "wecom typing: opening frame refused",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
			return
		}
	}
	m.armGuard(sessionID, h)
}

// OnSettled closes the bubble of a round that never became a run — agent
// offline or archived, or an enqueue that failed. This is the only chance to
// stop that spinner: with no task there is no task lifecycle event, so neither
// the chat-done subscriber nor the failure subscriber will ever fire. The copy
// is deliberately thin because the replier's own notice follows as a separate
// message with the reason.
//
// The TAIL is the right bubble: OnSettled answers the flush that just
// settled, which is the newest round — the ones ahead of it belong to runs
// that are already real and end through their own events.
func (m *TypingIndicatorManager) OnSettled(ctx context.Context, sessionID pgtype.UUID) {
	m.closeBubble(ctx, sessionID,
		func(s *streamStore) (streamHandle, bool) { return s.takeTail(sessionID, roundOver) },
		streamCopyNotStarted, "settled")
}

// Register subscribes the manager to the run failure event.
//
// EventChatDone is deliberately NOT subscribed here: the answer belongs in the
// bubble, and only the outbound subscriber holds the answer. Registering for
// it here would close the bubble first and leave the reply to arrive
// underneath it.
func (m *TypingIndicatorManager) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskFailed, m.handleTaskFailed)
}

// handleTaskFailed says a run died, in the bubble if there still is one and as
// a plain message if there is not.
//
// task:failed has two publishers and they do not agree on the payload.
// broadcastTaskEvent, which FailTask uses, carries chat_session_id.
// HandleFailedTasks — every sweeper, recover-orphans, and the daemon heartbeat
// timeout — carries task_id, agent_id, issue_id, status and failure_reason, and
// no session anywhere. That second publisher is the whole crashed-daemon path,
// so without the task-id fallback the bubble spins on until the guard replaces
// it with "still working, I'll reply separately" — a promise about a run that
// has been dead for five minutes. The fallback stays on this side of the
// boundary: the adapter carries its own routing rather than widening a payload
// three other consumers read.
//
// The bubble is not the whole of it. A handle is consumed by whichever ending
// gets there first, and the guard is allowed to be that one at the five-minute
// mark while the run carries on — so every run longer than five minutes that
// then failed finds no handle here. This notice is the only "that run did not
// go through" WeCom ever produces: the replier speaks for needs_binding,
// offline, archived and issue_created and for nothing else. So the handle is
// one address among three, and sayTheRunFailed works down the rest.
func (m *TypingIndicatorManager) handleTaskFailed(e events.Event) {
	if m.streams == nil {
		return
	}
	if m.bindings == nil && m.streams.depth() == 0 && m.streams.remembered() == 0 {
		// Nothing open, nothing owed, and no way to find a chat: no reason to
		// read a row for someone else's run.
		return
	}
	sessionID, ok := m.sessionFor(e)
	if !ok {
		return // an issue / autopilot run, with no chat session and no bubble
	}
	ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
	defer cancel()
	taskID := taskIDFromEvent(e)
	if m.closeBubble(ctx, sessionID,
		func(s *streamStore) (streamHandle, bool) { return s.takeTask(sessionID, taskID, roundOver) },
		streamCopyFailed, "task failed") {
		return
	}
	m.sayTheRunFailed(ctx, sessionID, taskID)
}

// sayTheRunFailed delivers the failure to a round whose bubble is already gone.
//
// Two addresses, in the order of how much they are trusted. The note the handle
// left behind is the chat the question came from, which is what the guard's
// promise was made in and what the binding row may no longer point at. Failing
// that — a restart mid-run, a turn whose opening frame the server refused —
// the binding row is the only address there is.
//
// A round the store says is accounted for gets nothing. That is the whole of
// the not-twice rule: the answer took the handle the same way the guard does,
// and a task:failed arriving behind a delivered answer — an auto-retry's first
// attempt, a sweeper that ran late — must not contradict it.
func (m *TypingIndicatorManager) sayTheRunFailed(ctx context.Context, sessionID pgtype.UUID, taskID string) {
	if m.senders == nil {
		return
	}
	addr, verdict := m.streams.claimEnding(sessionID)
	switch verdict {
	case roundToldAlready:
		return
	case roundForgotten:
		found, ok := m.addressFromBinding(ctx, sessionID)
		if !ok {
			return
		}
		addr = found
		// No handle was consumed and no note was claimed on this path, so
		// filing one is the only thing that stops the next publisher of the
		// same news from repeating it.
		m.streams.remember(sessionID, addr, roundOver, taskID)
	}
	if err := m.senders.sendTextCtx(ctx, addr.InstallationID, addr.ChatID, addr.ChatType, streamCopyFailed); err != nil {
		m.log.WarnContext(ctx, "wecom typing: could not say the run failed",
			"chat_session_id", util.UUIDToString(sessionID),
			"installation_id", util.UUIDToString(addr.InstallationID), "error", err)
	}
}

// addressFromBinding reads the chat a session belongs to off the binding row —
// the same two queries outbound.go makes when an answer has no bubble to land
// in. A session with no wecom binding is not ours to speak in: this subscriber
// sees every failed run on a shared bus, including Slack's and the web UI's.
func (m *TypingIndicatorManager) addressFromBinding(ctx context.Context, sessionID pgtype.UUID) (roundAddress, bool) {
	if m.bindings == nil {
		return roundAddress{}, false
	}
	binding, err := m.bindings.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   channelTypeWecom,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			m.log.WarnContext(ctx, "wecom typing: cannot find the chat a failed run belongs to",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
		}
		return roundAddress{}, false
	}
	// The installation row answers whether the bot is still installed. The
	// binding row outlives a revoke, so a session keeps looking reachable
	// after the bot has been removed.
	inst, err := m.bindings.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: cannot load the installation a failed run belongs to",
			"installation_id", util.UUIDToString(binding.InstallationID), "error", err)
		return roundAddress{}, false
	}
	if inst.Status != string(InstallationActive) {
		return roundAddress{}, false
	}
	return roundAddress{
		InstallationID: binding.InstallationID,
		ChatID:         binding.ChannelChatID,
		ChatType:       aibotChatTypeFromChannel(channel.ChatType(binding.ChatType)),
	}, true
}

// sessionFor finds the chat session behind a task lifecycle event. The
// sweeper's task:failed does not carry one, so it comes off the task row.
//
// Everything here runs on the goroutine that published the event: the daemon's
// own HTTP handler, or a sweeper tick. Hence the short deadline.
func (m *TypingIndicatorManager) sessionFor(e events.Event) (pgtype.UUID, bool) {
	if sessionID, ok := sessionIDFromEvent(e); ok {
		return sessionID, true
	}
	if m.tasks == nil {
		return pgtype.UUID{}, false
	}
	taskID := taskIDFromEvent(e)
	if taskID == "" {
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		return pgtype.UUID{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskLookupTimeout)
	defer cancel()
	task, err := m.tasks.GetAgentTask(ctx, id)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return task.ChatSessionID, task.ChatSessionID.Valid
}

// taskLookupTimeout is the whole budget this subscriber may spend on somebody
// else's goroutine. The bus is synchronous: a task:failed subscriber runs on
// the goroutine that published the event, and a sweeper tick is not ours to
// hold while a loaded pool answers.
const taskLookupTimeout = 800 * time.Millisecond

// taskIDFromEvent prefers the envelope's routing hint and falls back to the
// payload. ChatDonePayload matters most: broadcastChatDone sets no TaskID on
// the envelope, and on the in-process bus the payload stays typed — miss it
// and every answer takes the HEAD round unconditionally, which is the wrong
// bubble whenever a guard has already closed the running round's.
func taskIDFromEvent(e events.Event) string {
	if e.TaskID != "" {
		return e.TaskID
	}
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		return p.TaskID
	case protocol.TaskProgressPayload:
		return p.TaskID
	case map[string]any:
		s, _ := p["task_id"].(string)
		return s
	}
	return ""
}

// armGuard schedules the close that happens when nothing else does. WeCom
// stops accepting frames for a stream past streamMaxAge, so a bubble that
// outlives the window — a long run, or a round stuck in the queue behind one —
// would otherwise become a spinner we can no longer touch. The guard closes
// exactly the bubble it was armed for, by stream id: with several bubbles open
// in one session, a timer that took the head could seal a newer round's bubble
// with an older round's promise.
//
// This is the one closer that does not end the round. Its copy says the reply
// is coming separately, and the run is still going — so the handle it consumes
// leaves a note behind, and whatever the run does next is said against that.
func (m *TypingIndicatorManager) armGuard(sessionID pgtype.UUID, h streamHandle) {
	if m.guardAfter <= 0 {
		return
	}
	t := time.AfterFunc(m.guardAfter, func() {
		ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
		defer cancel()
		m.closeBubble(ctx, sessionID,
			func(s *streamStore) (streamHandle, bool) { return s.takeStream(sessionID, h.StreamID, roundContinues) },
			streamCopyStillWorking, "window expiring")
	})
	m.streams.arm(sessionID, h.StreamID, t)
}

// closeBubble seals one bubble with text, if take finds one to seal, and
// reports whether it did. take names WHICH bubble — the failed task's, the
// settled flush's (tail), the guard's own (by stream id) — and consuming the
// handle inside the store makes this idempotent: two closers racing produce one
// closing frame. The ending each take carries is what the caller's words amount
// to for the round; every closer ends it except the guard, whose copy promises
// a separate reply while the run goes on.
//
// A closing frame that cannot go out falls back to a plain message, the same
// way the answer does in outbound.go. The words matter more here than there:
// streamCopyFailed is the only "that run did not go through" WeCom ever
// produces, so a frame lost to a reconnect window would otherwise leave the
// user with a spinner and no explanation that would ever arrive. The addressing
// comes off the handle, captured at ingest, because by now the binding row may
// point at a different chat.
func (m *TypingIndicatorManager) closeBubble(ctx context.Context, sessionID pgtype.UUID, take func(*streamStore) (streamHandle, bool), text, why string) bool {
	if m.senders == nil || m.streams == nil || !sessionID.Valid {
		return false
	}
	h, ok := take(m.streams)
	if !ok {
		return false
	}
	err := m.senders.stream(ctx, h, text, true)
	if err == nil {
		return true
	}
	m.log.WarnContext(ctx, "wecom typing: closing frame failed, saying it as a new message",
		"chat_session_id", util.UUIDToString(sessionID),
		"reason", why, "unusable", streamUnusable(err), "error", err)
	if sendErr := m.senders.sendTextCtx(ctx, h.InstallationID, h.ChatID, h.ChatType, text); sendErr != nil {
		m.log.WarnContext(ctx, "wecom typing: the fallback message was unsendable too",
			"chat_session_id", util.UUIDToString(sessionID), "reason", why, "error", sendErr)
	}
	return true
}

// sessionIDFromEvent recovers the chat session from a task lifecycle event.
// EventChatDone puts it on the envelope; EventTaskFailed carries it only in the
// broadcast payload, and only for chat runs — so both places are checked.
func sessionIDFromEvent(e events.Event) (pgtype.UUID, bool) {
	if e.ChatSessionID != "" {
		if id, err := util.ParseUUID(e.ChatSessionID); err == nil && id.Valid {
			return id, true
		}
	}
	if p, ok := e.Payload.(map[string]any); ok {
		if s, _ := p["chat_session_id"].(string); s != "" {
			if id, err := util.ParseUUID(s); err == nil && id.Valid {
				return id, true
			}
		}
	}
	return pgtype.UUID{}, false
}
