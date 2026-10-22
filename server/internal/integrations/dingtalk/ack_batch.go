package dingtalk

import (
	"bytes"
	"context"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type reactionInputQueries interface {
	GetChatMessage(context.Context, pgtype.UUID) (db.ChatMessage, error)
}

// Register a receipt against persisted input ownership, not a second debounce
// timer. Unsealed inputs in one context form the pending batch; after sealing,
// task_id separates that batch from the next one in the same conversation.
// Provider requests and database reads run outside the state lock.
func (n *ackNotifier) prepareIngest(ctx context.Context, key string, state *ackState) ([]*ackState, bool) {
	for ctx.Err() == nil {
		n.mu.Lock()
		if n.settledInputs[state.inputID] {
			n.mu.Unlock()
			return nil, false
		}
		previous := append([]*ackState(nil), n.active[key]...)
		candidates := make([]*ackState, 0, len(previous))
		for _, existing := range previous {
			if existing.inst.ID == state.inst.ID && existing.msg.MessageID == state.msg.MessageID {
				n.mu.Unlock()
				return nil, false
			}
			if existing.inst.ID == state.inst.ID {
				candidates = append(candidates, existing)
			}
		}
		n.mu.Unlock()

		var superseded []*ackState
		if n.inputs != nil {
			if !state.inputID.Valid {
				return nil, false
			}
			current, err := n.inputs.GetChatMessage(ctx, state.inputID)
			if err != nil || !current.ChannelIngested || current.Role != "user" || current.ChatSessionID != state.sessionID {
				n.logger.WarnContext(ctx, "dingtalk reaction: accepted input unavailable", "error", err)
				return nil, false
			}
			older := false
			for _, candidate := range candidates {
				input, err := n.inputs.GetChatMessage(ctx, candidate.inputID)
				if err != nil {
					n.logger.WarnContext(ctx, "dingtalk reaction: batch input unavailable", "error", err)
					return nil, false
				}
				if !sameReactionBatch(current, input) {
					continue
				}
				// Match ListChatInputMessages ordering, including its UUID tie-break.
				if input.CreatedAt.Time.After(current.CreatedAt.Time) || (input.CreatedAt.Time.Equal(current.CreatedAt.Time) && bytes.Compare(input.ID.Bytes[:], current.ID.Bytes[:]) > 0) {
					older = true
				}
				superseded = append(superseded, candidate)
			}
			latest, err := n.inputs.GetChatMessage(ctx, state.inputID)
			if err != nil {
				return nil, false
			}
			// Sealing can commit between the reads above. Retry classification
			// before changing any visible anchor when the current owner moved.
			if latest.TaskID != current.TaskID || latest.ChannelContextRevision != current.ChannelContextRevision {
				continue
			}
			if older {
				return nil, false
			}
		}

		n.mu.Lock()
		if n.settledInputs[state.inputID] {
			n.mu.Unlock()
			return nil, false
		}
		if !slices.Equal(n.active[key], previous) {
			n.mu.Unlock()
			continue
		}
		for _, previous := range superseded {
			previous.settled = true
		}
		n.active[key] = append(n.active[key], state)
		n.mu.Unlock()
		return superseded, true
	}
	n.logger.WarnContext(ctx, "dingtalk reaction: input lookup cancelled; receipt skipped", "error", ctx.Err())
	return nil, false
}

func sameReactionBatch(a, b db.ChatMessage) bool {
	if a.ChatSessionID != b.ChatSessionID || !b.ChannelIngested || b.Role != "user" {
		return false
	}
	if a.TaskID.Valid || b.TaskID.Valid {
		return a.TaskID.Valid && a.TaskID == b.TaskID
	}
	return a.ChannelContextRevision == b.ChannelContextRevision
}

// Terminal events retire only their sealed input. Remember a bounded set of
// input IDs so a detached ingest hook that has not begun cannot re-add a receipt.
func (n *ackNotifier) onInputsSettled(ctx context.Context, sessionID pgtype.UUID, inputs []db.ChatMessage) {
	ids := make(map[pgtype.UUID]bool, len(inputs))
	for _, input := range inputs {
		if input.ID.Valid && input.ChannelIngested {
			ids[input.ID] = true
		}
	}
	key := util.UUIDToString(sessionID)
	n.mu.Lock()
	for id := range ids {
		n.rememberSettledInput(id)
	}
	var settled, remaining []*ackState
	for _, state := range n.active[key] {
		if ids[state.inputID] {
			state.settled = true
			settled = append(settled, state)
		} else {
			remaining = append(remaining, state)
		}
	}
	if len(remaining) == 0 {
		delete(n.active, key)
	} else {
		n.active[key] = remaining
	}
	n.mu.Unlock()
	n.recallStates(ctx, settled)
}

// Caller holds n.mu. Both task termination and archive share the same bounded
// input fence so re-delivery of a known hook cannot recreate its receipt.
func (n *ackNotifier) rememberSettledInput(id pgtype.UUID) {
	if !id.Valid || n.settledInputs[id] {
		return
	}
	if n.settledInputs == nil {
		n.settledInputs = make(map[pgtype.UUID]bool)
	}
	if len(n.settledOrder) == maxReplySources {
		delete(n.settledInputs, n.settledOrder[n.settledNext])
		n.settledOrder[n.settledNext] = id
		n.settledNext = (n.settledNext + 1) % maxReplySources
	} else {
		n.settledOrder = append(n.settledOrder, id)
	}
	n.settledInputs[id] = true
}
