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

// threadParent is a top-level message that heads a thread (reply_count > 0).
func threadParent(user, text, ts string, replyCount int, latestReply string) slack.Message {
	m := msg(user, text, ts)
	m.ReplyCount = replyCount
	m.LatestReply = latestReply
	return m
}

func activeSlackInstall() db.ChannelInstallation {
	return db.ChannelInstallation{Status: "active", Config: slackInstallConfigJSON()}
}

// groupBinding builds a group session binding whose own thread root is threadRoot.
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

func findByTS(msgs []channel.HistoryMessage, ts string) *channel.HistoryMessage {
	for i := range msgs {
		if msgs[i].TS == ts {
			return &msgs[i]
		}
	}
	return nil
}

// TestChannelOverview verifies `chat history` reads conversations.history,
// normalizes oldest-first, and tags thread parents with their id + reply count
// without expanding thread contents.
func TestChannelOverview(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("100.000000"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{
		// Slack returns newest-first.
		historyMsgs: []slack.Message{
			threadParent("U1", "deploy discussion", "102.000000", 3, "105.000000"),
			msg("U2", "fyi unrelated", "101.000000"),
			msg("U3", "@bot take a look", "100.000000"),
		},
		users: []slack.User{{ID: "U1", RealName: "Alice"}},
	}
	h := newTestHistory(q, fc)

	page, err := h.ChannelOverview(context.Background(), uid(9), channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("ChannelOverview: %v", err)
	}
	if fc.historyCalls != 1 || fc.repliesCalls != 0 {
		t.Fatalf("expected conversations.history only, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if fc.lastHistory.ChannelID != "C1" {
		t.Errorf("channel id = %q, want C1", fc.lastHistory.ChannelID)
	}
	if page.ChannelType != "slack" || page.ThreadID != "" {
		t.Errorf("channel_type/thread_id = %q/%q, want slack/<empty>", page.ChannelType, page.ThreadID)
	}
	if len(page.Messages) != 3 || page.Messages[0].TS != "100.000000" || page.Messages[2].TS != "102.000000" {
		t.Fatalf("expected 3 msgs oldest-first, got %+v", page.Messages)
	}
	parent := findByTS(page.Messages, "102.000000")
	if parent == nil || parent.ThreadID != "102.000000" || parent.ReplyCount != 3 || parent.LatestReply != "105.000000" || parent.Author != "Alice" {
		t.Fatalf("thread parent metadata wrong: %+v", parent)
	}
	if plain := findByTS(page.Messages, "101.000000"); plain == nil || plain.ThreadID != "" || plain.ReplyCount != 0 {
		t.Fatalf("plain message should carry no thread metadata: %+v", plain)
	}
}

// TestThreadCurrent reads the session's own thread via conversations.replies.
func TestThreadCurrent(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{
		msg("U1", "second", "52.000000"),
		msg("U1", "root", "50.000000"),
	}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if fc.repliesCalls != 1 || fc.historyCalls != 0 {
		t.Fatalf("expected conversations.replies only, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if fc.lastReplies.Timestamp != "50.000000" || fc.lastReplies.ChannelID != "C1" {
		t.Errorf("replies anchored at %q/%q, want C1/50.000000", fc.lastReplies.ChannelID, fc.lastReplies.Timestamp)
	}
	if page.ThreadID != "50.000000" {
		t.Errorf("thread_id = %q, want 50.000000", page.ThreadID)
	}
	if len(page.Messages) != 2 || page.Messages[0].TS != "50.000000" {
		t.Fatalf("expected 2 msgs oldest-first, got %+v", page.Messages)
	}
}

// TestThreadByID reads a specific (non-current) thread within the same channel.
func TestThreadByID(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{msg("U1", "x", "77.000000")}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "70.000000", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if fc.lastReplies == nil || fc.lastReplies.Timestamp != "70.000000" {
		t.Fatalf("expected replies anchored at the passed id 70.000000, got %+v", fc.lastReplies)
	}
	if fc.lastReplies.ChannelID != "C1" {
		t.Errorf("channel must stay pinned to the session: got %q, want C1", fc.lastReplies.ChannelID)
	}
	if page.ThreadID != "70.000000" {
		t.Errorf("thread_id = %q, want 70.000000", page.ThreadID)
	}
}

// TestThreadDMUsesHistory: a DM has no threads, so a thread read falls back to
// the linear conversation (conversations.history).
func TestThreadDMUsesHistory(t *testing.T) {
	q := &fakeHistoryQueries{binding: dmBinding(), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{historyMsgs: []slack.Message{msg("U1", "hi", "100.000000")}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if fc.historyCalls != 1 || fc.repliesCalls != 0 {
		t.Fatalf("DM thread must use conversations.history, got history=%d replies=%d", fc.historyCalls, fc.repliesCalls)
	}
	if page.ThreadID != "" {
		t.Errorf("DM has no thread id, got %q", page.ThreadID)
	}
}

func TestChannelOverviewNoBinding(t *testing.T) {
	q := &fakeHistoryQueries{bindingErr: pgx.ErrNoRows}
	h := newTestHistory(q, &fakeHistoryClient{})
	if _, err := h.ChannelOverview(context.Background(), uid(9), channel.HistoryOptions{}); !errors.Is(err, ErrNoSlackSession) {
		t.Fatalf("err = %v, want ErrNoSlackSession", err)
	}
}

func TestThreadInactiveInstall(t *testing.T) {
	q := &fakeHistoryQueries{
		binding: groupBinding("50.0"),
		inst:    db.ChannelInstallation{Status: "revoked", Config: slackInstallConfigJSON()},
	}
	h := newTestHistory(q, &fakeHistoryClient{})
	if _, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{}); !errors.Is(err, ErrNoSlackSession) {
		t.Fatalf("err = %v, want ErrNoSlackSession", err)
	}
}

func TestChannelOverviewLimitClamp(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.0"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{}
	h := newTestHistory(q, fc)
	if _, err := h.ChannelOverview(context.Background(), uid(9), channel.HistoryOptions{Limit: 5000}); err != nil {
		t.Fatalf("ChannelOverview: %v", err)
	}
	if fc.lastHistory.Limit != maxHistoryLimit {
		t.Errorf("limit = %d, want clamp to %d", fc.lastHistory.Limit, maxHistoryLimit)
	}
}

// TestThreadRecoversBotAttachmentText covers alerting/webhook bots (Grafana
// cards, incoming webhooks): the body lives in attachments with an empty
// top-level Text. The root must be recovered, not dropped (MUL-3931 / #4803).
func TestThreadRecoversBotAttachmentText(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	root := slack.Message{Msg: slack.Msg{
		Username:  "Grafana",
		BotID:     "B1",
		Timestamp: "50.000000",
		Attachments: []slack.Attachment{{
			Fallback: "[FIRING:1] HighLatency prod",
			Title:    "HighLatency",
			Text:     "p99 over threshold",
		}},
	}}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{
		msg("U1", "@bot what's this alert?", "51.000000"),
		root,
	}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected root + reply, got %d: %+v", len(page.Messages), page.Messages)
	}
	got := findByTS(page.Messages, "50.000000")
	if got == nil {
		t.Fatalf("bot root message was dropped: %+v", page.Messages)
	}
	if got.Text != "[FIRING:1] HighLatency prod" {
		t.Errorf("root text = %q, want the attachment fallback", got.Text)
	}
	if got.Author != "Grafana" {
		t.Errorf("root author = %q, want Grafana", got.Author)
	}
}

// TestThreadRecoversBlocksText: a message with no Text and no attachment
// fallback is flattened from its Block Kit blocks.
func TestThreadRecoversBlocksText(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	root := slack.Message{Msg: slack.Msg{
		Username:  "Webhook",
		BotID:     "B2",
		Timestamp: "50.000000",
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Deploy failed", false, false)),
			slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "service *api* rolled back", false, false), nil, nil),
		}},
	}}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{root}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	got := findByTS(page.Messages, "50.000000")
	if got == nil {
		t.Fatalf("blocks-only message was dropped: %+v", page.Messages)
	}
	if got.Text != "Deploy failed\nservice *api* rolled back" {
		t.Errorf("root text = %q, want the flattened blocks", got.Text)
	}
}

// TestThreadRecoversRichTextBlock: a message whose only body is a Block Kit
// rich_text block (the shape Slack's rich text input produces) is flattened.
func TestThreadRecoversRichTextBlock(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	root := slack.Message{Msg: slack.Msg{
		Username:  "Webhook",
		BotID:     "B3",
		Timestamp: "50.000000",
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewRichTextBlock("blk",
				slack.NewRichTextSection(
					slack.NewRichTextSectionTextElement("incident opened: ", nil),
					slack.NewRichTextSectionLinkElement("https://ex.co/i/1", "INC-1", nil),
				),
			),
		}},
	}}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{root}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	got := findByTS(page.Messages, "50.000000")
	if got == nil {
		t.Fatalf("rich_text-only message was dropped: %+v", page.Messages)
	}
	if got.Text != "incident opened: INC-1" {
		t.Errorf("root text = %q, want the flattened rich_text", got.Text)
	}
}

// TestThreadDropsEmptySystemMarker: a message with no readable body anywhere
// (a join/leave/system marker) is still dropped — the fallback must not
// resurrect content-less markers.
func TestThreadDropsEmptySystemMarker(t *testing.T) {
	q := &fakeHistoryQueries{binding: groupBinding("50.000000"), inst: activeSlackInstall()}
	fc := &fakeHistoryClient{repliesMsgs: []slack.Message{
		{Msg: slack.Msg{SubType: "channel_join", User: "U1", Timestamp: "50.000000"}},
		msg("U2", "real reply", "51.000000"),
	}}
	h := newTestHistory(q, fc)

	page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].TS != "51.000000" {
		t.Fatalf("expected only the real reply, got %+v", page.Messages)
	}
}

// TestHistoryTargetDerivesRoot pins channel + thread-root recovery from a binding.
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
