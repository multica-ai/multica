package lark

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeTypingAPIClient records reaction calls and can be programmed to fail.
type fakeTypingAPIClient struct {
	addCalled    []addReactionCall
	deleteCalled []deleteReactionCall
	addErr       error
	deleteErr    error
	addReturn    string
}

type addReactionCall struct {
	creds     InstallationCredentials
	messageID string
	emojiType string
}

type deleteReactionCall struct {
	creds      InstallationCredentials
	messageID  string
	reactionID string
}

func (f *fakeTypingAPIClient) IsConfigured() bool { return true }
func (f *fakeTypingAPIClient) SendInteractiveCard(context.Context, SendCardParams) (string, error) {
	return "", nil
}
func (f *fakeTypingAPIClient) PatchInteractiveCard(context.Context, PatchCardParams) error {
	return nil
}
func (f *fakeTypingAPIClient) SendTextMessage(context.Context, SendTextParams) (string, error) {
	return "", nil
}
func (f *fakeTypingAPIClient) SendDirectTextMessage(context.Context, SendDirectTextParams) (string, error) {
	return "", nil
}
func (f *fakeTypingAPIClient) SendMarkdownCard(context.Context, SendMarkdownCardParams) (string, error) {
	return "", nil
}
func (f *fakeTypingAPIClient) SendBindingPromptCard(context.Context, BindingPromptParams) error {
	return nil
}
func (f *fakeTypingAPIClient) GetBotInfo(context.Context, InstallationCredentials) (BotInfo, error) {
	return BotInfo{}, nil
}
func (f *fakeTypingAPIClient) GetMessage(context.Context, InstallationCredentials, string) ([]LarkMessage, error) {
	return nil, nil
}
func (f *fakeTypingAPIClient) ListChatMessages(context.Context, InstallationCredentials, ListMessagesParams) ([]LarkMessage, error) {
	return nil, nil
}
func (f *fakeTypingAPIClient) BatchGetUsers(context.Context, InstallationCredentials, []string) (map[string]string, error) {
	return nil, nil
}
func (f *fakeTypingAPIClient) AddMessageReaction(_ context.Context, p AddReactionParams) (string, error) {
	f.addCalled = append(f.addCalled, addReactionCall{p.InstallationID, p.MessageID, p.EmojiType})
	return f.addReturn, f.addErr
}
func (f *fakeTypingAPIClient) DeleteMessageReaction(_ context.Context, p DeleteReactionParams) error {
	f.deleteCalled = append(f.deleteCalled, deleteReactionCall{p.InstallationID, p.MessageID, p.ReactionID})
	return f.deleteErr
}

type fakeTypingQueries struct {
	binding      db.LarkChatSessionBinding
	installation db.LarkInstallation
	bindingErr   error
	installErr   error
}

func (f *fakeTypingQueries) GetLarkChatSessionBindingBySession(context.Context, pgtype.UUID) (db.LarkChatSessionBinding, error) {
	return f.binding, f.bindingErr
}
func (f *fakeTypingQueries) GetLarkInstallation(context.Context, pgtype.UUID) (db.LarkInstallation, error) {
	return f.installation, f.installErr
}

type fakeTypingCreds struct{ secret string }

func (f fakeTypingCreds) DecryptAppSecret(inst db.LarkInstallation) (string, error) {
	return f.secret, nil
}

func TestTypingIndicatorAddRecordsState(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-123"}
	queries := &fakeTypingQueries{}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, queries, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}

	mgr.Add(context.Background(), inst, session, "msg-1", "")

	if len(api.addCalled) != 1 {
		t.Fatalf("expected 1 add call, got %d", len(api.addCalled))
	}
	if api.addCalled[0].messageID != "msg-1" || api.addCalled[0].emojiType != typingEmoji {
		t.Fatalf("unexpected add call params: %+v", api.addCalled[0])
	}

	key := uuidString(session)
	mgr.mu.RLock()
	states := mgr.states[key]
	mgr.mu.RUnlock()
	if len(states) != 1 || states[0].MessageID != "msg-1" || states[0].ReactionID != "reaction-123" {
		t.Fatalf("unexpected state: %+v", states)
	}
}

func TestTypingIndicatorAddSkipsOnEmptyMessageID(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-123"}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{}, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	mgr.Add(context.Background(), inst, session, "", "")

	if len(api.addCalled) != 0 {
		t.Fatalf("expected 0 add calls, got %d", len(api.addCalled))
	}
}

func TestTypingIndicatorAddSkipsOldMessages(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-123"}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{}, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	oldTime := time.Now().Add(-3 * time.Minute).UnixMilli()
	mgr.Add(context.Background(), inst, session, "msg-old", strconv.FormatInt(oldTime, 10))

	if len(api.addCalled) != 0 {
		t.Fatalf("expected 0 add calls for old message, got %d", len(api.addCalled))
	}
}

func TestTypingIndicatorAddLogsOnAPIError(t *testing.T) {
	api := &fakeTypingAPIClient{addErr: errors.New("lark down")}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{}, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	mgr.Add(context.Background(), inst, session, "msg-1", "")

	if len(api.addCalled) != 1 {
		t.Fatalf("expected 1 add call, got %d", len(api.addCalled))
	}

	key := uuidString(session)
	mgr.mu.RLock()
	states := mgr.states[key]
	mgr.mu.RUnlock()
	if len(states) != 0 {
		t.Fatalf("expected 0 states after error, got %d", len(states))
	}
}

func TestTypingIndicatorClearDeletesReactions(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-abc"}
	queries := &fakeTypingQueries{
		binding: db.LarkChatSessionBinding{
			InstallationID: pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9}, Valid: true},
		},
		installation: db.LarkInstallation{
			ID:     pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9}, Valid: true},
			AppID:  "cli_test",
			Region: "feishu",
		},
	}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, queries, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}

	mgr.Add(context.Background(), inst, session, "msg-1", "")
	if len(api.addCalled) != 1 {
		t.Fatal("add should have been called")
	}

	mgr.Clear(context.Background(), session)

	if len(api.deleteCalled) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(api.deleteCalled))
	}
	if api.deleteCalled[0].messageID != "msg-1" || api.deleteCalled[0].reactionID != "reaction-abc" {
		t.Fatalf("unexpected delete params: %+v", api.deleteCalled[0])
	}

	key := uuidString(session)
	mgr.mu.RLock()
	states := mgr.states[key]
	mgr.mu.RUnlock()
	if len(states) != 0 {
		t.Fatalf("expected 0 states after clear, got %d", len(states))
	}
}

func TestTypingIndicatorClearNoOpWhenEmpty(t *testing.T) {
	api := &fakeTypingAPIClient{}
	queries := &fakeTypingQueries{
		binding: db.LarkChatSessionBinding{
			InstallationID: pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		},
		installation: db.LarkInstallation{
			ID:     pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
			AppID:  "cli_test",
			Region: "feishu",
		},
	}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, queries, newDiscardLogger())

	session := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}
	mgr.Clear(context.Background(), session)

	if len(api.deleteCalled) != 0 {
		t.Fatalf("expected 0 delete calls when empty, got %d", len(api.deleteCalled))
	}
}

func TestTypingIndicatorClearLogsOnDeleteError(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-xyz", deleteErr: errors.New("delete failed")}
	queries := &fakeTypingQueries{
		binding: db.LarkChatSessionBinding{
			InstallationID: pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		},
		installation: db.LarkInstallation{
			ID:     pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
			AppID:  "cli_test",
			Region: "feishu",
		},
	}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, queries, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}

	mgr.Add(context.Background(), inst, session, "msg-1", "")
	mgr.Clear(context.Background(), session)

	if len(api.deleteCalled) != 1 {
		t.Fatalf("expected 1 delete call attempt, got %d", len(api.deleteCalled))
	}
}

func TestTypingIndicatorMultipleMessagesPerSession(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-n"}
	queries := &fakeTypingQueries{
		binding: db.LarkChatSessionBinding{
			InstallationID: pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		},
		installation: db.LarkInstallation{
			ID:     pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
			AppID:  "cli_test",
			Region: "feishu",
		},
	}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, queries, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}

	mgr.Add(context.Background(), inst, session, "msg-a", "")
	mgr.Add(context.Background(), inst, session, "msg-b", "")

	if len(api.addCalled) != 2 {
		t.Fatalf("expected 2 add calls, got %d", len(api.addCalled))
	}

	mgr.Clear(context.Background(), session)

	if len(api.deleteCalled) != 2 {
		t.Fatalf("expected 2 delete calls, got %d", len(api.deleteCalled))
	}
}

func TestTypingIndicatorConcurrentAddAndClear(t *testing.T) {
	api := &fakeTypingAPIClient{addReturn: "reaction-concurrent"}
	queries := &fakeTypingQueries{
		binding: db.LarkChatSessionBinding{
			InstallationID: pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		},
		installation: db.LarkInstallation{
			ID:     pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
			AppID:  "cli_test",
			Region: "feishu",
		},
	}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, queries, newDiscardLogger())

	inst := db.LarkInstallation{AppID: "cli_test", Region: "feishu"}
	session := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			mgr.Add(context.Background(), inst, session, "msg", "")
		}
		close(done)
	}()
	go func() {
		for i := 0; i < 50; i++ {
			mgr.Clear(context.Background(), session)
		}
	}()
	<-done
	time.Sleep(10 * time.Millisecond)
}
