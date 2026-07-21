package slack

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeReplySender struct {
	sent  *channel.OutboundMessage
	calls int
}

func (f *fakeReplySender) Send(_ context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	f.calls++
	cp := out
	f.sent = &cp
	return channel.SendResult{MessageID: "1.1"}, nil
}

type fakeBindingMinter struct {
	raw     string
	gotWS   pgtype.UUID
	gotInst pgtype.UUID
	gotUser string
	calls   int
}

func (f *fakeBindingMinter) Mint(_ context.Context, ws, inst pgtype.UUID, user string) (BindingToken, error) {
	f.calls++
	f.gotWS, f.gotInst, f.gotUser = ws, inst, user
	return BindingToken{Raw: f.raw, ExpiresAt: time.Unix(0, 0)}, nil
}

func newTestReplier(binding bindingMinter, sender replySender) *OutboundReplier {
	r := NewOutboundReplier(OutboundReplierConfig{
		Binding: binding,
		Decrypt: nil, // identity: stored bot token is base64 plaintext
		AppURL:  "https://multica.example",
	})
	r.newSender = func(credentials) replySender { return sender }
	return r
}

// installConfigJSON with a base64 (identity-decryptable) bot token so
// decodeCredentials succeeds inside post().
const replierConfigJSON = `{"app_id":"T1","bot_user_id":"UBOT","bot_token_encrypted":"eG94Yi10ZXN0"}`

func testResolvedInstallation(t *testing.T) engine.ResolvedInstallation {
	return engine.ResolvedInstallation{
		ID:          mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		AgentID:     mustUUID(t, "22222222-2222-2222-2222-222222222222"),
		Active:      true,
		Platform:    db.ChannelInstallation{Config: []byte(replierConfigJSON)},
	}
}

func testInboundForReply() channel.InboundMessage {
	return channel.InboundMessage{
		MessageID: "1700000000.000300",
		Source: channel.Source{
			ChannelType: TypeSlack,
			ChatID:      "C1",
			ChatType:    channel.ChatTypeGroup,
			SenderID:    "UALICE",
			ThreadID:    "1700000000.000200",
		},
	}
}

func TestReply_NeedsBinding_MintsAndPostsPrompt(t *testing.T) {
	sender := &fakeReplySender{}
	minter := &fakeBindingMinter{raw: "tok_RAW-123"}
	r := newTestReplier(minter, sender)
	inst := testResolvedInstallation(t)
	msg := testInboundForReply()

	r.Reply(context.Background(), inst, msg, engine.Result{
		Outcome: engine.OutcomeNeedsBinding,
		Sender:  "UALICE",
	})

	if minter.calls != 1 || minter.gotUser != "UALICE" {
		t.Fatalf("Mint called %d times for user %q", minter.calls, minter.gotUser)
	}
	if minter.gotWS != inst.WorkspaceID || minter.gotInst != inst.ID {
		t.Error("Mint must receive the resolved workspace + installation ids")
	}
	if sender.calls != 1 || sender.sent == nil {
		t.Fatalf("expected one reply, got %d", sender.calls)
	}
	if sender.sent.ChatID != "C1" || sender.sent.ThreadID != "1700000000.000200" {
		t.Errorf("reply target = %+v", sender.sent)
	}
	// The prompt must carry the redeem URL with the minted token, wrapped as a
	// Slack link so formatMrkdwn does not mangle the base64url token.
	wantLink := "<https://multica.example/slack/bind?token=tok_RAW-123|link your account>"
	if !strings.Contains(sender.sent.Text, wantLink) {
		t.Errorf("prompt text = %q, want it to contain %q", sender.sent.Text, wantLink)
	}
}

func TestReply_AgentOfflineAndArchived_PostNotices(t *testing.T) {
	for _, tc := range []struct {
		outcome engine.Outcome
		want    string
	}{
		{engine.OutcomeAgentOffline, agentOfflineText},
		{engine.OutcomeAgentArchived, agentArchivedText},
	} {
		sender := &fakeReplySender{}
		r := newTestReplier(&fakeBindingMinter{}, sender)
		r.Reply(context.Background(), testResolvedInstallation(t), testInboundForReply(), engine.Result{Outcome: tc.outcome})
		if sender.calls != 1 || sender.sent == nil || sender.sent.Text != tc.want {
			t.Errorf("outcome %s: got %d sends, text %q, want %q", tc.outcome, sender.calls, textOrEmpty(sender.sent), tc.want)
		}
	}
}

func TestReply_IngestedWithIssue_Confirms(t *testing.T) {
	sender := &fakeReplySender{}
	r := newTestReplier(&fakeBindingMinter{}, sender)
	r.Reply(context.Background(), testResolvedInstallation(t), testInboundForReply(), engine.Result{
		Outcome:         engine.OutcomeIngested,
		IssueID:         mustUUID(t, "55555555-5555-5555-5555-555555555555"),
		IssueIdentifier: "MUL-42",
		IssueTitle:      "Fix the thing",
	})
	if sender.calls != 1 || sender.sent == nil {
		t.Fatalf("expected one confirmation, got %d", sender.calls)
	}
	if !strings.Contains(sender.sent.Text, "MUL-42") || !strings.Contains(sender.sent.Text, "Fix the thing") {
		t.Errorf("confirmation text = %q", sender.sent.Text)
	}
}

func TestReply_IngestedWithoutIssue_Silent(t *testing.T) {
	sender := &fakeReplySender{}
	r := newTestReplier(&fakeBindingMinter{}, sender)
	// A plain chat message (no /issue) must NOT post — the agent's own reply
	// lands via the EventChatDone outbound subscriber.
	r.Reply(context.Background(), testResolvedInstallation(t), testInboundForReply(), engine.Result{
		Outcome: engine.OutcomeIngested,
	})
	if sender.calls != 0 {
		t.Errorf("plain ingested message must stay silent, got %d sends", sender.calls)
	}
}

func TestReply_Dropped_Silent(t *testing.T) {
	sender := &fakeReplySender{}
	r := newTestReplier(&fakeBindingMinter{}, sender)
	r.Reply(context.Background(), testResolvedInstallation(t), testInboundForReply(), engine.Result{Outcome: engine.OutcomeDropped})
	if sender.calls != 0 {
		t.Errorf("dropped outcome must stay silent, got %d sends", sender.calls)
	}
}

func TestIssueCreatedText(t *testing.T) {
	if got := issueCreatedText(engine.Result{IssueIdentifier: "MUL-7", IssueTitle: "Title"}); got != "✅ Created MUL-7 — Title" {
		t.Errorf("with title = %q", got)
	}
	if got := issueCreatedText(engine.Result{IssueNumber: 9}); got != "✅ Created #9" {
		t.Errorf("fallback to number = %q", got)
	}
}

func textOrEmpty(m *channel.OutboundMessage) string {
	if m == nil {
		return ""
	}
	return m.Text
}
