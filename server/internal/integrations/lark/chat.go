package lark

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// The chat-session ensure/append/`/issue` machinery that used to live here (and
// in chat_service.go) has moved to the channel-agnostic engine.ChatSession
// (MUL-3516), which Feishu now consumes via NewFeishuResolverSet — there is no
// Feishu-specific session service anymore. What remains is the inbound drop
// audit seam, still Feishu-shaped.

// AuditLogger records dropped inbound events to lark_inbound_audit. The
// interface deliberately does not accept a message body — see the drop-audit
// policy in MUL-2671 §4.7.
type AuditLogger interface {
	RecordDrop(ctx context.Context, p AuditDropParams) error
}

type AuditDropParams struct {
	InstallationID pgtype.UUID // may be invalid for installation-less events
	ChatID         ChatID
	EventType      string
	LarkEventID    string
	LarkMessageID  string
	Reason         DropReason
}
