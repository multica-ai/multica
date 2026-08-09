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
	// streamCopyCancelled — the run was cancelled, so no answer is coming.
	// Separate copy from streamCopyFailed on purpose: "试一次" invites a retry
	// of something the user just stopped on purpose.
	streamCopyCancelled = "⏹️ 这次处理已取消。"
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

// taskOrigin is the task row plus where the run's input came from. The typing
// indicator needs both halves: the row to find the chat session behind a
// sweeper's task:failed, and the provenance stamp to decide whether a failed
// run's notice belongs in the WeCom room at all — the same question outbound.go
// asks before pushing an answer there. Kept apart from taskLookup because the
// round matcher and the outbound subscriber only ever need the row.
// *db.Queries satisfies it.
type taskOrigin interface {
	taskLookup
	engine.ChannelProvenanceQueries
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
	tasks    taskOrigin
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
	// sweepers publish without one, and answers where that run's input came
	// from. Nil limits failure handling to the events that carry a session,
	// and leaves the origin question unanswerable — see failureBelongsOnWecom
	// for what this manager does when it cannot ask.
	Tasks taskOrigin

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

// TypingIndicatorWiring reports which of the four dependencies a manager holds.
//
// Every one of them is optional and every one of them narrows the manager
// silently when it is missing: nothing panics, nothing logs, Register still
// subscribes, and the events still arrive — the closing frame just never gets
// written, which the user sees as a bubble that spins until the guard replaces
// it with a promise nobody keeps. That makes "is it wired" unfalsifiable from
// the outside, and a boot path that drops one looks exactly like a healthy
// one. This is the inspection point that makes it falsifiable.
type TypingIndicatorWiring struct {
	// Senders is the live WebSocket registry. Without it no closing frame and
	// no plain-message fallback can be written at all.
	Senders bool
	// Streams is the round store shared with the outbound subscriber. Without
	// it every handler returns on its first line, so no bubble is ever closed
	// by any ending.
	Streams bool
	// Tasks resolves a task id to its chat session. Without it the sweepers'
	// task:failed — which names a task and no session — resolves to nothing
	// and the bubble behind a swept run is never closed.
	Tasks bool
	// Bindings finds the chat behind a session when no round is on file.
	// Without it a run that fails after its bubble is gone (guard closed it at
	// five minutes, or the process restarted mid-run) tells the user nothing,
	// and the guard's "I'll reply separately" is never answered.
	Bindings bool
}

// Wiring reports the dependencies this manager was built with. For boot-wiring
// guards; it copies four booleans and hands out no references.
func (m *TypingIndicatorManager) Wiring() TypingIndicatorWiring {
	return TypingIndicatorWiring{
		Senders:  m.senders != nil,
		Streams:  m.streams != nil,
		Tasks:    m.tasks != nil,
		Bindings: m.bindings != nil,
	}
}

// OnIngested paints a "working on it" bubble for the run this message belongs
// to and records what it takes to come back and fill it in. Which run that is
// comes from batch — the engine debouncer's own verdict, decided under the
// lock that arms the window — so the first message of a run paints a bubble
// and the rest join it, and a message the debouncer gave a run of its own gets
// a bubble of its own immediately, because a wait with nothing on screen reads
// as a message that was lost. The bubble carries no words while it waits: the
// think tag renders as the client's own animated dots, which is the receipt,
// and words would need a language before there is anything to say.
//
// The Router calls this on a detached goroutine with its own deadline, so
// nothing here needs to be quick for the ACK's sake — but everything here is
// best-effort: a bubble that fails to open costs the user a few seconds of
// uncertainty, and the answer still arrives as a plain message.
func (m *TypingIndicatorManager) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.senders == nil || m.streams == nil || !sessionID.Valid || batch == 0 {
		return
	}
	// A standalone /issue is answered by the replier and deliberately never
	// triggers an agent run, so no chat-done event would ever arrive to close
	// a bubble opened for it.
	if msg.SkipAgentRun {
		return
	}
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
	}
	if m.streams.open(sessionID, batch, h) != roundOpened {
		// roundJoined — the batcher folded this message into a run whose
		// bubble is already on screen, and that bubble is this message's
		// receipt too. roundFinished — this goroutine outlived the run it was
		// painting for, and a bubble now would be one nothing ever closes.
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
			m.streams.drop(sessionID, batch)
			m.log.WarnContext(ctx, "wecom typing: opening frame refused",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
			return
		}
	}
	m.armGuard(sessionID, batch)
}

// OnRunStarted files the task the debounced flush created for this run. It is
// the binding every later ending is matched on: the answer, the failure and
// the cancellation all name a task, and this is what turns that name into "the
// bubble this question opened" without reading anything off arrival order.
//
// It can arrive before OnIngested has painted the bubble — the Router detaches
// the ingest goroutine and the flush runs on the batcher's timer — so the
// store files the run either way and the bubble attaches to it when it lands.
func (m *TypingIndicatorManager) OnRunStarted(_ context.Context, sessionID pgtype.UUID, batch engine.RunBatchID, taskID pgtype.UUID) {
	if m.streams == nil || !sessionID.Valid || !taskID.Valid {
		return
	}
	m.streams.bind(sessionID, batch, util.UUIDToString(taskID))
}

// OnSettled closes the bubble of a round that never became a run — agent
// offline or archived, or an enqueue that failed. This is the only chance to
// stop that spinner: with no task there is no task lifecycle event, so neither
// the chat-done subscriber nor the failure subscriber will ever fire. The copy
// is deliberately thin because the replier's own notice follows as a separate
// message with the reason.
//
// batch names which bubble: the flush that settled reports the run it was
// answering, so a session with several rounds open closes the right one
// instead of whichever happens to be newest.
func (m *TypingIndicatorManager) OnSettled(ctx context.Context, sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.senders == nil || m.streams == nil || !sessionID.Valid {
		return
	}
	m.streams.sayEnding(sessionID, byBatch(batch), roundOver, nil,
		func(t roundTurn) (roundAddress, error) {
			if !t.HasBubble {
				return roundAddress{}, errNothingToSay
			}
			return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyNotStarted, "settled")
		})
}

// Register subscribes the manager to the two ways a run ends without an
// answer. Both have to be here or the bubble outlives its run: a failure and a
// cancellation each publish nothing the outbound subscriber reads, so nothing
// else would ever seal that stream.
//
// EventChatDone is deliberately NOT subscribed here: the answer belongs in the
// bubble, and only the outbound subscriber holds the answer. Registering for
// it here would close the bubble first and leave the reply to arrive
// underneath it.
func (m *TypingIndicatorManager) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskFailed, m.handleTaskFailed)
	bus.Subscribe(protocol.EventTaskCancelled, m.handleTaskCancelled)
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
	// An attempt the platform is already retrying is not an ending. FailTask
	// publishes task:failed for it anyway — the web card has to clear — and
	// flags it retry_pending so consumers stay quiet; taskFailedFields even
	// withholds the error text, and dingtalk's outbound already honours it.
	// Closing the bubble here would tell the user "这次没跑通" about an attempt
	// whose replacement is already queued, and the retry's answer would then
	// land underneath a bubble that had declared failure. The round stays open
	// for the attempt that reports the real outcome; the retry clone's own
	// events find it through the batch owner it inherited (roundTaker).
	if retryPending(e) {
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
	// A run started in the browser against this same session gets its notice
	// there. Announcing it here would tell everyone in the room that something
	// they never saw has gone wrong. Asked BEFORE the bubble is taken, for the
	// reason outbound.go asks before finishing one: a gate placed after the
	// take has already sealed a WeCom round's bubble with a web run's ending.
	if !m.failureBelongsOnWecom(ctx, sessionID, taskID) {
		return
	}
	m.rounds().sayEnding(ctx, sessionID, byTask(taskID), roundOver,
		func(t roundTurn) (roundAddress, error) { return m.sayTheRunFailed(ctx, sessionID, t) })
}

// failureBelongsOnWecom asks the question outbound.go asks of an answer: did
// this run's input come from the channel? The engine makes the INSTALLER the
// creator of a group's chat_session, so that session appears in their own
// Multica chat list and they can ask it something in a browser. Both runs fail
// the same way, on the same bus, carrying the same session — nothing in the
// event says which surface asked.
//
// This is an authorization check on writing into somebody else's group chat,
// so uncertainty is not permission. A lookup that did not answer is not
// evidence the question came from WeCom, and "one line of copy naming no
// question and no answer" still tells a room that activity it cannot see went
// wrong — the existence of the activity is the disclosure. So an origin that
// cannot be established refuses, and says so at WARN.
//
// That costs nothing on the case worth protecting, because that case has local
// evidence. A round sitting on the guard's "还在处理，完成后我再单独回复你" has
// this run on its owed list, and a round still open has it bound; both are
// written only by the inbound path, and both are read here before anything is
// consumed. So a WeCom round whose promise is outstanding is delivered while
// the database is down, and it is only the runs this process has never seen
// that have to produce a row to be spoken for.
func (m *TypingIndicatorManager) failureBelongsOnWecom(ctx context.Context, sessionID pgtype.UUID, taskID string) bool {
	// Positive proof of origin, held in memory. Asked first, so the paths
	// below never decide the one case where silence breaks a promise.
	if m.streams.knowsRound(sessionID, taskID) {
		return true
	}
	if taskID == "" {
		// Both task:failed publishers carry one in production — see the block
		// comment above handleTaskFailed — so this is a payload shape nothing
		// real produces, and it names no run to attribute.
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "no task id on the event")
		return false
	}
	if m.tasks == nil {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "no task lookup configured")
		return false
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "unparseable task id")
		return false
	}
	task, err := m.tasks.GetAgentTask(ctx, id)
	if err != nil {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "cannot read the task row: "+err.Error())
		return false
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, m.tasks, task)
	if err != nil {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "cannot read the channel-ingested stamp: "+err.Error())
		return false
	}
	return deliver
}

// refuseUnknownOrigin logs a failure notice this process declined to put in a
// WeCom room. WARN because it is a real outcome for the asker — a run of
// theirs ended and they were not told — and the only signal that a database
// the origin check depends on has stopped answering.
func (m *TypingIndicatorManager) refuseUnknownOrigin(ctx context.Context, sessionID pgtype.UUID, taskID, reason string) {
	m.log.WarnContext(ctx, "wecom typing: refusing to announce a failed run whose origin cannot be established",
		"chat_session_id", util.UUIDToString(sessionID), "task_id", taskID, "reason", reason)
}

// handleTaskCancelled seals the bubble of a run the user stopped.
//
// Cancellation is a terminal state that publishes no chat:done and no
// task:failed, so without this the bubble spins for the full five minutes and
// the guard then promises a separate reply — about a run the user cancelled
// themselves, that will never come. Every cancel path lands here: CancelTask
// for a running or queued task, CancelQueuedChatTasks for the follow-ups
// behind it, and the agent-level and issue-level bulk cancels, which all
// broadcast task:cancelled per row. A session with several rounds open
// therefore gets one closing frame per cancelled run, each on its own bubble,
// because the round is matched by the task id the flush bound to it.
//
// Unlike a failure this does NOT go looking in the binding row when no round
// is on file. streamCopyFailed is the only "that run did not go through" WeCom
// ever produces, which is why a failure is worth chasing an address for; a
// cancellation was performed by the user, and chasing it would turn one
// "cancel all tasks" click into a message in every chat that agent serves —
// including sessions where WeCom never showed a bubble at all.
func (m *TypingIndicatorManager) handleTaskCancelled(e events.Event) {
	if m.streams == nil {
		return
	}
	if m.streams.depth() == 0 && m.streams.remembered() == 0 {
		return
	}
	sessionID, ok := m.sessionFor(e)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
	defer cancel()
	m.rounds().sayEnding(ctx, sessionID, byTask(taskIDFromEvent(e)), roundOver,
		func(t roundTurn) (roundAddress, error) {
			if m.senders == nil {
				return roundAddress{}, errNothingToSay
			}
			if t.HasBubble {
				return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyCancelled, "task cancelled")
			}
			// No bubble left. If the guard already closed one for this run it
			// promised a separate reply, and that promise is now void — say so,
			// in the chat the promise was made in. Only for THIS round's own
			// promise: a session can hold several, the copy differs per
			// outcome, and announcing a cancel against another round's promise
			// would tell its asker "已取消" about a run nobody stopped.
			if !t.Promised || !t.Addr.known() {
				return roundAddress{}, errNothingToSay
			}
			return t.Addr, m.sayAsPlainMessage(ctx, sessionID, t.Addr, streamCopyCancelled)
		})
}

// rounds builds the matcher that turns a task id on an event into the round it
// belongs to.
func (m *TypingIndicatorManager) rounds() roundTaker {
	return roundTaker{streams: m.streams, tasks: m.tasks, log: m.log}
}

// retryPending reports whether FailTask has already created a retry child for
// this attempt. taskFailedFields sets it on every task:failed payload.
func retryPending(e events.Event) bool {
	p, ok := e.Payload.(map[string]any)
	if !ok {
		return false
	}
	pending, _ := p["retry_pending"].(bool)
	return pending
}

// sayTheRunFailed delivers a failed run's ending wherever the round can still
// be reached, in the order of how much each address is trusted.
//
// The bubble first, while the round still has one. Then the note the handle
// left behind, which is the chat the question came from and what the guard's
// promise was made in — the binding row may no longer point at it. Failing both
// — a restart mid-run, a turn whose opening frame the server refused — the
// binding row is the only address there is.
//
// A round the store says is accounted for never reaches here at all: the ledger
// answers roundToldAlready and runs no delivery. That is the whole of the
// not-twice rule, and it is per run, so a task:failed arriving behind this run's
// own delivered answer stays quiet while a second run's failure is still told.
func (m *TypingIndicatorManager) sayTheRunFailed(ctx context.Context, sessionID pgtype.UUID, t roundTurn) (roundAddress, error) {
	if m.senders == nil {
		return roundAddress{}, errNothingToSay
	}
	if t.HasBubble {
		return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyFailed, "task failed")
	}
	addr := t.Addr
	if !addr.known() {
		found, ok := m.addressFromBinding(ctx, sessionID)
		if !ok {
			return roundAddress{}, errNothingToSay
		}
		addr = found
	}
	return addr, m.sayAsPlainMessage(ctx, sessionID, addr, streamCopyFailed)
}

func (m *TypingIndicatorManager) sayAsPlainMessage(ctx context.Context, sessionID pgtype.UUID, addr roundAddress, text string) error {
	err := m.senders.sendTextCtx(ctx, addr.InstallationID, addr.ChatID, addr.ChatType, text)
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: could not deliver a run's ending",
			"chat_session_id", util.UUIDToString(sessionID),
			"installation_id", util.UUIDToString(addr.InstallationID), "error", err)
	}
	return err
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
// exactly the round it was armed for, by batch: with several bubbles open in
// one session, a timer that took the head could seal a newer round's bubble
// with an older round's promise.
//
// This is the one closer that does not end the round. Its copy says the reply
// is coming separately, and the run is still going — so the handle it consumes
// leaves a note behind, and whatever the run does next is said against that.
func (m *TypingIndicatorManager) armGuard(sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.guardAfter <= 0 {
		return
	}
	t := time.AfterFunc(m.guardAfter, func() {
		ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
		defer cancel()
		m.fireGuard(ctx, sessionID, batch)
	})
	m.streams.arm(sessionID, batch, t)
}

// fireGuard is what the timer does, kept apart from the timer so the guard's
// behaviour has one definition and a test can run it without waiting out the
// five minutes.
//
// The promise is filed by the ledger and only if these words landed. A guard
// whose frame and fallback both fail has promised the user nothing, so there is
// nothing to keep: the run's real ending finds no promise, chases the binding
// row, and tells the user what happened directly.
func (m *TypingIndicatorManager) fireGuard(ctx context.Context, sessionID pgtype.UUID, batch engine.RunBatchID) {
	m.streams.sayEnding(sessionID, byBatch(batch), roundContinues, nil,
		func(t roundTurn) (roundAddress, error) {
			if !t.HasBubble {
				return roundAddress{}, errNothingToSay
			}
			return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyStillWorking, "window expiring")
		})
}

// writeClosing seals one bubble with text and reports whether the words reached
// a sender. Which bubble was decided by the ledger, which consumed the handle
// under its own lock — that is what makes two closers racing produce one
// closing frame.
//
// A closing frame that cannot go out falls back to a plain message, the same
// way the answer does in outbound.go. The words matter more here than there:
// streamCopyFailed is the only "that run did not go through" WeCom ever
// produces, so a frame lost to a reconnect window would otherwise leave the
// user with a spinner and no explanation that would ever arrive. The addressing
// comes off the handle, captured at ingest, because by now the binding row may
// point at a different chat.
//
// Both routes failing is what the returned error is for. Under I3 the ledger
// then records nothing, so this run's ending is still owed and the next
// publisher of it — a sweeper tick, WeCom's own redelivery — says it instead of
// finding it filed as already said.
func (m *TypingIndicatorManager) writeClosing(ctx context.Context, sessionID pgtype.UUID, h streamHandle, text, why string) error {
	err := m.senders.stream(ctx, h, text, true)
	if err == nil {
		return nil
	}
	m.log.WarnContext(ctx, "wecom typing: closing frame failed, saying it as a new message",
		"chat_session_id", util.UUIDToString(sessionID),
		"reason", why, "unusable", streamUnusable(err), "error", err)
	sendErr := m.senders.sendTextCtx(ctx, h.InstallationID, h.ChatID, h.ChatType, text)
	if sendErr != nil {
		m.log.WarnContext(ctx, "wecom typing: the fallback message was unsendable too",
			"chat_session_id", util.UUIDToString(sessionID), "reason", why, "error", sendErr)
	}
	return sendErr
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
