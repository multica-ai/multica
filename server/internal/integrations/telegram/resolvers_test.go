package telegram

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type captureChatSession struct {
	appendIn engine.AppendInput
}

func (f *captureChatSession) EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error) {
	return pgtype.UUID{}, nil
}

func (f *captureChatSession) MarkPendingFresh(context.Context, pgtype.UUID, string) error {
	return nil
}

func (f *captureChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	f.appendIn = in
	return engine.AppendResult{}, nil
}

func (f *captureChatSession) BindMediaRefs(context.Context, engine.BindMediaInput) error {
	return nil
}

func TestTelegramSessionBinder_AppendPreservesFreshContextIntent(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}

	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		Message: channel.InboundMessage{
			MessageID:  "-100200:10",
			Text:       "summarize this",
			ForceFresh: true,
			Source: channel.Source{
				ChatID:   "-100200",
				ChatType: channel.ChatTypeGroup,
				ThreadID: "42",
			},
		},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if !session.appendIn.ForceFresh {
		t.Fatal("AppendUserMessage lost ForceFresh; /new <message> would remain in the previous context generation")
	}
}
