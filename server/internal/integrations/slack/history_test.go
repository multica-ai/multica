package slack

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeHistoryQueries struct {
	binding    db.ChannelChatSessionBinding
	bindingErr error
	inst       db.ChannelInstallation
	instErr    error
}

func (f *fakeHistoryQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.binding, f.bindingErr
}

func (f *fakeHistoryQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.inst, f.instErr
}

type fakeHistoryClient struct {
	historyMsgs  []slack.Message
	repliesMsgs  []slack.Message
	users        []slack.User
	historyCalls int
	repliesCalls int
	lastHistory  *slack.GetConversationHistoryParameters
	lastReplies  *slack.GetConversationRepliesParameters
}

func (f *fakeHistoryClient) GetConversationHistoryContext(_ context.Context, p *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	f.historyCalls++
	f.lastHistory = p
	return &slack.GetConversationHistoryResponse{Messages: f.historyMsgs}, nil
}

func (f *fakeHistoryClient) GetConversationRepliesContext(_ context.Context, p *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	f.repliesCalls++
	f.lastReplies = p
	return f.repliesMsgs, false, "", nil
}

func (f *fakeHistoryClient) GetUsersInfoContext(_ context.Context, _ ...string) (*[]slack.User, error) {
	return &f.users, nil
}

func msg(user, text, ts string) slack.Message {
	return slack.Message{Msg: slack.Msg{User: user, Text: text, Timestamp: ts}}
}

func activeSlackInstall() db.ChannelInstallation {
	return db.ChannelInstallation{Status: "active", Config: slackInstallConfigJSON()}
}

// groupBinding builds a group session binding rooted at threadRoot (the thread
// the bot's reply opened on the @mention).
func groupBinding(threadRoot string) db.ChannelChatSessionBinding {
	b := db.ChannelChatSessionBinding{
		InstallationID: uid(2),
		ChannelChatID:  "C1:" + threadRoot,
		ChatType:       string(channel.ChatTypeGroup),
		Config:         []byte(`{"channel_id":"C1"}`),
	}
	if threadRoot != "" {
		b.LastThreadID = pgtype.Text{String: threadRoot, Valid: true}
	}
	return b
}

func dmBinding() db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		InstallationID: uid(2),
		ChannelChatID:  "D1",
		ChatType:       string(channel.ChatTypeP2P),
		Config:         []byte(`{"channel_id":"D1"}`),
	}
}

func newTestHistory(q historyQueries, fc historyClient) *History {
	h := NewHistory(q, nil, nil) // nil decrypter => stored bytes treated as plaintext
	h.newClient = func(string) historyClient { return fc }
	return h
}

// TestHistoryFetchChannelScope verifies a channel-scope read uses
// conversations.history and normalizes oldest-first with roles + labels.
func TestHistoryFetchChannelScope(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{
		// Slack returns newest-first; the bot (UBOT) replied last.
		historyMsgs: []slack.Message{
			msg("UBOT", "on it", "102.000000"),
			msg("U1", "@bot look into this", "101.000000"),
			msg("U2", "alert: 5xx spiking", "100.000000"),
		},
		users: []slack.User{{ID: "U1", RealName: "Alice"}}, // U2 unresolved -> positional
	}
	h := newTestHistory(q, fc)

	page, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{Scope: channel.HistoryScopeChannel})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fc.historyCalls != 1 || fc.repliesCalls != 0 {
		t.Fatalf("expected conversations.history, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if fc.lastHistory.ChannelID != "C1" {
		t.Errorf("channel id = %q, want C1", fc.lastHistory.ChannelID)
	}
	if page.ChannelType != "slack" || page.Scope != channel.HistoryScopeChannel {
		t.Errorf("channel_type/scope = %q/%q, want slack/channel", page.ChannelType, page.Scope)
	}
	if len(page.Messages) != 3 || page.Messages[0].TS != "100.000000" || page.Messages[2].TS != "102.000000" {
		t.Fatalf("expected 3 msgs oldest-first, got %+v", page.Messages)
	}
	if got := page.Messages[0]; got.Author != "User 1" || got.Role != channel.HistoryRoleUser {
		t.Errorf("msg0 author/role = %q/%q, want User 1/user", got.Author, got.Role)
	}
	if got := page.Messages[1]; got.Author != "Alice" {
		t.Errorf("msg1 author = %q, want Alice", got.Author)
	}
	if got := page.Messages[2]; got.Author != "Bot" || got.Role != channel.HistoryRoleAssistant {
		t.Errorf("msg2 author/role = %q/%q, want Bot/assistant", got.Author, got.Role)
	}
}

// TestHistoryFetchThreadScope verifies a thread-scope read uses
// conversations.replies anchored on the session's thread root (from the binding).
func TestHistoryFetchThreadScope(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{
		msg("U1", "second", "52.000000"),
		msg("U1", "root", "50.000000"),
	}}
	h := newTestHistory(q, fc)

	page, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{Scope: channel.HistoryScopeThread, Limit: 10})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fc.repliesCalls != 1 || fc.historyCalls != 0 {
		t.Fatalf("expected conversations.replies, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if fc.lastReplies.Timestamp != "50.000000" || fc.lastReplies.ChannelID != "C1" {
		t.Errorf("replies anchored at %q/%q, want C1/50.000000", fc.lastReplies.ChannelID, fc.lastReplies.Timestamp)
	}
	if page.Scope != channel.HistoryScopeThread {
		t.Errorf("scope = %q, want thread", page.Scope)
	}
	if len(page.Messages) != 2 || page.Messages[0].TS != "50.000000" {
		t.Fatalf("expected 2 msgs oldest-first, got %+v", page.Messages)
	}
}

// TestHistoryFetchDMIgnoresThreadScope confirms a DM (no threads) degrades a
// thread request to channel history.
func TestHistoryFetchDMIgnoresThreadScope(t *testing.T) {
	q := &fakeHistoryQueries{binding: dmBinding(), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{historyMsgs: []slack.Message{msg("U1", "hi", "100.000000")}}
	h := newTestHistory(q, fc)

	page, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{Scope: channel.HistoryScopeThread})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fc.historyCalls != 1 || fc.repliesCalls != 0 {
		t.Fatalf("DM must use conversations.history, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if page.Scope != channel.HistoryScopeChannel {
		t.Errorf("scope = %q, want channel (DM has no thread)", page.Scope)
	}
}

// TestHistoryFetchThreadFallsBackWithoutRoot: a group binding with no recoverable
// thread root degrades a thread request to channel history.
func TestHistoryFetchThreadFallsBackWithoutRoot(t *testing.T) {
	q := &fakeHistoryQueries{
		binding: db.ChannelChatSessionBinding{InstallationID: uid(2), ChannelChatID: "C1", ChatType: string(channel.ChatTypeGroup), Config: []byte(`{"channel_id":"C1"}`)},
		inst:    activeSlackInstall(),
	}
	fc := &fakeHistoryClient{historyMsgs: []slack.Message{msg("U1", "x", "100.000000")}}
	h := newTestHistory(q, fc)

	page, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{Scope: channel.HistoryScopeThread})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fc.historyCalls != 1 || fc.repliesCalls != 0 {
		t.Fatalf("expected fallback to history, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if page.Scope != channel.HistoryScopeChannel {
		t.Errorf("scope = %q, want channel", page.Scope)
	}
}

// TestHistoryTargetDerivesRoot pins the channel + thread-root recovery from a
// binding: last_thread_id first, then the composite-key suffix, empty for a DM.
func TestHistoryTargetDerivesRoot(t *testing.T) {
	if ch, root := historyTarget(groupBinding("50.0")); ch != "C1" || root != "50.0" {
		t.Errorf("from last_thread_id: got %q/%q, want C1/50.0", ch, root)
	}
	keyOnly := db.ChannelChatSessionBinding{ChannelChatID: "C9:77.7", Config: []byte(`{"channel_id":"C9"}`)}
	if ch, root := historyTarget(keyOnly); ch != "C9" || root != "77.7" {
		t.Errorf("from key suffix: got %q/%q, want C9/77.7", ch, root)
	}
	if ch, root := historyTarget(dmBinding()); ch != "D1" || root != "" {
		t.Errorf("dm: got %q/%q, want D1/<empty>", ch, root)
	}
}

// TestHistoryFetchNoBinding maps a missing Slack binding to ErrNoSlackSession.
func TestHistoryFetchNoBinding(t *testing.T) {
	q := &fakeHistoryQueries{bindingErr: pgx.ErrNoRows}
	h := newTestHistory(q, &fakeHistoryClient{})
	if _, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{}); !errors.Is(err, ErrNoSlackSession) {
		t.Fatalf("err = %v, want ErrNoSlackSession", err)
	}
}

// TestHistoryFetchInactiveInstall treats a revoked installation as empty.
func TestHistoryFetchInactiveInstall(t *testing.T) {
	q := &fakeHistoryQueries{
		binding: groupBinding("50.0"),
		inst:    db.ChannelInstallation{Status: "revoked", Config: slackInstallConfigJSON()},
	}
	h := newTestHistory(q, &fakeHistoryClient{})
	if _, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{}); !errors.Is(err, ErrNoSlackSession) {
		t.Fatalf("err = %v, want ErrNoSlackSession", err)
	}
}

// TestHistoryLimitClamp confirms an over-large limit is clamped before the call.
func TestHistoryLimitClamp(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.0"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{}
	h := newTestHistory(q, fc)
	if _, err := h.Fetch(context.Background(), uid(9), channel.HistoryOptions{Scope: channel.HistoryScopeChannel, Limit: 5000}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fc.lastHistory.Limit != maxHistoryLimit {
		t.Errorf("limit = %d, want clamp to %d", fc.lastHistory.Limit, maxHistoryLimit)
	}
}
