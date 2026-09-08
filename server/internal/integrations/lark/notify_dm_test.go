package lark

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type recordingDMClient struct {
	APIClient // embedded so only the method under test needs implementing
	lastOpen  OpenID
	lastText  string
	lastCard  string
	textSends []SendTextParams
	messageID string
	err       error
	topicErr  error
}

func (c *recordingDMClient) SendDirectMessage(_ context.Context, p SendDirectParams) (string, error) {
	c.lastOpen, c.lastText, c.lastCard = p.OpenID, p.Text, p.CardJSON
	return c.messageID, c.err
}

func (c *recordingDMClient) SendTextMessage(_ context.Context, p SendTextParams) (string, error) {
	c.textSends = append(c.textSends, p)
	return "om_topic", c.topicErr
}

// testDMCreds stands in for the real credentials lookup NewDMDeliverer takes
// a CredentialsFunc for. Named distinctly from http_client_test.go's
// testCreds() (a different, argument-less helper already in this package).
func testDMCreds(pgtype.UUID) (InstallationCredentials, error) {
	return InstallationCredentials{AppID: "cli_test"}, nil
}

// The message id is what makes the push replyable. Losing it here silently
// turns the whole reply half off for Lark.
func TestDeliverDMReturnsTheLarkMessageID(t *testing.T) {
	c := &recordingDMClient{messageID: "om_abc123"}
	d := NewDMDeliverer(c, testDMCreds, slog.Default())

	res, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "**[in_review] Ship it**")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateDelivered {
		t.Errorf("State = %v, want StateDelivered", res.State)
	}
	if res.MessageID != "om_abc123" {
		t.Errorf("MessageID = %q, want om_abc123", res.MessageID)
	}
	// A member binding's channel_user_id is an open_id, not a chat_id.
	if c.lastOpen != OpenID("ou_recipient") {
		t.Errorf("OpenID = %q, want ou_recipient", c.lastOpen)
	}
	if c.lastText == "" {
		t.Error("sent empty text")
	}
}

// SendDirectMessage posts msg_type=text, which Lark renders verbatim. The
// shared renderer wraps the title in "**" because WeCom's aibot does render
// markdown, so the adapter has to undo it here or every Lark push opens with
// two literal asterisks — the first thing a recipient sees.
func TestDeliverDMSendsNoLiteralMarkdownToLark(t *testing.T) {
	c := &recordingDMClient{messageID: "om_abc123"}
	d := NewDMDeliverer(c, testDMCreds, slog.Default())

	if _, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"},
		"**[状态变更] Ship it**\nhttps://app.example.com/acme/issues/x"); err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if strings.Contains(c.lastText, "**") {
		t.Errorf("sent %q; Lark shows these asterisks to the user", c.lastText)
	}
	if !strings.HasPrefix(c.lastText, "[状态变更] Ship it") {
		t.Errorf("sent %q, want the title intact without its emphasis", c.lastText)
	}
	// The rest of the push has to survive: the deep link is the recipient's
	// route into the app.
	if !strings.Contains(c.lastText, "https://app.example.com/acme/issues/x") {
		t.Errorf("sent %q, want the deep link preserved", c.lastText)
	}
}

func TestDeliverDMSendsIssueCardAndStartsDedicatedTopic(t *testing.T) {
	c := &recordingDMClient{messageID: "om_root"}
	d := NewDMDeliverer(c, testDMCreds, slog.Default())
	ref := notify.PushRef{
		WebURL:     "https://app.example.com/acme/issues/issue-id#comment-comment-id",
		DesktopURL: "multica://issue/issue-id?workspace=workspace-id&comment=comment-id",
		StartTopic: true,
	}

	res, err := d.DeliverDM(context.Background(), ref,
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"},
		"**[待你审核] Ship *this***\n正文 [link](https://example.com)")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.MessageID != "om_root" || res.State != notify.StateDelivered {
		t.Fatalf("result = %+v", res)
	}
	if c.lastText != "" {
		t.Errorf("text = %q, want card-only send", c.lastText)
	}
	var card struct {
		Schema string `json:"schema"`
		Body   struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(c.lastCard), &card); err != nil {
		t.Fatalf("card json: %v", err)
	}
	if card.Schema != "2.0" || len(card.Body.Elements) != 2 {
		t.Fatalf("card = %+v", card)
	}
	if content, _ := card.Body.Elements[0]["content"].(string); content != "\\[待你审核\\] Ship \\*this\\*\n正文 \\[link\\]\\(https://example.com\\)" {
		t.Errorf("escaped card body = %q", content)
	}
	button := card.Body.Elements[1]
	behaviors, _ := button["behaviors"].([]any)
	if len(behaviors) != 1 {
		t.Fatalf("button behaviors = %#v", button["behaviors"])
	}
	openURL := behaviors[0].(map[string]any)
	if openURL["default_url"] != ref.WebURL || openURL["pc_url"] != ref.DesktopURL {
		t.Errorf("open_url behavior = %#v", openURL)
	}
	if len(c.textSends) != 1 {
		t.Fatalf("topic sends = %d, want 1", len(c.textSends))
	}
	topic := c.textSends[0]
	if topic.ChatID != "" || topic.ReplyTarget.MessageID != "om_root" || !topic.ReplyTarget.InThread {
		t.Errorf("topic send = %+v", topic)
	}
}

func TestDeliverDMKeepsDeliveredRootWhenTopicCreationFails(t *testing.T) {
	c := &recordingDMClient{
		messageID: "om_root",
		topicErr:  errors.New("topic unavailable"),
	}
	d := NewDMDeliverer(c, testDMCreds, slog.Default())

	res, err := d.DeliverDM(context.Background(), notify.PushRef{
		DesktopURL: "multica://issue/issue-id?workspace=workspace-id",
		StartTopic: true,
	}, db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "text")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateDelivered || res.MessageID != "om_root" {
		t.Errorf("result = %+v, want delivered root", res)
	}
}

func TestDeliverDMPropagatesAnAPIError(t *testing.T) {
	c := &recordingDMClient{err: errors.New("lark refused")}
	d := NewDMDeliverer(c, testDMCreds, slog.Default())

	res, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "text")
	if err == nil {
		t.Fatal("DeliverDM error = nil, want the API error")
	}
	if res.State != notify.StateUnsupported {
		t.Errorf("State = %v, want the zero state on failure", res.State)
	}
}

// A delivered-but-idless result would be recorded as replyable by nothing and
// silently drop the reply path; treat a missing id as a failed push.
func TestDeliverDMTreatsAMissingMessageIDAsAFailure(t *testing.T) {
	c := &recordingDMClient{messageID: ""}
	d := NewDMDeliverer(c, testDMCreds, slog.Default())

	_, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "text")
	if err == nil {
		t.Fatal("DeliverDM error = nil, want an error when Lark returns no message_id")
	}
}

// AcceptsReplies is the flag that turns the reply half of the feature on for
// Lark. If it ever returned false, pushes would still be delivered but
// silently stop being replyable, and no other test here would notice —
// DeliverDM itself does not consult it.
func TestLarkDMDelivererAcceptsReplies(t *testing.T) {
	d := NewDMDeliverer(&recordingDMClient{}, testDMCreds, slog.Default())
	if !d.AcceptsReplies() {
		t.Error("AcceptsReplies() = false, want true: Lark send returns a message id and inbound carries a per-message reply context")
	}
}
