package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueCreatedText(t *testing.T) {
	issueID := pgtype.UUID{Valid: true}
	if got := issueCreatedText(engine.Result{IssueID: issueID, IssueIdentifier: "MUL-42", IssueTitle: "Fix login"}, ""); got != "✅ Created MUL-42 — Fix login" {
		t.Fatalf("got %q", got)
	}
	if got := issueCreatedText(engine.Result{IssueID: issueID, IssueNumber: 7}, ""); got != "✅ Created #7" {
		t.Fatalf("fallback got %q", got)
	}
}

func TestIssueDuplicateText(t *testing.T) {
	issueID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	got := issueDuplicateText(engine.Result{
		IssueID: issueID, IssueIdentifier: "MUL-42", IssueTitle: "Fix login", IssueDuplicate: true,
	}, "")
	if got != "⚠️ Not created — active issue MUL-42 already exists: Fix login" {
		t.Fatalf("duplicate text = %q", got)
	}
}

func TestDroppedReplyText(t *testing.T) {
	issueMsg := channel.InboundMessage{Text: "[Image]", CommandText: "/issue login is broken", AddressedToBot: true}
	cases := []struct {
		name string
		res  engine.Result
		msg  channel.InboundMessage
		want string
	}{
		{"non-member /issue gets refusal",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			issueMsg, issueNotMemberText},
		{"revoked installation /issue gets disconnected notice",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonRevokedInstallation},
			issueMsg, issueDisabledText},
		{"duplicate /issue stays silent",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonDuplicate},
			issueMsg, ""},
		{"non-member plain chat stays silent",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			channel.InboundMessage{Text: "hello", AddressedToBot: true}, ""},
		{"unaddressed group /issue stays silent",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			channel.InboundMessage{Text: "/issue x", AddressedToBot: false}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := droppedReplyText(tc.res, tc.msg); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReplierCommandConfirmationsKeepSuccessMarker(t *testing.T) {
	config, err := json.Marshal(installConfig{AppID: "app", RobotCode: "robot", AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret"))})
	if err != nil {
		t.Fatal(err)
	}
	inst := engine.ResolvedInstallation{WorkspaceID: pgtype.UUID{Bytes: [16]byte{42}, Valid: true}, Platform: db.ChannelInstallation{Config: config}}
	for _, tc := range []struct {
		name, text string
		result     engine.Result
		success    bool
		plainReply bool
	}{
		{name: "clear", result: engine.Result{Outcome: engine.OutcomeFreshPending}, text: "Fresh start ready. Your next chat message will run without previous context.", success: true},
		{name: "new", result: engine.Result{Outcome: engine.OutcomeChatStarted}, text: "Started a new Multica chat. Your next message will enter it.", success: true},
		{name: "issue", result: engine.Result{Outcome: engine.OutcomeIngested, IssueID: pgtype.UUID{Valid: true}, IssueWorkspaceSlug: "team-b", IssueIdentifier: "MUL-42", IssueTitle: "✅ Keep this title"}, text: "Created [MUL\\-42](https://multica.example/team-b/issues/00000000-0000-0000-0000-000000000000) — ✅ Keep this title", success: true},
		{name: "issue without title", result: engine.Result{Outcome: engine.OutcomeIngested, IssueID: pgtype.UUID{Valid: true}, IssueWorkspaceSlug: "team-b", IssueNumber: 7}, text: "Created [\\#7](https://multica.example/team-b/issues/00000000-0000-0000-0000-000000000000)", success: true},
		{name: "duplicate issue", result: engine.Result{Outcome: engine.OutcomeIngested, IssueID: pgtype.UUID{Valid: true}, IssueWorkspaceSlug: "team-b", IssueIdentifier: "MUL-42", IssueTitle: "✅ Keep this title", IssueDuplicate: true}, text: "⚠️ Not created — active issue [MUL\\-42](https://multica.example/team-b/issues/00000000-0000-0000-0000-000000000000) already exists: ✅ Keep this title"},
		{name: "offline", result: engine.Result{Outcome: engine.OutcomeAgentOffline}, text: "⚠️ The agent is offline, so this message won't be processed automatically."},
		{name: "ordinary reply", text: "✅ Keep this reply", plainReply: true},
	} {
		for _, route := range []struct {
			name     string
			chatType channel.ChatType
			path     string
		}{
			{name: "group proactive", chatType: channel.ChatTypeGroup, path: pathSendGroup},
			{name: "private", chatType: channel.ChatTypeP2P, path: pathSendP2P},
		} {
			t.Run(tc.name+"/"+route.name, func(t *testing.T) {
				d := newDingtalkSendServer(t)
				msg := channel.InboundMessage{
					Source: channel.Source{ChannelType: TypeDingTalk, ChatType: route.chatType, ChatID: "conversation", SenderID: "staff"},
					Text:   "✅ Keep this quote",
				}
				r := NewOutboundReplier(OutboundReplierConfig{
					Client: NewClient(nil, d.srv.URL), AppURL: "https://multica.example",
				})
				if tc.plainReply {
					if err := r.post(context.Background(), inst, msg, tc.text); err != nil {
						t.Fatal(err)
					}
				} else {
					r.Reply(context.Background(), inst, msg, tc.result)
				}
				if len(d.sendBodies) != 1 || d.lastPath != route.path {
					t.Fatalf("sent %d messages to %q, want one to %q", len(d.sendBodies), d.lastPath, route.path)
				}
				var got markdownParam
				if err := json.Unmarshal([]byte(d.lastBody["msgParam"].(string)), &got); err != nil {
					t.Fatal(err)
				}
				want := tc.text
				if tc.success {
					want = "✅ " + want
				}
				if route.chatType != channel.ChatTypeP2P {
					if tc.result.Outcome != engine.OutcomeAgentOffline && tc.result.Outcome != engine.OutcomeAgentArchived {
						want = "> ✅ Keep this quote\n\n---\n\n" + want
					}
				}
				if got.Text != want {
					t.Fatalf("reply = %q, want %q", got.Text, want)
				}
			})
		}
	}
}

func TestReplierRecoveredGenerationUsesOnlyStableRoute(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chatType channel.ChatType
		outcome  engine.Outcome
		path     string
	}{
		{"group offline", channel.ChatTypeGroup, engine.OutcomeAgentOffline, pathSendGroup},
		{"direct archived", channel.ChatTypeP2P, engine.OutcomeAgentArchived, pathSendP2P},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			config, err := json.Marshal(installConfig{AppID: "app", RobotCode: "robot", AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret"))})
			if err != nil {
				t.Fatal(err)
			}
			msg := channel.InboundMessage{Source: channel.Source{ChannelType: TypeDingTalk, ChatType: tc.chatType, ChatID: "stable-conversation"}}
			if tc.chatType == channel.ChatTypeP2P {
				msg.Source.SenderID = "stable-recipient"
			}
			r := NewOutboundReplier(OutboundReplierConfig{Client: NewClient(nil, d.srv.URL)})
			r.Reply(context.Background(), engine.ResolvedInstallation{Platform: db.ChannelInstallation{Config: config}}, msg, engine.Result{Outcome: tc.outcome})
			if d.lastPath != tc.path {
				t.Fatalf("path=%q, want %q", d.lastPath, tc.path)
			}
			var param markdownParam
			if err := json.Unmarshal([]byte(d.lastBody["msgParam"].(string)), &param); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(param.Text, "> ") || strings.Contains(param.Text, "@") || d.lastBody["at"] != nil {
				t.Fatalf("recovered notice gained live-message targeting: %v", d.lastBody)
			}
			if tc.chatType == channel.ChatTypeP2P {
				ids, _ := d.lastBody["userIds"].([]any)
				if len(ids) != 1 || ids[0] != "stable-recipient" {
					t.Fatalf("recipient=%v", ids)
				}
			} else if d.lastBody["openConversationId"] != "stable-conversation" {
				t.Fatalf("conversation=%v", d.lastBody["openConversationId"])
			}
		})
	}
}
