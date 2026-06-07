package lark

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dbAuditLogger is the concrete AuditLogger implementation backed by
// lark_inbound_audit. It deliberately holds no caches and no
// indirection — the RecordLarkInboundDrop query rejects any column
// that could carry a message body, and this struct does too.
type dbAuditLogger struct {
	queries *db.Queries
}

// NewAuditLogger returns an AuditLogger that writes drop events to the
// lark_inbound_audit table. The interface signature does not accept a
// message body, mirroring §4.7 of the design (non-content audit only).
func NewAuditLogger(queries *db.Queries) AuditLogger {
	return &dbAuditLogger{queries: queries}
}

func (l *dbAuditLogger) RecordDrop(ctx context.Context, p AuditDropParams) error {
	return l.queries.RecordLarkInboundDrop(ctx, db.RecordLarkInboundDropParams{
		EventType:      p.EventType,
		DropReason:     string(p.Reason),
		InstallationID: p.InstallationID,
		LarkChatID:     textIfNonEmpty(string(p.ChatID)),
		LarkEventID:    textIfNonEmpty(p.LarkEventID),
		LarkMessageID:  textIfNonEmpty(p.LarkMessageID),
	})
}

// textIfNonEmpty returns a Valid pgtype.Text for non-empty strings and
// a NULL pgtype.Text otherwise. Avoids storing literal empty strings
// in the audit table, which would mask the difference between "the
// event lacked this field" and "the field was deliberately empty".
func textIfNonEmpty(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
