package telegram

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type captureChatSession struct {
	startIn  engine.StartSessionInput
	appendIn engine.AppendInput
}

func (f *captureChatSession) EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error) {
	return pgtype.UUID{}, nil
}

func (f *captureChatSession) StartSession(_ context.Context, in engine.StartSessionInput) (engine.StartSessionResult, error) {
	f.startIn = in
	return engine.StartSessionResult{SessionID: pgtype.UUID{Valid: true}}, nil
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
		t.Fatal("AppendUserMessage lost ForceFresh; /clear <message> would remain in the previous context generation")
	}
}

func TestTelegramSessionBinder_StartSessionPreservesRouteAndFirstTurn(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}

	result, err := binder.StartSession(context.Background(), engine.StartSessionParams{
		Creator: telegramTestUUID(6),
		Sender:  telegramTestUUID(7),
		Message: channel.InboundMessage{
			MessageID: "-100200:10",
			Text:      "summarize this",
			Source: channel.Source{
				ChatID:   "-100200",
				ChatType: channel.ChatTypeGroup,
				ThreadID: "42",
			},
		},
		PersistMessage:         true,
		HistoryBoundaryPending: true,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if !result.SessionID.Valid {
		t.Fatal("StartSession result lost session id")
	}
	if got := session.startIn.BindingKey; got != "-100200:42" {
		t.Fatalf("BindingKey = %q, want forum-topic route", got)
	}
	if got := session.startIn.ThreadID; got != "42" {
		t.Fatalf("ThreadID = %q, want reply topic", got)
	}
	if got := session.startIn.Body; got != "summarize this" {
		t.Fatalf("Body = %q, want first /new turn", got)
	}
	if !session.startIn.PersistMessage || !session.startIn.HistoryBoundaryPending {
		t.Fatalf("start flags = persist:%t history:%t, want both true", session.startIn.PersistMessage, session.startIn.HistoryBoundaryPending)
	}
	if session.startIn.Sender != telegramTestUUID(6) || session.startIn.Initiator != telegramTestUUID(7) {
		t.Fatalf("creator/initiator mapping wrong: %+v", session.startIn)
	}
}
