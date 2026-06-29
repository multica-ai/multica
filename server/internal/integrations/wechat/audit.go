package wechat

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AuditQueries interface {
	CreateWechatInboundAudit(ctx context.Context, arg db.CreateWechatInboundAuditParams) error
}

type AuditLogger struct {
	queries AuditQueries
}

func NewAuditLogger(queries AuditQueries) *AuditLogger {
	return &AuditLogger{queries: queries}
}

type AuditDropParams struct {
	InstallationID  string
	EventType       string
	WechatMessageID string
	ChatID          string
	Reason          DropReason
}

func (a *AuditLogger) RecordDrop(ctx context.Context, p AuditDropParams) error {
	var instID pgtype.UUID
	if p.InstallationID != "" {
		_ = instID.Scan(p.InstallationID)
	}

	return a.queries.CreateWechatInboundAudit(ctx, db.CreateWechatInboundAuditParams{
		InstallationID:  instID,
		WechatChatID:    pgtype.Text{String: p.ChatID, Valid: p.ChatID != ""},
		EventType:       p.EventType,
		WechatMessageID: pgtype.Text{String: p.WechatMessageID, Valid: p.WechatMessageID != ""},
		DropReason:      string(p.Reason),
	})
}
