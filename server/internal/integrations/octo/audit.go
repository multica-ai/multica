package octo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AuditDropParams describes a dropped inbound event. It deliberately carries no
// message body — the audit log records routing / identity / drop_reason only.
type AuditDropParams struct {
	InstallationID pgtype.UUID
	ChannelID      ChannelID
	MessageID      string
	Reason         DropReason
}

// AuditLogger records dropped inbound events. An interface so the dispatcher can
// be unit-tested without a database.
type AuditLogger interface {
	RecordDrop(ctx context.Context, p AuditDropParams) error
}

// dbAuditLogger is the production AuditLogger backed by generated queries.
type dbAuditLogger struct {
	queries *db.Queries
}

// NewAuditLogger constructs the production AuditLogger.
func NewAuditLogger(queries *db.Queries) AuditLogger {
	return &dbAuditLogger{queries: queries}
}

func (l *dbAuditLogger) RecordDrop(ctx context.Context, p AuditDropParams) error {
	return l.queries.RecordOctoInboundDrop(ctx, db.RecordOctoInboundDropParams{
		DropReason:     string(p.Reason),
		InstallationID: p.InstallationID,
		OctoChannelID:  textOrNull(string(p.ChannelID)),
		OctoMessageID:  textOrNull(p.MessageID),
	})
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
