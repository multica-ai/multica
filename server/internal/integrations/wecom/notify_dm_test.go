package wecom

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// testPushRef is a non-empty PushRef. The wecom tests never assert on it
// beyond feeding the relay key, so any stable pair of ids will do.
var testPushRef = notify.PushRef{InboxItemID: "item-1", RecipientUserID: "user-1"}

// newTestRelay builds a relay whose publish() always reaches a fanout target,
// so DeliverDM's off-lease handoff branch has somewhere to route to.
func newTestRelay(t *testing.T) *RelayOutbound {
	t.Helper()
	return NewRelayOutbound(&fanoutRelay{}, nil, RelayConfig{Shards: 1}, slog.Default())
}

// WeCom sends over the aibot socket, which reports no platform message id.
// Delivered-with-no-id is the honest answer: the push happened and nobody can
// reply to it.
func TestDeliverDMReportsDeliveredWithoutAMessageID(t *testing.T) {
	q := &fakeOutboundQueries{}
	o, instID, conn := newOutboundWithConn(t, q)
	binding := db.ChannelUserBinding{InstallationID: instID, ChannelUserID: "T_USER_1"}

	res, err := o.DeliverDM(context.Background(), testPushRef, binding, "**[in_review] Ship it**")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateDelivered {
		t.Errorf("State = %v, want StateDelivered", res.State)
	}
	if res.MessageID != "" {
		t.Errorf("MessageID = %q, want empty: wecom send acks carry no id", res.MessageID)
	}

	body := conn.sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("chatid = %v, want T_USER_1", body["chatid"])
	}
	// The binding's channel_user_id is the bot-scoped T-* userid, which WeCom
	// treats as the chatid of a single chat. Never guess group.
	if body["chat_type"] != float64(chatTypeSingleInt) {
		t.Errorf("chat_type = %v, want %d", body["chat_type"], chatTypeSingleInt)
	}
}

// EventInboxNew fires on whichever replica the load balancer picked; the WS
// lease lives on exactly one. Handing the frame to that replica is neither a
// delivery nor a failure.
func TestDeliverDMHandsOffWhenThisReplicaHasNoSocket(t *testing.T) {
	q := &fakeOutboundQueries{}
	o := NewOutbound(q, newSendersRegistry(), slog.Default(), WithRelay(newTestRelay(t)))
	binding := db.ChannelUserBinding{InstallationID: mustTestUUID(t), ChannelUserID: "T_USER_1"}

	res, err := o.DeliverDM(context.Background(), testPushRef, binding, "text")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateHandedOff {
		t.Errorf("State = %v, want StateHandedOff", res.State)
	}
}

// sentMarkdown reads the markdown body out of the first send frame. The
// content sits under the "markdown" object, not at the top level.
func sentMarkdown(t *testing.T, conn *recordingConn) string {
	t.Helper()
	md, ok := conn.sendBody(t, 0)["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("send frame has no markdown object: %v", conn.sendBody(t, 0))
	}
	s, _ := md["content"].(string)
	if s == "" {
		t.Fatal("send frame markdown content is empty")
	}
	return s
}

// The push text is assembled in the shared notify package, which splices a
// member-authored issue title and body into it. markdown.go's rule is that
// every WeCom caller doing that runs breakMemberLinks first: a title like
// "[click here](http://evil.example)" would otherwise arrive as a working link
// inside a message the bot signs.
func TestDeliverDMBreaksMemberAuthoredLinks(t *testing.T) {
	q := &fakeOutboundQueries{}
	o, instID, conn := newOutboundWithConn(t, q)
	binding := db.ChannelUserBinding{InstallationID: instID, ChannelUserID: "T_USER_1"}

	if _, err := o.DeliverDM(context.Background(), testPushRef, binding,
		"**[in_review] [重置密码](http://evil.example)**"); err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}

	sent := sentMarkdown(t, conn)
	if strings.Contains(sent, "](") {
		t.Errorf("member-authored link survived into the bot message: %q", sent)
	}
	if !strings.Contains(sent, "重置密码") {
		t.Errorf("the guard dropped the title text instead of separating it: %q", sent)
	}
}

// WeCom refuses an over-long markdown body, so the cap has to be enforced on
// this side of the shared renderer, which has no per-platform length budget.
//
// The link case covers the seam between the two adjustments: the cap runs
// after breakMemberLinks and must not undo it. It only drops characters, so it
// cannot put a "]" back beside a "(" — this pins that.
//
// Both cases also pin the deep link surviving. A WeCom push is never
// replyable, so the link is the recipient's only route to the notification,
// and a plain tail cut over the assembled message is exactly what removes it.
func TestDeliverDMTruncatesToTheMarkdownLimit(t *testing.T) {
	const link = "https://app.example.com/acme/issues/abc"
	tests := []struct {
		name string
		text string
	}{
		{"a long body", "**[状态变更] Ship it**\n" +
			strings.Repeat("蒜", inboxMarkdownMaxLen+500) + "\n" + link},
		{"a long body carrying link syntax", "**[状态变更] Ship it**\n" +
			strings.Repeat("[点这里](http://evil.example)", inboxMarkdownMaxLen) + "\n" + link},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeOutboundQueries{}
			o, instID, conn := newOutboundWithConn(t, q)
			binding := db.ChannelUserBinding{InstallationID: instID, ChannelUserID: "T_USER_1"}

			if _, err := o.DeliverDM(context.Background(), testPushRef, binding, tt.text); err != nil {
				t.Fatalf("DeliverDM: %v", err)
			}

			sent := sentMarkdown(t, conn)
			if got := len([]rune(sent)); got > inboxMarkdownMaxLen {
				t.Errorf("sent %d runes, want at most %d", got, inboxMarkdownMaxLen)
			}
			if strings.Contains(sent, "](") {
				t.Error("the cap put a close bracket back next to an open paren")
			}
			if !strings.Contains(sent, link) {
				t.Error("the cap dropped the deep link, the only route a WeCom recipient has")
			}
			if !strings.Contains(sent, "Ship it") {
				t.Error("the cap dropped the title")
			}
		})
	}
}
