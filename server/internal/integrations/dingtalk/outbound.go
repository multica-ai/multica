package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the DingTalk outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	ListChatInputMessages(ctx context.Context, taskID pgtype.UUID) ([]db.ChatMessage, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers an agent's chat reply back to DingTalk — the outbound half
// of the round trip. On EventChatDone / EventTaskFailed it finds the DingTalk
// chat binding for the task's session and posts the reply (or failure notice)
// into the originating conversation. Sessions with no DingTalk binding are
// ignored, so it coexists with the Feishu and Slack subscribers on the shared
// event bus. Registered only when DingTalk is configured.
//
// On EventTaskCancelled it posts nothing through the binding: it withdraws the
// processing ack instead, addressed from what the notifier recorded when it made
// the promise. See withdrawProcessingAck.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	client  *Client
	ack     *ackNotifier
	logger  *slog.Logger
}

// NewOutbound builds the DingTalk outbound subscriber over the generated queries,
// the AppSecret decrypter, the shared token-caching Client, and the ack notifier
// the inbound side posts the processing message through. It must be the same
// notifier instance the resolver set was built with — that is the only record of
// which conversations are still owed a reply. A nil notifier leaves cancelled
// runs unannounced.
func NewOutbound(q outboundQueries, decrypt Decrypter, client *Client, ack *ackNotifier, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = NewClient(nil, "")
	}
	return &Outbound{q: q, decrypt: decrypt, client: client, ack: ack, logger: logger}
}

// Register subscribes to the three events that end a run. Task-failed and
// task-cancelled keep the DingTalk conversation consistent with the web
// transcript — without them the run ends in silence and the user is left
// staring at the "👀 On it" ack forever.
//
// DingTalk is the odd one out among the channel adapters. Slack and Lark put a
// reaction on the user's own message and take it off again; the classic robot
// API this adapter sends through exposes no reaction, so ack.go's indicator is
// a real, non-retractable message that promised a reply. Closing it can only
// mean posting a second message withdrawing that promise. A badge we remove was
// ours to remove; a message we post is not, so the cancel path posts only where
// an ack is outstanding and the cancelled run could be the one that made it.
//
// One cancel stays out of reach of all three subscriptions: archiving an agent
// cancels its tasks without broadcasting per row (handler/agent.go,
// ArchiveAgent), because agent:archived already invalidates every client's task
// list. Nothing about a refreshed task list withdraws a message already sitting
// in a DingTalk room, so archiving an agent mid-run still leaves the ack open.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
	bus.Subscribe(protocol.EventTaskCancelled, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck DingTalk HTTP call must not wedge
	// the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "dingtalk outbound: delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	taskID, sessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	if e.Type == protocol.EventTaskCancelled {
		return o.withdrawProcessingAck(ctx, taskID, sessionID)
	}
	if retryPendingFailure(e) {
		// The retry reports its own outcome, so this run is not over: nothing to
		// deliver, and the ack it made still stands.
		return nil
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either this was never a DingTalk session, or its binding is gone:
			// archiving removes the binding without cancelling what is running
			// (handler/chat.go). The second case still has to discharge the
			// promise. The run has ended, and a promise left on record is spent
			// by the next unrelated cancel in that conversation — which is the
			// false "no reply is coming" this whole path exists to avoid.
			//
			// The in-memory check comes first so a Feishu, Slack or web-only
			// session still costs one query rather than three.
			if o.ack != nil && o.ack.hasOutstandingAck(sessionID) {
				o.releaseIfThisRunWasAcked(ctx, taskID, sessionID)
			}
			return nil
		}
		return fmt.Errorf("lookup dingtalk chat binding: %w", err)
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}
	// This is the run the ack was made for and it has reported an ending, so the
	// promise is discharged here rather than after a successful send: an empty
	// completion, a revoked installation and a send that fails all end the run
	// just as finally as a delivered reply, and a promise left on record after
	// any of them would be withdrawn by some unrelated later cancel in the same
	// conversation.
	if o.ack != nil {
		o.ack.releaseOutstandingAck(sessionID)
	}
	content := eventContent(e)
	if content == "" {
		return nil // nothing to say (an empty completion, or a failure with no error text)
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	s := &sender{client: o.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	if _, err := s.send(ctx, outboundTarget(binding), content); err != nil {
		return fmt.Errorf("post dingtalk reply: %w", err)
	}
	return nil
}

// releaseIfThisRunWasAcked discharges the session's promise when the run that
// just ended is the one the promise was made for. It asks the same question the
// delivery path asks below, asked again here because the binding that path
// needs is gone and it returned early.
//
// Failures leave the promise alone. It expires on ackPromiseMaxAge either way,
// and guessing on an unreadable origin is how a web run comes to discharge a
// channel run's promise.
func (o *Outbound) releaseIfThisRunWasAcked(ctx context.Context, taskID, sessionID pgtype.UUID) {
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		o.logger.WarnContext(ctx, "dingtalk: could not load the task behind an unbound ending", "error", err)
		return
	}
	acked, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		o.logger.WarnContext(ctx, "dingtalk: could not classify the origin of an unbound ending", "error", err)
		return
	}
	if !acked {
		return
	}
	o.ack.releaseOutstandingAck(sessionID)
}

// withdrawProcessingAck closes the processing ack for a cancelled run.
//
// Two questions have to agree before anything is posted, and neither one can be
// answered from the other side.
//
// Is anything owed in this conversation at all? Only the ack notifier knows. An
// ack is posted by the inbound path alone, into a real room, so nothing on
// record means nothing is owed and the cancel stays silent. Taking the promise
// is also what holds a bulk cancel to one message: the event arrives once per
// cancelled row and the promise is gone after the first. It is taken before the
// send, so a failed send cannot re-open it — this message cannot be unsent, and
// posting it twice is worse than not posting it. The failure is logged by the
// caller.
//
// Is this particular cancelled task the run that promise belongs to? Only the
// task can say, and the promise cannot: it is keyed by session, and a session
// carries more than one run whenever the user opens the same conversation in
// Multica and sends a turn there while the channel's turn is still working.
// Stopping that browser turn must not tell the room its answer is not coming,
// because it is — from the run still going. So a task whose input batch holds
// user messages, none of them channel ingested, is a web run and is left alone,
// promise and all.
//
// taskMayOwnProcessingAck covers the case between those two: a batch with no
// user messages in it is not a verdict either way, so a task that cannot be
// classified defers to the promise, which is the more direct evidence anyway.
// The known ways to arrive empty mostly do mean the notice is owed — a channel
// task can own an empty batch (the #6611 shape), and deleting a session
// cascades its messages away before the cancels are broadcast — but the list is
// not closed, and one member of it cuts the other way: the claim guard
// (handler/daemon.go) cancels a task whose user text cannot be read, and that
// task may be a web run. Deferring to the promise then posts the notice for a
// run the room never asked about. The guard fires only on genuinely corrupt
// state by its own account, so this is a narrow, deliberate residue rather than
// a case the classifier handles.
//
// The installation comes from the promise instead of the session's binding for
// the same session-delete reason: the binding is dropped in the transaction
// that cancels the tasks, and the cancels are broadcast after it commits.
func (o *Outbound) withdrawProcessingAck(ctx context.Context, taskID, sessionID pgtype.UUID) error {
	if o.ack == nil || !o.ack.hasOutstandingAck(sessionID) {
		// No promise here, so nothing is owed and no query is worth spending:
		// most cancelled runs have nothing to do with DingTalk.
		return nil
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	mine, err := o.taskMayOwnProcessingAck(ctx, task)
	if err != nil {
		return err
	}
	if !mine {
		return nil // a web run on a bound session: its cancel is not this room's news
	}
	promise, ok := o.ack.takeOutstandingAck(sessionID)
	if !ok {
		return nil // another cancel for the same conversation got here first
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          promise.installationID,
		ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and cancel
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	s := &sender{client: o.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	if _, err := s.send(ctx, promise.target, ackCancelledText); err != nil {
		return fmt.Errorf("post dingtalk cancellation notice: %w", err)
	}
	return nil
}

// taskMayOwnProcessingAck reports whether a cancelled task could be the run an
// outstanding ack was posted for.
//
// TaskInputIsChannelIngested is the same origin question the reply path asks,
// but its "no" is two answers in one: a batch of web-sent messages, and a batch
// with nothing in it. The first is a real web run. The second is not a verdict —
// it is the query having nothing to read, which is what a task cancelled for an
// empty input batch looks like, and what every task looks like once its session
// delete has cascaded the messages away. Only the first may silence the notice.
func (o *Outbound) taskMayOwnProcessingAck(ctx context.Context, task db.AgentTaskQueue) (bool, error) {
	channelIngested, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return false, fmt.Errorf("classify task input origin: %w", err)
	}
	if channelIngested {
		return true, nil
	}
	// Reached only with a valid ChatInputTaskID — TaskInputIsChannelIngested
	// answers true for a task that owns no batch.
	batch, err := o.q.ListChatInputMessages(ctx, task.ChatInputTaskID)
	if err != nil {
		return false, fmt.Errorf("load task input batch: %w", err)
	}
	return len(batch) == 0, nil
}

// retryPendingFailure reports a task:failed broadcast that an automatic retry
// will follow. taskFailedFields omits the error text while a retry is pending
// precisely so consumers can tell an intermediate attempt from the run's real
// ending.
func retryPendingFailure(e events.Event) bool {
	if e.Type != protocol.EventTaskFailed {
		return false
	}
	m, ok := e.Payload.(map[string]any)
	if !ok {
		return false
	}
	pending, _ := m["retry_pending"].(bool)
	return pending
}

// eventContent extracts the deliverable text from an EventChatDone payload
// (typed, or its map form after a serialization round trip) or an
// EventTaskFailed payload. Empty means stay silent.
//
// For task-failed the text mirrors the web transcript's failure chat_message:
// the broadcast's `error` field carries the same redacted failure text and is
// omitted while an auto-retry is pending (the retry attempt reports its own
// outcome), so error-present means deliverable.
//
// EventTaskCancelled never reaches here: it carries no text of its own —
// A cancel is published with the task row's ids and status and nothing else —
// and its message is the fixed withdrawal in withdrawProcessingAck.
func eventContent(e events.Event) string {
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if e.Type == protocol.EventTaskFailed {
			if retryPendingFailure(e) {
				return ""
			}
			if s, _ := p["error"].(string); s != "" {
				return "⚠️ " + s
			}
			return ""
		}
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

func taskAndSessionFromEvent(e events.Event) (taskID, sessionID pgtype.UUID, ok bool) {
	if e.TaskID != "" {
		_ = taskID.Scan(e.TaskID)
	}
	if e.ChatSessionID != "" {
		_ = sessionID.Scan(e.ChatSessionID)
	}
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		if !taskID.Valid {
			_ = taskID.Scan(p.TaskID)
		}
		if !sessionID.Valid {
			_ = sessionID.Scan(p.ChatSessionID)
		}
	case map[string]any:
		if !taskID.Valid {
			if raw, _ := p["task_id"].(string); raw != "" {
				_ = taskID.Scan(raw)
			}
		}
		if !sessionID.Valid {
			if raw, _ := p["chat_session_id"].(string); raw != "" {
				_ = sessionID.Scan(raw)
			}
		}
	}
	return taskID, sessionID, taskID.Valid
}
