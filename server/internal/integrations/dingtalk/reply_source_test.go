package dingtalk

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestReplySourceRequiresLocalAcceptedInput(t *testing.T) {
	client := NewClient(nil, "")
	inst, inputID := sessionUUID(80), sessionUUID(81)
	msg := groupReactionMessage("source")
	msg.Text = "private question"
	msg.Raw = []byte(`{"session_webhook":"private bearer"}`)
	capture := &captureChatSession{appendErr: errors.New("append failed"), appendResult: engine.AppendResult{MessageID: inputID}}
	binder := &sessionBinder{session: capture, replies: client}
	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{InstallationID: inst, Message: msg}); err == nil {
		t.Fatal("expected append failure")
	}
	if _, ok := client.replySourceFor(inst, inputID); ok {
		t.Fatal("failed input retained an anchor")
	}
	capture.appendErr = nil
	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{InstallationID: inst, Message: msg}); err != nil {
		t.Fatal(err)
	}
	source, ok := client.replySourceFor(inst, inputID)
	if !ok || source.message.MessageID != "source" || source.message.Text != "" || len(source.message.Raw) != 0 {
		t.Fatal("source cache lost its anchor or retained callback content")
	}
	if _, ok := client.replySourceFor(sessionUUID(82), inputID); ok {
		t.Fatal("anchor crossed installation")
	}
	if _, ok := NewClient(nil, "").replySourceFor(inst, inputID); ok {
		t.Fatal("restart recovered memory attribution")
	}
	// /new with a first message enters through StartSession instead of AppendMessage.
	freshID := sessionUUID(83)
	capture.startResult = engine.StartSessionResult{Append: engine.AppendResult{MessageID: freshID}}
	if _, err := binder.StartSession(context.Background(), engine.StartSessionParams{Installation: engine.ResolvedInstallation{ID: inst}, Message: msg, PersistMessage: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.replySourceFor(inst, freshID); !ok {
		t.Fatal("new-session input lost its anchor")
	}
}

func TestReplySourceMemoryIsBounded(t *testing.T) {
	client := NewClient(nil, "")
	inst, first := sessionUUID(84), sessionUUID(85)
	client.rememberReplySource(inst, first, groupReactionMessage("first"))
	client.rememberReplySource(inst, first, groupReactionMessage("duplicate"))
	if source, _ := client.replySourceFor(inst, first); source.message.MessageID != "first" {
		t.Fatal("duplicate input replaced accepted source")
	}
	for i := 0; i < maxReplySources; i++ {
		client.rememberReplySource(inst, dbid.NewV7(), groupReactionMessage("other"))
	}
	if len(client.sources.entries) != maxReplySources || len(client.sources.order) != maxReplySources {
		t.Fatal("memory cache exceeded its bound")
	}
	if _, ok := client.replySourceFor(inst, first); ok {
		t.Fatal("oldest anchor was not evicted")
	}
}

func TestReplySourceConcurrentCaptureAndLookup(t *testing.T) {
	client := NewClient(nil, "")
	inst, input := sessionUUID(86), sessionUUID(87)
	msg := groupReactionMessage("source")
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.rememberReplySource(inst, input, msg)
			client.replySourceFor(inst, input)
		}()
	}
	wg.Wait()
	if len(client.sources.entries) != 1 || len(client.sources.order) != 1 {
		t.Fatal("concurrent duplicate created multiple entries")
	}
}

func TestReplySourceRequiresCompleteCoordinates(t *testing.T) {
	inst, input := sessionUUID(88), sessionUUID(89)
	for _, missing := range []string{"client", "installation", "input", "message", "conversation"} {
		t.Run(missing, func(t *testing.T) {
			client := NewClient(nil, "")
			installationID, inputID, msg := inst, input, groupReactionMessage("source")
			switch missing {
			case "client":
				client = nil
			case "installation":
				installationID.Valid = false
			case "input":
				inputID.Valid = false
			case "message":
				msg.MessageID = ""
			case "conversation":
				msg.Source.ChatID = ""
			}
			client.rememberReplySource(installationID, inputID, msg)
			if _, ok := client.replySourceFor(inst, input); ok {
				t.Fatal("incomplete coordinates created a usable reply anchor")
			}
			if client != nil && len(client.sources.entries) != 0 {
				t.Fatal("incomplete coordinates consumed cache capacity")
			}
		})
	}
}
