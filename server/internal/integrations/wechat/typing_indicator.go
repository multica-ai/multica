package wechat

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TypingIndicatorManager is a placeholder for the WeChat "processing" indicator.
// Unlike Slack/Feishu, the iLink protocol's typing surface requires a
// getconfig round-trip to obtain a typing_ticket before sendtyping can be
// called, so the MVP defers it (the ResolverSet is built with typing=nil, which
// the typed-nil guard turns into a disabled indicator). This type exists so the
// resolver wiring compiles and so a later phase can flesh it out without
// touching the resolver signature. Its methods are no-ops.
type TypingIndicatorManager struct {
	log *slog.Logger
}

// NewTypingIndicatorManager builds the (no-op) manager. Kept for API symmetry
// with the Slack/Lark adapters; not wired in the MVP.
func NewTypingIndicatorManager(q TypingIndicatorQueries, decrypt Decrypter, logger *slog.Logger) *TypingIndicatorManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &TypingIndicatorManager{log: logger}
}

// TypingIndicatorQueries is the query surface a real implementation would need.
// Declared (not satisfied here) so a future implementation drops in without
// reshaping the constructor signature.
type TypingIndicatorQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Add is a no-op in the MVP.
func (m *TypingIndicatorManager) Add(ctx context.Context, inst db.ChannelInstallation, sessionID pgtype.UUID, chatID, messageID string) {}

// Clear is a no-op in the MVP.
func (m *TypingIndicatorManager) Clear(ctx context.Context, sessionID pgtype.UUID) {}
