package lark

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InboundVerifier persists evidence that this binding received a message over WS.
// It says nothing about ongoing liveness, routing, execution, or reply delivery.
type InboundVerifier interface {
	RecordInboundReceipt(context.Context, Installation, InboundMessage) error
}

func validReceiptMessage(inst Installation, msg InboundMessage) bool {
	if msg.EventType != "im.message.receive_v1" || msg.EventID == "" ||
		inst.AppID == "" || msg.AppID != inst.AppID || msg.MessageID == "" ||
		msg.ChatID == "" || msg.SenderOpenID == "" || msg.MessageType == "" ||
		(msg.ChatType != ChatTypeP2P && msg.ChatType != ChatTypeGroup) {
		return false
	}
	var content map[string]json.RawMessage
	return json.Unmarshal([]byte(msg.Content), &content) == nil && content != nil
}

func (s *InstallationService) RecordInboundReceipt(ctx context.Context, snapshot Installation, msg InboundMessage) error {
	if !snapshot.ID.Valid || !validReceiptMessage(snapshot, msg) {
		return nil
	}
	// The factory holds the installation ID and config snapshot, but workspace
	// identity is resolved by the router. Resolve it here as well, then fence the
	// write atomically against a rebind/revoke racing this read.
	current, err := s.queries.GetLarkInstallation(ctx, snapshot.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.AppID != snapshot.AppID || current.BindingGeneration != snapshot.BindingGeneration ||
		current.Status != string(InstallationActive) || current.FirstMessageReceivedAt != nil {
		return nil
	}
	return s.queries.Queries.RecordFeishuInboundReceipt(ctx, db.RecordFeishuInboundReceiptParams{
		ID:                snapshot.ID,
		WorkspaceID:       current.WorkspaceID,
		AppID:             snapshot.AppID,
		BindingGeneration: snapshot.BindingGeneration,
	})
}
