package inbound

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

const (
	chatSettingsFilterStepName = "chat-settings-filter"
	chatListenModeMentions     = "mentions"
	chatListenModeAll          = "all"
)

// chatBindingContextSource loads workspace binding settings for a chat.
type chatBindingContextSource interface {
	LookupChatContext(ctx context.Context, connectionID, chatID string) (ChatBindingContext, error)
}

type chatSettingsFilterStep struct {
	src chatBindingContextSource
}

// NewChatSettingsFilterStep gates group messages using the workspace binding's
// listen_mode and the adapter-provided BotMentioned flag. It runs after
// identity and /bind handling so control commands still reach slash expansion.
func NewChatSettingsFilterStep(src chatBindingContextSource) Step {
	if src == nil {
		return noopChatSettingsFilterStep{}
	}
	return &chatSettingsFilterStep{src: src}
}

type noopChatSettingsFilterStep struct{}

func (noopChatSettingsFilterStep) Name() string { return chatSettingsFilterStepName }

func (noopChatSettingsFilterStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	return evt, DecisionContinue, nil
}

func (*chatSettingsFilterStep) Name() string { return chatSettingsFilterStepName }

func (s *chatSettingsFilterStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if evt.Type != port.EventTypeMessageReceived {
		return evt, DecisionContinue, nil
	}
	if evt.ChatType == port.ChatTypeDirect {
		return evt, DecisionContinue, nil
	}
	if strings.HasPrefix(strings.TrimSpace(evt.Text), "/") {
		return evt, DecisionContinue, nil
	}

	row, err := s.src.LookupChatContext(ctx, evt.ConnectionID(), evt.ChatID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("chat-settings-filter: binding lookup failed", "error", err)
		return evt, DecisionContinue, fmt.Errorf("chat-settings-filter: %w", err)
	}
	if row.WorkspaceID == "" {
		// Unbound group: drop normal chatter; /bind was handled earlier.
		return evt, DecisionSkip, nil
	}

	listen := row.ListenMode
	if listen == "" {
		listen = chatListenModeMentions
	}
	if listen == chatListenModeAll {
		return evt, DecisionContinue, nil
	}
	if evt.BotMentioned {
		return evt, DecisionContinue, nil
	}
	return evt, DecisionSkip, nil
}
