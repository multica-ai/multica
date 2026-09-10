package dingtalk

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const ackCleanupTimeout = 5 * time.Second

type ackState struct {
	inst      engine.ResolvedInstallation
	msg       channel.InboundMessage
	settled   bool
	attempted bool
	inputID   pgtype.UUID
	sessionID pgtype.UUID
}

// ackNotifier keeps a local provider anchor for each input batch. Persisted
// input ownership identifies batches; provider coordinates come only from the
// accepted-ingest cache. A restart, cross-process event, or provider failure can
// still leave a reaction behind. Reaction failures never reject input or prevent
// a reply. Done is separate:
// the outbound sender calls OnReplyDelivered only after every answer chunk lands.
type ackNotifier struct {
	client        *Client
	decrypt       Decrypter
	logger        *slog.Logger
	mu            sync.Mutex
	active        map[string][]*ackState
	inputs        reactionInputQueries
	settledInputs map[pgtype.UUID]bool
	settledOrder  []pgtype.UUID
	settledNext   int
	sendReaction  func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error
}

var _ engine.TypingNotifier = (*ackNotifier)(nil)

func NewAckNotifier(client *Client, decrypt Decrypter, logger *slog.Logger, inputs reactionInputQueries) *ackNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = NewClient(nil, "")
	}
	return &ackNotifier{client: client, decrypt: decrypt, logger: logger, inputs: inputs, active: make(map[string][]*ackState)}
}

func (n *ackNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	if !sessionID.Valid || msg.MessageID == "" || msg.Source.ChatID == "" {
		return
	}
	key := util.UUIDToString(sessionID)
	// Keep only reaction coordinates, not callback bodies or session webhooks.
	state := &ackState{inst: inst, msg: channel.InboundMessage{MessageID: msg.MessageID, Source: msg.Source},
		inputID: n.client.replyInputFor(inst.ID, msg), sessionID: sessionID}
	previous, accepted := n.prepareIngest(ctx, key, state)
	if !accepted {
		return
	}
	n.recallStates(ctx, previous)
	n.mu.Lock()
	alreadySettled := state.settled
	if !alreadySettled {
		state.attempted = true
	}
	n.mu.Unlock()
	if alreadySettled {
		return
	}
	n.react(ctx, state.inst, state.msg, emotionAcknowledged, false)
	// A clear may win while the add is in flight. Give that completed add one
	// bounded recall; this does not require shared batch/generation identities.
	n.mu.Lock()
	settled := state.settled
	n.mu.Unlock()
	if settled {
		n.recallStates(ctx, []*ackState{state})
	}
}

// OnSettled is the shared router's failed-flush hook. That hook carries only a
// session ID, so it cannot distinguish overlapping failed/pending generations.
// Task terminal events instead use onInputsSettled with their owned input IDs.
// This hook never marks input Done.
func (n *ackNotifier) OnSettled(ctx context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	n.mu.Lock()
	states := n.active[key]
	delete(n.active, key)
	for _, state := range states {
		state.settled = true
	}
	n.mu.Unlock()
	n.recallStates(ctx, states)
}

// Archive does not need a task to retire locally registered receipts. Mark each
// known input settled before recalling it, including adds still in flight. A
// restored agent can immediately accept new inputs; there is no agent-wide timer.
func (n *ackNotifier) onAgentArchived(ctx context.Context, agentID pgtype.UUID) {
	if !agentID.Valid {
		return
	}
	n.mu.Lock()
	var retired []*ackState
	for key, states := range n.active {
		var remaining []*ackState
		for _, state := range states {
			if state.inst.AgentID == agentID {
				state.settled = true
				n.rememberSettledInput(state.inputID)
				retired = append(retired, state)
			} else {
				remaining = append(remaining, state)
			}
		}
		if len(remaining) == 0 {
			delete(n.active, key)
		} else {
			n.active[key] = remaining
		}
	}
	n.mu.Unlock()
	n.recallStates(ctx, retired)
}

func (n *ackNotifier) recallStates(ctx context.Context, states []*ackState) {
	if len(states) == 0 {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackCleanupTimeout)
	defer cancel()
	for _, state := range states {
		n.mu.Lock()
		attempted := state.attempted
		n.mu.Unlock()
		if !attempted || n.react(cleanup, state.inst, state.msg, emotionAcknowledged, true) == nil {
			n.removeRecalledState(state)
		}
	}
}

func (n *ackNotifier) removeRecalledState(state *ackState) {
	key := util.UUIDToString(state.sessionID)
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, current := range n.active[key] {
		if current == state && state.settled {
			n.active[key] = append(n.active[key][:i], n.active[key][i+1:]...)
			if len(n.active[key]) == 0 {
				delete(n.active, key)
			}
			return
		}
	}
}

// OnReplyDelivered uses only the provider anchor captured during local ingest.
// A restart or cache miss skips Done. Delivery snapshot message IDs are not an
// attribution authority; SRC-001 is a separate shared-core defect.
func (n *ackNotifier) OnReplyDelivered(ctx context.Context, inst engine.ResolvedInstallation, inputMessageID pgtype.UUID) {
	source, ok := n.client.replySourceFor(inst.ID, inputMessageID)
	if !ok {
		return
	}
	n.react(ctx, inst, source.message, emotionDone, false)
}

func (n *ackNotifier) react(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, name string, recall bool) error {
	send := n.sendReaction
	if send == nil {
		send = n.realSendReaction
	}
	if err := send(ctx, inst, msg, name, recall); err != nil {
		n.logger.WarnContext(ctx, "dingtalk reaction failed", "message_id", msg.MessageID, "recall", recall, "error", err)
		return err
	}
	return nil
}

func (n *ackNotifier) realSendReaction(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, name string, recall bool) error {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, n.decrypt)
	if err != nil {
		return err
	}
	s := &sender{
		client: n.client, robotCode: creds.RobotCode,
		appKey: creds.AppKey, appSecret: creds.AppSecret,
	}
	return s.setEmojiReaction(ctx, reactionTargetFromMessage(msg), name, recall)
}
