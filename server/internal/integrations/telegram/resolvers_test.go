package telegram

import (
	"context"
	"strings"
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
			MessageID:   "-100200:10",
			Text:        "summarize this",
			CommandText: "current instruction",
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
	if session.startIn.CommandText != "current instruction" {
		t.Fatalf("CommandText = %q", session.startIn.CommandText)
	}
	if !session.startIn.PersistMessage || !session.startIn.HistoryBoundaryPending {
		t.Fatalf("start flags = persist:%t history:%t, want both true", session.startIn.PersistMessage, session.startIn.HistoryBoundaryPending)
	}
	if session.startIn.Sender != telegramTestUUID(6) || session.startIn.Initiator != telegramTestUUID(7) {
		t.Fatalf("creator/initiator mapping wrong: %+v", session.startIn)
	}
}

func TestTelegramQuotedTitleSourceReachesSession(t *testing.T) {
	for _, start := range []bool{false, true} {
		t.Run(map[bool]string{false: "append", true: "new"}[start], func(t *testing.T) {
			command := "Compare alternatives"
			if start {
				command = "/new " + command
			}
			msg, ok := inboundFromUpdate(Update{UpdateID: 1, Message: &Message{MessageID: 10, From: &User{ID: 111, FirstName: "Grace"}, Chat: Chat{ID: -100200, Type: "supergroup"}, Text: "@my_bot " + command, ReplyToMessage: &Message{MessageID: 9, From: &User{ID: 222, FirstName: "Ada"}, Text: "Old unrelated discussion"}}}, 999, "my_bot")
			if !ok || !msg.AddressedToBot {
				t.Fatal("quoted request was not accepted")
			}
			if !strings.Contains(msg.Text, "Old unrelated discussion") || !strings.Contains(msg.Text, "Compare alternatives") {
				t.Fatalf("producer body=%q", msg.Text)
			}
			session := &captureChatSession{}
			binder := &sessionBinder{session: session}
			if start {
				msg.CommandText, _ = engine.ParseNewChatCommand(msg.CommandText)
				if _, err := binder.StartSession(context.Background(), engine.StartSessionParams{Message: msg, PersistMessage: true}); err != nil {
					t.Fatal(err)
				}
				if session.startIn.CommandText != "Compare alternatives" || session.startIn.Body != msg.Text {
					t.Fatalf("start instruction/context=%q/%q", session.startIn.CommandText, session.startIn.Body)
				}
			} else {
				if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{Message: msg}); err != nil {
					t.Fatal(err)
				}
				if session.appendIn.CommandText != "Compare alternatives" || session.appendIn.Body != msg.Text {
					t.Fatalf("append instruction/context=%q/%q", session.appendIn.CommandText, session.appendIn.Body)
				}
			}
		})
	}
}
