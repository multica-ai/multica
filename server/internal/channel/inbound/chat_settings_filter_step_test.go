package inbound

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

type stubChatBindingLookup struct {
	ctx ChatBindingContext
	err error
}

func (s stubChatBindingLookup) LookupChatContext(context.Context, string, string) (ChatBindingContext, error) {
	return s.ctx, s.err
}

func TestChatSettingsFilterStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("direct_skips_filter", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{})
		evt := port.InboundEvent{
			Type:     port.EventTypeMessageReceived,
			ChatType: port.ChatTypeDirect,
			Text:     "hello",
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil {
			t.Fatal(err)
		}
		if d != DecisionContinue {
			t.Fatalf("decision = %v", d)
		}
	})

	t.Run("slash_prefix_continues", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{})
		evt := port.InboundEvent{
			Type:     port.EventTypeMessageReceived,
			ChatType: port.ChatTypeGroup,
			Text:     "  /help",
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil || d != DecisionContinue {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})

	t.Run("recall_bypasses", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{})
		evt := port.InboundEvent{
			Type:     port.EventTypeMessageRecalled,
			ChatType: port.ChatTypeGroup,
			Text:     "",
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil || d != DecisionContinue {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})

	t.Run("unbound_group_skips", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{err: pgx.ErrNoRows})
		evt := port.InboundEvent{
			Type:     port.EventTypeMessageReceived,
			ChatType: port.ChatTypeGroup,
			Text:     "noise",
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil || d != DecisionSkip {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})

	t.Run("mentions_without_bot_skips", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{
			ctx: ChatBindingContext{WorkspaceID: "ws-1", ListenMode: "mentions"},
		})
		evt := port.InboundEvent{
			Type:         port.EventTypeMessageReceived,
			ChatType:     port.ChatTypeGroup,
			Text:         "hello",
			BotMentioned: false,
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil || d != DecisionSkip {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})

	t.Run("mentions_with_bot_continues", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{
			ctx: ChatBindingContext{WorkspaceID: "ws-1", ListenMode: "mentions"},
		})
		evt := port.InboundEvent{
			Type:         port.EventTypeMessageReceived,
			ChatType:     port.ChatTypeGroup,
			Text:         "hello",
			BotMentioned: true,
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil || d != DecisionContinue {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})

	t.Run("all_mode_without_mention_continues", func(t *testing.T) {
		step := NewChatSettingsFilterStep(stubChatBindingLookup{
			ctx: ChatBindingContext{WorkspaceID: "ws-1", ListenMode: "all"},
		})
		evt := port.InboundEvent{
			Type:         port.EventTypeMessageReceived,
			ChatType:     port.ChatTypeGroup,
			Text:         "broadcast",
			BotMentioned: false,
		}
		_, d, err := step.Run(ctx, evt)
		if err != nil || d != DecisionContinue {
			t.Fatalf("d=%v err=%v", d, err)
		}
	})

	t.Run("lookup_error_returns_error", func(t *testing.T) {
		want := errors.New("db unavailable")
		step := NewChatSettingsFilterStep(stubChatBindingLookup{err: want})
		evt := port.InboundEvent{
			Type:         port.EventTypeMessageReceived,
			ChatType:     port.ChatTypeGroup,
			Text:         "hello",
			BotMentioned: false,
		}
		_, _, err := step.Run(ctx, evt)
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	})
}
