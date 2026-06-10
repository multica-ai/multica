package wecom

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OutboundStreamStore persists wecom_outbound_stream rows when the WS
// connector opens a streaming ACK for an ingested message.
type OutboundStreamStore struct {
	queries *db.Queries
}

func NewOutboundStreamStore(queries *db.Queries) *OutboundStreamStore {
	if queries == nil {
		return nil
	}
	return &OutboundStreamStore{queries: queries}
}

type RecordOutboundStreamParams struct {
	InstallationID pgtype.UUID
	ChatSessionID  pgtype.UUID
	ReqID          string
	StreamID       string
	WecomChatID    string
	WecomChatType  string
}

func (s *OutboundStreamStore) RecordStreaming(ctx context.Context, p RecordOutboundStreamParams) error {
	if s == nil || s.queries == nil {
		return nil
	}
	_, err := s.queries.CreateWecomOutboundStream(ctx, db.CreateWecomOutboundStreamParams{
		InstallationID: p.InstallationID,
		ChatSessionID:  p.ChatSessionID,
		TaskID:         pgtype.UUID{},
		ReqID:          p.ReqID,
		StreamID:       p.StreamID,
		WecomChatID:    p.WecomChatID,
		WecomChatType:  p.WecomChatType,
		Status:         StreamStatusStreaming,
	})
	return err
}
