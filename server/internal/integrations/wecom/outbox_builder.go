package wecom

// outbox_builder.go — the WeCom PayloadBuilder for the outbound reconciler.
// The reconciler finds terminal agent tasks on WeCom-bound sessions that never
// got a queue row (the producing replica died between finishing the task and
// enqueueing); this file resolves each one into the row that should have been
// written.
//
// It intentionally re-reads the binding and installation the candidate query
// already joined: the scan and this build are not one transaction, and the
// binding is where the delivery target lives, so a revoke or rebind in between
// must skip the candidate rather than address a stale chat.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// reconcileBuilderQueries is the generated-query surface the builder needs.
type reconcileBuilderQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetChatMessageByTaskAssistant(ctx context.Context, taskID pgtype.UUID) (db.ChatMessage, error)
}

// reconcileBuilder implements outbox.PayloadBuilder for WeCom.
type reconcileBuilder struct {
	q reconcileBuilderQueries
}

var _ outbox.PayloadBuilder = (*reconcileBuilder)(nil)

// NewReconcilePayloadBuilder builds the WeCom PayloadBuilder boot hands to
// outbox.NewReconciler. *db.Queries satisfies the query surface.
func NewReconcilePayloadBuilder(q reconcileBuilderQueries) outbox.PayloadBuilder {
	return &reconcileBuilder{q: q}
}

// SourceKinds are the task-derived kinds the realtime producer writes. Inbox
// notifications are absent on purpose: they are not derived from a task, so
// the task-window scan has no way to find a missing one.
func (b *reconcileBuilder) SourceKinds() []string {
	return []string{sourceKindChatDone, sourceKindTaskFailed}
}

// Build resolves one candidate into a queue row.
func (b *reconcileBuilder) Build(ctx context.Context, cand outbox.Candidate) (outbox.Request, bool, error) {
	binding, err := b.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: cand.ChatSessionID,
		ChannelType:   channelTypeWecom,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return outbox.Request{}, false, nil
		}
		return outbox.Request{}, false, fmt.Errorf("load chat binding: %w", err)
	}
	if binding.InstallationID != cand.InstallationID {
		// Rebound to a different installation since the scan. The reply belongs
		// to a conversation this target no longer owns.
		return outbox.Request{}, false, nil
	}

	inst, err := b.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return outbox.Request{}, false, nil
		}
		return outbox.Request{}, false, fmt.Errorf("load installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		return outbox.Request{}, false, nil
	}

	sourceKind, payload, ok, err := b.payloadFor(ctx, cand, inst)
	if err != nil || !ok {
		return outbox.Request{}, false, err
	}

	return outbox.Request{
		InstallationID: inst.ID,
		WorkspaceID:    inst.WorkspaceID,
		ChatSessionID:  binding.ChatSessionID,
		SourceKind:     sourceKind,
		SourceID:       util.UUIDToString(cand.TaskID),
		TargetChatID:   binding.ChannelChatID,
		TargetChatType: int16(aibotChatTypeFromChannel(channel.ChatType(binding.ChatType))),
		MsgType:        msgTypeMarkdown,
		Payload:        payload,
	}, true, nil
}

// payloadFor renders the payload document for a candidate's terminal status.
func (b *reconcileBuilder) payloadFor(ctx context.Context, cand outbox.Candidate, inst db.ChannelInstallation) (string, []byte, bool, error) {
	switch cand.TaskStatus {
	case "completed":
		msg, err := b.q.GetChatMessageByTaskAssistant(ctx, cand.TaskID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Completed with no assistant message: nothing to deliver.
				return "", nil, false, nil
			}
			return "", nil, false, fmt.Errorf("load assistant message: %w", err)
		}
		if strings.TrimSpace(msg.Content) == "" {
			return "", nil, false, nil
		}
		payload, err := json.Marshal(outboundPayload{Content: msg.Content})
		if err != nil {
			return "", nil, false, fmt.Errorf("marshal chat_done payload: %w", err)
		}
		return sourceKindChatDone, payload, true, nil

	case "failed":
		agentName := ""
		// Best effort: a missing agent row costs the notice its name, not the
		// notice itself.
		if agent, err := b.q.GetAgent(ctx, inst.AgentID); err == nil {
			agentName = agent.Name
		}
		reason := ""
		if cand.FailureReason.Valid {
			reason = cand.FailureReason.String
		}
		payload, err := json.Marshal(outboundPayload{
			Template:      templateTaskFailed,
			FailureReason: reason,
			AgentName:     agentName,
		})
		if err != nil {
			return "", nil, false, fmt.Errorf("marshal task_failed payload: %w", err)
		}
		return sourceKindTaskFailed, payload, true, nil

	default:
		return "", nil, false, nil
	}
}
