package sharecrm

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type captureChatSession struct {
	appendIn     engine.AppendInput
	startIn      engine.StartSessionInput
	freshSession pgtype.UUID
	freshMessage string
}

func (f *captureChatSession) EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error) {
	return pgtype.UUID{}, nil
}

func (f *captureChatSession) StartSession(_ context.Context, in engine.StartSessionInput) (engine.StartSessionResult, error) {
	f.startIn = in
	return engine.StartSessionResult{}, nil
}

func (f *captureChatSession) MarkPendingFresh(_ context.Context, sessionID pgtype.UUID, messageID string) error {
	f.freshSession = sessionID
	f.freshMessage = messageID
	return nil
}

func (f *captureChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	f.appendIn = in
	return engine.AppendResult{}, nil
}

func (f *captureChatSession) BindMediaRefs(context.Context, engine.BindMediaInput) error {
	return nil
}

func TestShareCRMSessionBinder_AppendPreservesFreshContextIntent(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}

	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		Message: channel.InboundMessage{
			MessageID:  "m-fresh",
			Text:       "summarize this",
			ForceFresh: true,
			Source: channel.Source{
				ChatID:   "0:fs:session123:",
				ChatType: channel.ChatTypeP2P,
			},
		},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if !session.appendIn.ForceFresh {
		t.Fatal("AppendUserMessage lost ForceFresh; /clear <message> would remain in the previous context generation")
	}
	if session.appendIn.MessageID != "m-fresh" {
		t.Fatalf("MessageID = %q, want m-fresh", session.appendIn.MessageID)
	}
}

func TestShareCRMSessionBinder_MarkPendingFreshForwardsMessageID(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}
	sessionID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	if err := binder.MarkPendingFresh(context.Background(), sessionID, "bare-new"); err != nil {
		t.Fatalf("MarkPendingFresh: %v", err)
	}
	if session.freshMessage != "bare-new" {
		t.Fatalf("messageID = %q, want bare-new", session.freshMessage)
	}
	if session.freshSession != sessionID {
		t.Fatalf("sessionID = %v, want %v", session.freshSession, sessionID)
	}
}

func TestShareCRMSessionBinder_StartSessionForwardsRouting(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}
	creator := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	initiator := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}

	if _, err := binder.StartSession(context.Background(), engine.StartSessionParams{
		Installation: engine.ResolvedInstallation{ID: pgtype.UUID{Bytes: [16]byte{4}, Valid: true}},
		Creator:      creator,
		Sender:       initiator,
		Message: channel.InboundMessage{
			MessageID: "m-new",
			Text:      "hello after /new",
			Source: channel.Source{
				ChatID:   "0:fs:session-new:",
				ChatType: channel.ChatTypeP2P,
			},
		},
		PersistMessage: true,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if session.startIn.BindingKey != "0:fs:session-new:" {
		t.Fatalf("BindingKey = %q, want chat id", session.startIn.BindingKey)
	}
	if session.startIn.Sender != creator {
		t.Fatalf("embedded Sender = %v, want creator", session.startIn.Sender)
	}
	if session.startIn.Initiator != initiator {
		t.Fatalf("Initiator = %v, want authenticated sender", session.startIn.Initiator)
	}
	if session.startIn.Body != "hello after /new" || session.startIn.MessageID != "m-new" {
		t.Fatalf("first-turn body/id = %q/%q", session.startIn.Body, session.startIn.MessageID)
	}
	if !session.startIn.PersistMessage {
		t.Fatal("PersistMessage dropped; /new <message> would create an empty chat")
	}
}
