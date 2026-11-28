package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[0] = b
	u.Valid = true
	return u
}

type fakeOutboundQueries struct {
	binding             db.ChannelChatSessionBinding
	bindingErr          error
	inst                db.ChannelInstallation
	instErr             error
	task                db.AgentTaskQueue
	taskErr             error
	taskChannelIngested bool
	provenanceRows      int64
	provenanceErr       error
	gotProvenance       db.SetChatMessageChannelOutboundProvenanceByTaskParams
	recordedOutbound    []db.RecordChannelOutboundMessageParams
	recordOutboundErr   error
}

func (f *fakeOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return f.task, f.taskErr
}

func (f *fakeOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return f.taskChannelIngested, nil
}

func (f *fakeOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	if f.bindingErr != nil {
		return db.ChannelTaskDelivery{}, f.bindingErr
	}
	return db.ChannelTaskDelivery{
		BindingID: f.binding.ID, InstallationID: f.binding.InstallationID,
		ChannelType: string(TypeSlack), ChannelChatID: f.binding.ChannelChatID,
		ChannelMessageID: f.binding.LastMessageID, ChannelThreadID: f.binding.LastThreadID,
		RouteRevision: f.binding.RouteRevision, Config: f.binding.Config,
	}, nil
}

func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.inst, f.instErr
}
func (f *fakeOutboundQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{Slug: "acme"}, nil
}

func (f *fakeOutboundQueries) RecordChannelOutboundMessage(_ context.Context, arg db.RecordChannelOutboundMessageParams) error {
	f.recordedOutbound = append(f.recordedOutbound, arg)
	return f.recordOutboundErr
}

func (f *fakeOutboundQueries) SetChatMessageChannelOutboundProvenanceByTask(_ context.Context, arg db.SetChatMessageChannelOutboundProvenanceByTaskParams) (int64, error) {
	f.gotProvenance = arg
	if f.provenanceRows == 0 && f.provenanceErr == nil {
		return 1, nil
	}
	return f.provenanceRows, f.provenanceErr
}

type fakeSender struct {
	called      int
	got         channel.OutboundMessage
	gotBlocks   []slack.Block
	gotMetadata slack.SlackMetadata
	result      channel.SendResult
}

func (f *fakeSender) SendWithMetadata(_ context.Context, out channel.OutboundMessage, metadata slack.SlackMetadata) (channel.SendResult, error) {
	f.called++
	f.got = out
	f.gotMetadata = metadata
	if len(f.result.MessageIDs) > 0 || f.result.MessageID != "" {
		return f.result, nil
	}
	return channel.SendResult{MessageID: "1.1"}, nil
}

func (f *fakeSender) SendBlocks(ctx context.Context, out channel.OutboundMessage, blocks []slack.Block, metadata slack.SlackMetadata) (channel.SendResult, error) {
	f.gotBlocks = blocks
	return f.SendWithMetadata(ctx, out, metadata)
}

// slackInstallConfigJSON builds an installation config blob with base64 tokens
// (a nil Decrypter treats the decoded bytes as plaintext).
func slackInstallConfigJSON() []byte {
	b, _ := json.Marshal(map[string]string{
		"app_id":              "T1",
		"bot_user_id":         "UBOT",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte("xoxb-test")),
	})
	return b
}

func newTestOutbound(q outboundQueries, fs *fakeSender) *Outbound {
	o := NewOutbound(q, nil, "", nil)
	o.newSender = func(credentials) outboundSender { return fs }
	return o
}

func chatDoneEvent(sessionID string, content string) events.Event {
	return events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: sessionID,
		Payload: protocol.ChatDonePayload{
			TaskID:        "00000000-0000-0000-0000-000000000002",
			ChatSessionID: sessionID,
			Content:       content,
		},
	}
}

func TestOutbound_SkipsDirectChatTaskOnBoundSlackSession(t *testing.T) {
	q := &fakeOutboundQueries{
		task: db.AgentTaskQueue{ChatInputTaskID: uid(2)},
		binding: db.ChannelChatSessionBinding{
			InstallationID: uid(1),
			ChannelChatID:  "C123",
			Config:         []byte(`{"channel_id":"C123"}`),
		},
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fs := &fakeSender{}

	newTestOutbound(q, fs).handleEvent(chatDoneEvent("00000000-0000-0000-0000-000000000001", "private web reply"))

	if fs.called != 0 {
		t.Fatalf("sender called %d times, want 0 for a direct-chat task", fs.called)
	}
}

// A sealed channel task owns an input batch exactly like a direct task; the
// outbound gate must key on channel provenance, not owner presence, or every
// Slack reply is silently dropped.
func TestOutbound_PostsSealedChannelTaskReply(t *testing.T) {
	q := &fakeOutboundQueries{
		task:                db.AgentTaskQueue{ChatInputTaskID: uid(2)},
		taskChannelIngested: true,
		binding: db.ChannelChatSessionBinding{
			InstallationID: uid(1),
			ChannelChatID:  "C123",
			Config:         []byte(`{"channel_id":"C123"}`),
		},
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fs := &fakeSender{result: channel.SendResult{MessageID: "1.2", MessageIDs: []string{"1.1", "1.2"}}}

	newTestOutbound(q, fs).handleEvent(chatDoneEvent("00000000-0000-0000-0000-000000000001", "channel answer"))

	if fs.called != 1 {
		t.Fatalf("sender called %d times, want 1 for a sealed channel task", fs.called)
	}
	if len(q.gotProvenance.MessageIds) != 2 || q.gotProvenance.MessageIds[0] != "1.1" || q.gotProvenance.MessageIds[1] != "1.2" {
		t.Fatalf("recorded message ids = %v, want [1.1 1.2]", q.gotProvenance.MessageIds)
	}
	if !q.gotProvenance.ChannelType.Valid || q.gotProvenance.ChannelType.String != "slack" {
		t.Fatalf("recorded channel type = %+v, want slack", q.gotProvenance.ChannelType)
	}
	if q.gotProvenance.InstallationID != uid(1) || !q.gotProvenance.ChannelChatID.Valid || q.gotProvenance.ChannelChatID.String != "C123" {
		t.Fatalf("recorded target = installation:%+v chat:%+v", q.gotProvenance.InstallationID, q.gotProvenance.ChannelChatID)
	}
}

func TestOutbound_PostsReplyToBoundSlackChannel(t *testing.T) {
	q := &fakeOutboundQueries{
		// Composite isolation key; real channel + reply thread come from config /
		// last_thread_id.
		binding: db.ChannelChatSessionBinding{
			InstallationID: uid(1),
			ChannelChatID:  "C123:1111.0",
			Config:         []byte(`{"channel_id":"C123"}`),
			LastThreadID:   pgtype.Text{String: "1111.0", Valid: true},
		},
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fs := &fakeSender{}
	o := newTestOutbound(q, fs)

	o.handleEvent(chatDoneEvent("00000000-0000-0000-0000-000000000001", "**all done**"))

	if fs.called != 1 {
		t.Fatalf("sender called %d times, want 1", fs.called)
	}
	if fs.got.ChatID != "C123" {
		t.Errorf("ChatID = %q, want the real channel from config (not the composite key)", fs.got.ChatID)
	}
	if fs.got.ThreadID != "1111.0" {
		t.Errorf("ThreadID = %q, want the recorded reply thread", fs.got.ThreadID)
	}
	if fs.got.Text != "**all done**" {
		t.Errorf("Text = %q, want the raw content (Send applies mrkdwn)", fs.got.Text)
	}
}

func TestOutbound_IgnoresNonSlackAndEmptyAndRevoked(t *testing.T) {
	const sid = "00000000-0000-0000-0000-000000000001"
	activeInst := db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()}
	boundBinding := db.ChannelChatSessionBinding{InstallationID: uid(1), ChannelChatID: "C1", Config: []byte(`{"channel_id":"C1"}`)}

	cases := []struct {
		name string
		q    *fakeOutboundQueries
		evt  events.Event
	}{
		{
			name: "no slack binding (Feishu / web session)",
			q:    &fakeOutboundQueries{bindingErr: pgx.ErrNoRows},
			evt:  chatDoneEvent(sid, "hi"),
		},
		{
			name: "empty completion content",
			q:    &fakeOutboundQueries{binding: boundBinding, inst: activeInst},
			evt:  chatDoneEvent(sid, ""),
		},
		{
			name: "revoked installation",
			q:    &fakeOutboundQueries{binding: boundBinding, inst: db.ChannelInstallation{ID: uid(1), Status: "revoked", Config: slackInstallConfigJSON()}},
			evt:  chatDoneEvent(sid, "hi"),
		},
		{
			name: "non-chat event (no session id)",
			q:    &fakeOutboundQueries{},
			evt:  chatDoneEvent("", "hi"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeSender{}
			newTestOutbound(tc.q, fs).handleEvent(tc.evt)
			if fs.called != 0 {
				t.Errorf("%s: sender must not be called, got %d", tc.name, fs.called)
			}
		})
	}
}

func taskFailedEvent(payload map[string]any) events.Event {
	payload["task_id"] = "00000000-0000-0000-0000-000000000002"
	return events.Event{
		Type:          protocol.EventTaskFailed,
		TaskID:        "00000000-0000-0000-0000-000000000002",
		ChatSessionID: "00000000-0000-0000-0000-000000000001",
		Payload:       payload,
	}
}

func failedSlackQueries() *fakeOutboundQueries {
	return &fakeOutboundQueries{
		taskChannelIngested: true,
		binding: db.ChannelChatSessionBinding{
			ID: uid(7), InstallationID: uid(1), ChannelChatID: "C123:1111.0",
			Config:       []byte(`{"channel_id":"C123"}`),
			LastThreadID: pgtype.Text{String: "1111.0", Valid: true},
		},
		inst: db.ChannelInstallation{ID: uid(1), WorkspaceID: uid(3), Status: "active", Config: slackInstallConfigJSON()},
	}
}

func TestOutbound_TerminalFailurePostsRetryCard(t *testing.T) {
	q := failedSlackQueries()
	fs := &fakeSender{}
	o := NewOutbound(q, nil, "https://app.example/", nil)
	o.newSender = func(credentials) outboundSender { return fs }

	o.handleEvent(taskFailedEvent(map[string]any{"error": "runtime <@U9> offline", "retry_pending": false}))

	if fs.called != 1 || fs.got.ChatID != "C123" || fs.got.ThreadID != "1111.0" {
		t.Fatalf("card post = %d %+v, want one post into the bound thread", fs.called, fs.got)
	}
	if fs.gotMetadata.EventPayload["kind"] != "task_failure" {
		t.Fatalf("metadata kind = %v, want task_failure", fs.gotMetadata.EventPayload["kind"])
	}
	if len(fs.gotBlocks) != 2 {
		t.Fatalf("blocks = %d, want section + actions", len(fs.gotBlocks))
	}
	section := fs.gotBlocks[0].(*slack.SectionBlock)
	if !strings.Contains(section.Text.Text, "&lt;@U9&gt;") {
		t.Fatalf("section text %q must escape agent output", section.Text.Text)
	}
	actions := fs.gotBlocks[1].(*slack.ActionBlock).Elements.ElementSet
	retry := actions[0].(*slack.ButtonBlockElement)
	open := actions[1].(*slack.ButtonBlockElement)
	if retry.ActionID != retryTaskActionID || retry.Value != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("retry button = %+v", retry)
	}
	if open.URL != "https://app.example/acme/chat?session=00000000-0000-0000-0000-000000000001" {
		t.Fatalf("open URL = %q", open.URL)
	}
	if len(q.recordedOutbound) != 1 || q.recordedOutbound[0].OutboundKind != "task_failure" {
		t.Fatalf("recorded outbound = %+v, want one task_failure row", q.recordedOutbound)
	}
}

func TestOutbound_FailureWithPendingAutoRetryStaysSilent(t *testing.T) {
	q := failedSlackQueries()
	fs := &fakeSender{}

	newTestOutbound(q, fs).handleEvent(taskFailedEvent(map[string]any{"retry_pending": true, "failure_reason": "provider_network"}))

	if fs.called != 0 {
		t.Fatalf("sender called %d times, want 0 while an auto-retry is pending", fs.called)
	}
}
