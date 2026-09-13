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
//     good. So every way a turn can end — answered, failed, cancelled, never
//     started — writes a closing frame carrying visible text into the bubble
//     while there is one.
//   - OnSettled is not the normal ending. As on the other platforms the Router
//     only calls it when the flush produced no task run; the answer closes the
//     bubble from the chat-done subscriber in outbound.go, which is the only
//     place that has the answer to close it with.
//
// The bubble is a CACHE and nothing more (stream_store.go). A closer that
// finds one writes into it; one that does not says its words as an ordinary
// message where the words are worth saying — a failed run's notice — and stays
// silent where they are not. Nothing is owed on the strength of a bubble that
// is gone.
//
// TWO FRAMES PER ROUND, and no more: the opening one that paints the spinner,
// and the closing one that replaces it with the answer. The stream's own
// ten-minute window is counted from the opening frame and is not extended by
// anything written in between (streamMaxAge), so a run that outlives it loses
// its bubble and answers as an ordinary message — one message, whole. Filling
// the bubble in while the run is going, and carrying a long run over onto a
// fresh stream, are a separate layer on top of this one.

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The ways a streaming reply ends in something other than an answer live in
// the copy pack as copyPack.Stream* (strings.go), and each closer names the
// one it wants rather than a string: the bubble's language is a property of
// the reader, and by the time a closer runs the only thing that still knows
// who that is, is the handle the round was opened with.

// The same endings spelled in the compile-time default, for the tests that
// assert what a bubble was sealed with. A closer never reads these — it reads
// its round's own pack — but a deployment that configures no second language
// produces exactly these words, so they are what those tests pin.
var (
	streamCopyNoReply    = copyFor(DefaultLocale).StreamNoReply
	streamCopyMerged     = copyFor(DefaultLocale).StreamMerged
	streamCopyNotStarted = copyFor(DefaultLocale).StreamNotStarted
	streamCopyFailed     = copyFor(DefaultLocale).StreamFailed
	streamCopyCancelled  = copyFor(DefaultLocale).StreamCancelled
)

// streamCloseTimeout bounds a closing frame written from a bus subscriber,
// which has no caller's context to inherit.
const streamCloseTimeout = 10 * time.Second

// taskLookup resolves a task id to the chat session it belongs to. Both
// publishers of task:failed stamp the session whenever the task row has one,
// so this is the fallback for a payload that does not, and the row the round
// matcher reads to resolve an auto-retry clone. *db.Queries satisfies it.
type taskLookup interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
}

// taskOrigin is the task row plus where the run's input came from. The typing
// indicator needs both halves: the row to resolve an auto-retry clone and to
// recover a session no publisher put on the event, and the provenance stamp to
// decide whether a failed run's notice belongs in the WeCom room at all.
// outbound.go asks the same stamp about an answer, through its own
// outboundQueries. Kept apart from taskLookup because the round matcher only
// ever needs the row. *db.Queries satisfies it.
type taskOrigin interface {
	taskLookup
	engine.ChannelProvenanceQueries
}

// TypingIndicatorManager opens a streaming bubble per round when messages are
// ingested and owns each one until something closes it.
type TypingIndicatorManager struct {
	senders *sendersRegistry
	streams *streamStore
	tasks   taskOrigin
	// deliveries finds the chat a failed run's notice goes to when the round
	// has no bubble left: the same delivery row the answer is addressed by
	// (outbound.go, taskAddress).
	deliveries deliveryLookup
	// languages resolves a destination to the language its bubble is closed
	// in (language.go). nil closes every bubble in the deployment's.
	languages languageLookup
	log       *slog.Logger
}

// TypingIndicatorConfig wires the manager. Senders and Streams are the same
// two instances the outbound subscriber holds — the bubble is opened on one
// side of the process and closed on the other.
type TypingIndicatorConfig struct {
	Senders *sendersRegistry
	Streams *streamStore
	Logger  *slog.Logger

	// Tasks answers where a run's input came from, and resolves a task id to
	// its chat session for a task:failed that carries none. Nil leaves the
	// origin question unanswerable, so every failed run this process holds no
	// round for is refused rather than announced — see failureBelongsOnWecom
	// for what this manager does when it cannot ask.
	Tasks taskOrigin

	// Deliveries finds the chat a failed run was asked in when its round has
	// no bubble left — a run that failed after this process was restarted, or
	// after a bubble that was never painted — off the task's own delivery
	// row, the same one the answer is addressed by. It is also the ownership
	// test: a task:failed for a run with no WeCom delivery row is another
	// channel's and costs nothing more. Nil keeps the failure notice to the
	// rounds this process holds a bubble for.
	Deliveries deliveryLookup

	// Languages resolves the chat a bubble was opened in to the language it is
	// closed in: a 1:1 reads the asker's own Multica profile, a room the
	// deployment's. Nil puts every bubble on the deployment's.
	Languages languageLookup
}

var _ engine.TypingNotifier = (*TypingIndicatorManager)(nil)

// NewTypingIndicator builds the manager.
func NewTypingIndicator(cfg TypingIndicatorConfig) *TypingIndicatorManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &TypingIndicatorManager{
		senders:    cfg.Senders,
		streams:    cfg.Streams,
		tasks:      cfg.Tasks,
		deliveries: cfg.Deliveries,
		languages:  cfg.Languages,
		log:        logger,
	}
}

// TypingIndicatorWiring reports which of the four dependencies a manager holds.
//
// Every one of them is optional and every one of them narrows the manager
// silently when it is missing: nothing panics, nothing logs, Register still
// subscribes, and the events still arrive — the closing frame just never gets
// written, which the user sees as a bubble that spins until the server's
// window runs out on it. That makes "is it wired" unfalsifiable from the
// outside, and a boot path that drops one looks exactly like a healthy one.
// This is the inspection point that makes it falsifiable.
//
// Languages is deliberately not among them: a manager without it still closes
// every bubble, in the deployment's language rather than the reader's.
type TypingIndicatorWiring struct {
	// Senders is the live WebSocket registry. Without it no closing frame and
	// no plain-message fallback can be written at all.
	Senders bool
	// Streams is the round store shared with the outbound subscriber. Without
	// it every handler returns on its first line, so no bubble is ever closed
	// by any ending.
	Streams bool
	// Tasks reads the run's input batch, which is what establishes that the
	// question was asked over WeCom and not by the installer in their own
	// browser. Without it every failed run this process holds no round for is
	// refused instead of announced, so a run that outlived its bubble tells
	// the user nothing. It also resolves an auto-retry clone to the round its
	// parent opened, and recovers the session for a task:failed carrying none.
	Tasks bool
	// Deliveries finds the chat a failed run was asked in when no bubble is
	// on file. Without it a run that fails after its bubble is gone (the
	// process restarted mid-run, or the opening frame was refused) tells the
	// user nothing.
	Deliveries bool
}

// Wiring reports the dependencies this manager was built with. For boot-wiring
// guards; it copies four booleans and hands out no references.
func (m *TypingIndicatorManager) Wiring() TypingIndicatorWiring {
	return TypingIndicatorWiring{
		Senders:    m.senders != nil,
		Streams:    m.streams != nil,
		Tasks:      m.tasks != nil,
		Deliveries: m.deliveries != nil,
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

	chatType := aibotChatTypeFromChannel(msg.Source.ChatType)
	h := streamHandle{
		ReqID:          wm.ReqID,
		StreamID:       newStreamID(),
		InstallationID: inst.ID,
		ChatID:         chatID,
		ChatType:       chatType,
		// Resolved here, while the asker is still in hand. Every closer runs
		// later, from an event that names a task and nobody else, long after
		// this goroutine is gone.
		Locale: localeFor(ctx, m.languages, inst.ID, chatType, msg.Source.SenderID),
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
	// on the user's screen. Dropping the handle there leaves nothing that
	// could ever close it.
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
	// Past every path that gives the handle back. One bubble is on screen, or
	// will be when its stream id is next written, and from here something owes
	// it an ending — see RecordStreamOpened.
	m.senders.recordOpened()
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
//
// No bubble, nothing to say: the replier's notice is the whole of what the
// user is told, and there is no round left to address a second line to.
func (m *TypingIndicatorManager) OnSettled(ctx context.Context, sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.senders == nil || m.streams == nil || !sessionID.Valid {
		return
	}
	t, ok := m.streams.take(ctx, sessionID, byBatch(batch), nil)
	if !ok || !t.HasBubble {
		return
	}
	m.writeClosing(ctx, sessionID, t.Handle, copyFor(t.Handle.Locale).StreamNotStarted, "settled")
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
// Both publishers of task:failed stamp chat_session_id when the task row has
// one: service.taskEvent, which FailTask goes through, and the sweeper's own
// envelope in HandleFailedTasks (recover-orphans and the daemon heartbeat
// timeout come down that path). So the session is normally on the event, and
// sessionFor's read of it off the task row is a fallback for a payload neither
// publisher produces today. It is kept because a bubble left spinning is a
// failure nobody reports.
//
// The bubble is not the whole of it. A round that lost its bubble — a restart
// mid-run, an opening frame the server refused, a stream that ran out its
// window — still has an asker, and this notice is the only "that run did not
// go through" WeCom ever produces: the replier speaks for needs_binding,
// offline, archived and issue_created and for nothing else. So a round with no
// bubble is addressed by the task's own delivery row, the way the answer is.
//
// A repeat of one run's failure — the sweeper republishing it, the second
// publisher — is a second notice. Nothing here remembers having said it: the
// bubble is a cache, and a cache holds no account of what went out.
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
	if m.deliveries == nil && !m.streams.holding() {
		// Nothing on file, and no way to find a chat: no reason to read a row
		// for someone else's run.
		return
	}
	sessionID, ok := m.sessionFor(e)
	if !ok {
		return // an issue / autopilot run, with no chat session and no bubble
	}
	taskID := taskIDFromEvent(e)

	// Everything this handler asks a database runs on the goroutine that
	// published the event, so it gets the subscriber's own budget rather than
	// the ten seconds a closing frame may spend on the wire. See
	// taskLookupTimeout.
	dbCtx, cancelDB := context.WithTimeout(context.Background(), taskLookupTimeout)
	defer cancelDB()

	// Where was this question asked? Asked BEFORE the bubble is taken, because
	// a gate placed after the take has already sealed a WeCom round's bubble
	// with a web run's ending. The answer path orders its own gate the same
	// way — see the block above the gate in outbound.go's processEvent.
	//
	// A round on this session's open list is local proof and costs nothing:
	// it was opened by a message this adapter ingested and named by the flush
	// that answered it. Everything else is decided by the database, cheapest
	// first. task:failed fires for every run in the deployment — Slack's,
	// Lark's, DingTalk's, the web UI's — so the delivery row is read before
	// the task row: a run with no WeCom route is another channel's and never
	// reaches the second read (TestAnotherChannelsFailureNeverReachesTheTaskRow).
	var bound roundAddress
	if !m.streams.has(sessionID, taskID) {
		if taskID != "" {
			addr, ours := m.addressForTask(dbCtx, taskID)
			if !ours {
				return
			}
			bound = addr
		}
		if !m.failureBelongsOnWecom(dbCtx, sessionID, taskID) {
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
	defer cancel()
	t, _ := m.rounds().take(ctx, sessionID, byTask(taskID))
	if t.HasBubble {
		if m.senders == nil {
			return
		}
		m.writeClosing(ctx, sessionID, t.Handle, failureText(e, t.Handle.Locale), "task failed")
		return
	}
	// No bubble to write into: the round was never painted, or its stream has
	// run out. The words still go to the chat that asked.
	if !bound.known() {
		found, ours := m.addressForTask(ctx, taskID)
		if !ours {
			return
		}
		bound = found
	}
	m.sayAsPlainMessage(ctx, sessionID, bound,
		failureText(e, localeFor(ctx, m.languages, bound.InstallationID, bound.ChatType, bound.ChatID)))
}

// failureText is what a failed run says in the chat.
//
// The platform's own redacted reason when it published one — that is the text
// the web transcript shows and the one #7952 established for this channel,
// and "上下文超出模型限制" tells a person what to do next where a generic line
// does not. The copy pack's StreamFailed is the fallback for a failure that
// arrived without a reason, and for retry_pending, which taskFailedContent
// reports as empty because an attempt the platform is already retrying is not
// an ending.
func failureText(e events.Event, l Locale) string {
	if reason := taskFailedContent(e.Payload); reason != "" {
		return reason
	}
	return copyFor(l).StreamFailed
}

// failureBelongsOnWecom asks where this run's input came from: the channel, or
// somewhere else? The engine makes the INSTALLER the creator of a group's
// chat_session, so that session appears in their own Multica chat list and they
// can ask it something in a browser. Both runs fail the same way, on the same
// bus, carrying the same session — nothing in the event says which surface
// asked.
//
// The answer has the same exposure, and outbound.go makes the same
// engine.TaskInputIsChannelIngested call before it takes the round. The two
// endings of a run are decided by one stamp.
//
// This is an authorization check on writing into somebody else's group chat,
// so uncertainty is not permission. A lookup that did not answer is not
// evidence the question came from WeCom, and "one line of copy naming no
// question and no answer" still tells a room that activity it cannot see went
// wrong — the existence of the activity is the disclosure. So an origin that
// cannot be established refuses, and says so at WARN.
//
// That costs nothing on the case worth protecting, because that case has local
// evidence: a round still open has this run bound, and the caller reads that
// off the store before it gets here. So a WeCom round whose bubble is open is
// closed while the database is down, and it is only the runs this process
// holds no round for that have to produce a row to be spoken for.
func (m *TypingIndicatorManager) failureBelongsOnWecom(ctx context.Context, sessionID pgtype.UUID, taskID string) bool {
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
// task:failed, so without this the bubble spins until the server's window
// runs out on it. A session with several rounds open gets one closing frame
// per cancelled run, each on its own bubble, because the round is matched by
// the task id the flush bound to it.
//
// This handler is only ever as complete as its publishers. It sees a cancelled
// run when service.TaskService broadcasts task:cancelled for the row —
// CancelTask, CancelQueuedChatTasks for the follow-ups behind it, the
// agent-level and issue-level bulk cancels, and BroadcastCancelledTasks for the
// handlers that cancel inside a transaction. A cancel path that flips the row
// and publishes nothing is invisible here, and the bubble it strands has no
// other closer: the daemon's own completion arrives after the row is already
// cancelled, where CompleteAgentTask's status = 'running' guard matches nothing
// and the answer is discarded without a chat:done. Archiving an agent used to
// be exactly that path (handler.ArchiveAgent).
//
// Unlike a failure this does NOT go looking for an address when no bubble is
// on file. StreamFailed is the only "that run did not go through" WeCom ever
// produces, which is why a failure is worth chasing an address for; a
// cancellation was performed by the user, and chasing it would turn one
// "cancel all tasks" click into a message in every chat that agent serves —
// including sessions where WeCom never showed a bubble at all.
func (m *TypingIndicatorManager) handleTaskCancelled(e events.Event) {
	if m.streams == nil {
		return
	}
	// Anything on file at all, not just anything painted. A round bound to a
	// run whose opening frame is still in flight has no bubble yet; returning
	// here would leave it on the open list, and the frame landing afterwards
	// would paint a spinner with no ending left to close it — the cancel is
	// the last event this run produces. Retiring the round instead makes that
	// late paint a no-op.
	if !m.streams.holding() {
		return
	}
	sessionID, ok := m.sessionFor(e)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
	defer cancel()
	t, _ := m.rounds().take(ctx, sessionID, byTask(taskIDFromEvent(e)))
	if !t.HasBubble || m.senders == nil {
		return
	}
	m.writeClosing(ctx, sessionID, t.Handle, copyFor(t.Handle.Locale).StreamCancelled, "task cancelled")
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

func (m *TypingIndicatorManager) sayAsPlainMessage(ctx context.Context, sessionID pgtype.UUID, addr roundAddress, text string) error {
	if m.senders == nil {
		return errNoLiveConnection
	}
	err := m.senders.sendTextCtx(ctx, addr.InstallationID, addr.ChatID, addr.ChatType, text)
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: could not deliver a run's ending",
			"chat_session_id", util.UUIDToString(sessionID),
			"installation_id", util.UUIDToString(addr.InstallationID), "error", err)
	}
	return err
}

// addressForTask reads the chat a run was asked in off its delivery row — the
// same read outbound.go makes when an answer has no bubble to land in. A run
// with no WeCom delivery row is not ours to speak for: this subscriber sees
// every failed run on a shared bus, including Slack's and the web UI's. That
// makes it the ownership test as much as the address, which is why
// handleTaskFailed asks it before spending anything on the task row.
func (m *TypingIndicatorManager) addressForTask(ctx context.Context, taskID string) (roundAddress, bool) {
	if m.deliveries == nil {
		return roundAddress{}, false
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		return roundAddress{}, false
	}
	addr, skip, err := taskAddress(ctx, m.deliveries, id)
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: cannot find the chat a failed run belongs to",
			"task_id", taskID, "error", err)
		return roundAddress{}, false
	}
	if skip != "" || !addr.known() {
		// A reason means the turn was ours and is no longer deliverable; a zero
		// address with no reason means it was never ours — no delivery row, or
		// another platform's. Either way this subscriber says nothing, and
		// neither costs the task row a read.
		return roundAddress{}, false
	}
	return addr, true
}

// sessionFor finds the chat session behind a task lifecycle event.
//
// It is normally on the event itself. Both publishers of task:failed stamp it
// whenever the task row has one — service.taskEvent, and the sweeper's own
// envelope in HandleFailedTasks — and each sets the envelope field and the
// payload key together. The read off the task row below is a fallback for a
// payload neither publisher produces today; see the block comment above
// handleTaskFailed for why it is kept anyway.
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

// taskLookupTimeout is the whole database budget this subscriber may spend on
// somebody else's goroutine — the session lookup, the binding row and the
// origin gate together. The bus is synchronous: a task:failed subscriber runs
// on the goroutine that published the event, and a sweeper tick is not ours to
// hold while a loaded pool answers. streamCloseTimeout is the separate, longer
// budget for putting words on the wire once the decision has been made.
//
// A pool too slow to answer inside it costs a failure notice for a run this
// process holds no record of. The case worth protecting does not depend on it:
// a round with an open bubble is proved ours from memory and never reaches
// these queries — see handleTaskFailed.
const taskLookupTimeout = 800 * time.Millisecond

// taskIDFromEvent prefers the envelope's routing hint and falls back to the
// payload. ChatDonePayload matters most: broadcastChatDone sets no TaskID on
// the envelope, and on the in-process bus the payload stays typed — miss it
// and no answer ever finds its round.
func taskIDFromEvent(e events.Event) string {
	if e.TaskID != "" {
		return e.TaskID
	}
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		return p.TaskID
	case map[string]any:
		s, _ := p["task_id"].(string)
		return s
	}
	return ""
}

// writeClosing seals one bubble with text. Which bubble was decided by the
// store, which removed the round under its own lock — that is what makes two
// closers racing produce one closing frame.
//
// A closing frame that cannot go out falls back to a plain message, the same
// way the answer does in outbound.go. The words matter more here than there:
// StreamFailed is the only "that run did not go through" WeCom ever produces,
// so a frame refused for good would otherwise leave the user with a spinner
// and no explanation that would ever arrive. The addressing comes off the
// handle, captured at ingest, because by now the binding row may point at a
// different chat.
func (m *TypingIndicatorManager) writeClosing(ctx context.Context, sessionID pgtype.UUID, h streamHandle, text, why string) {
	err := m.streams.seal(ctx, m.senders, h, text)
	if err == nil {
		return
	}
	m.log.WarnContext(ctx, "wecom typing: closing frame failed, saying it as a new message",
		"chat_session_id", util.UUIDToString(sessionID),
		"reason", why, "unusable", streamUnusable(err), "error", err)
	if err := m.senders.sendTextCtx(ctx, h.InstallationID, h.ChatID, h.ChatType, text); err != nil {
		m.log.WarnContext(ctx, "wecom typing: the fallback message was unsendable too",
			"chat_session_id", util.UUIDToString(sessionID), "reason", why, "error", err)
	}
}

// sessionIDFromEvent recovers the chat session from a task lifecycle event.
//
// Every publisher on this path sets BOTH places for a chat run:
// service.broadcastChatDone stamps the envelope field and ChatDonePayload,
// service.taskEvent stamps the envelope field and the chat_session_id key, and
// the sweeper's own envelope does the same. So the payload read is a second
// look at the same value rather than some event type's only carrier — it costs
// one type switch and it is what keeps a re-serialized envelope working.
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
