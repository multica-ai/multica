package dingtalk

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSessionTitleFromMessage(t *testing.T) {
	if got := sessionTitleFromMessage("  讨论天气  "); got != "讨论天气" {
		t.Errorf("title = %q, want trimmed seed", got)
	}
	if got := sessionTitleFromMessage(""); got != "" {
		t.Errorf("empty text must yield empty title (engine falls back), got %q", got)
	}
	long := ""
	for i := 0; i < 60; i++ {
		long += "字"
	}
	if got := sessionTitleFromMessage(long); len([]rune(got)) != 50 {
		t.Errorf("title should cap at 50 runes, got %d", len([]rune(got)))
	}
}

func TestAppendInput_ComposesMediaBody(t *testing.T) {
	var ws, session, sender, inst, claim pgtype.UUID
	ws.Bytes[0], session.Bytes[0], sender.Bytes[0], inst.Bytes[0], claim.Bytes[0] = 1, 2, 3, 4, 5
	ws.Valid, session.Valid, sender.Valid, inst.Valid, claim.Valid = true, true, true, true, true

	staged := []engine.StagedMedia{{Filename: "image-1.png", URL: "https://files.test/i1.png"}}
	p := engine.AppendParams{
		SessionID:      session,
		Sender:         sender,
		InstallationID: inst,
		ClaimToken:     claim,
		WorkspaceID:    ws,
		Staged:         staged,
		MediaChatBind:  true,
		Message: channel.InboundMessage{
			MessageID: "m1",
			Text:      "look at this",
			Segments: []channel.Segment{
				{Text: "look at this ", MediaIdx: -1},
				{MediaIdx: 0},
			},
			PendingMedia: []channel.PendingMedia{{Kind: channel.MsgTypeImage, Ref: "c1"}},
		},
	}
	in := appendInput(p)
	wantBody := "look at this ![image-1.png](https://files.test/i1.png)"
	if in.Body != wantBody {
		t.Errorf("Body = %q, want %q", in.Body, wantBody)
	}
	// The command source is the image-STRIPPED Message.Text (the Router's own
	// isIssueTurn source), never the composed Body — so image markdown can
	// neither flip the /issue decision nor leak into the parsed title.
	if in.CommandText != "look at this" {
		t.Errorf("CommandText = %q, want the image-stripped Message.Text", in.CommandText)
	}
	if in.WorkspaceID != ws || !in.MediaChatBind || len(in.Staged) != 1 {
		t.Errorf("media fields not passed through: %+v", in)
	}
	if in.SessionID != session || in.Sender != sender || in.InstallationID != inst ||
		in.MessageID != "m1" || in.ClaimToken != claim {
		t.Errorf("base fields not passed through: %+v", in)
	}
}

func TestAppendInput_PlainTextUnchanged(t *testing.T) {
	p := engine.AppendParams{Message: channel.InboundMessage{Text: "hello there", MessageID: "m2"}}
	in := appendInput(p)
	if in.Body != "hello there" || in.CommandText != "hello there" {
		t.Errorf("plain text must pass through unchanged: %+v", in)
	}
	if len(in.Staged) != 0 || in.MediaChatBind {
		t.Errorf("no media fields expected: %+v", in)
	}
}

func TestDingTalkSessionRouting_P2PCarriesStaffID(t *testing.T) {
	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-1",
		ChatType: channel.ChatTypeP2P,
		SenderID: "staff-7",
	}}
	key, cfg := dingtalkSessionRouting(msg)
	if key != "cid-1" {
		t.Errorf("binding key = %q, want conversation id", key)
	}
	var dc dingtalkBindingConfig
	if err := json.Unmarshal(cfg, &dc); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if dc.ConversationType != convTypeP2P || dc.ConversationID != "cid-1" || dc.StaffID != "staff-7" {
		t.Errorf("p2p config = %+v", dc)
	}
}

func TestDingTalkSessionRouting_GroupOmitsStaffID(t *testing.T) {
	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-2",
		ChatType: channel.ChatTypeGroup,
		SenderID: "staff-7",
	}}
	_, cfg := dingtalkSessionRouting(msg)
	var dc dingtalkBindingConfig
	_ = json.Unmarshal(cfg, &dc)
	if dc.ConversationType != convTypeGroup || dc.StaffID != "" {
		t.Errorf("group config must omit staff id: %+v", dc)
	}
}

func TestOutboundTarget_RoundTripsBindingConfig(t *testing.T) {
	_, cfg := dingtalkSessionRouting(channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-3",
		ChatType: channel.ChatTypeP2P,
		SenderID: "staff-3",
	}})
	target := outboundTarget(db.ChannelChatSessionBinding{ChannelChatID: "cid-3", Config: cfg})
	if target.ConversationType != convTypeP2P || target.StaffID != "staff-3" || target.ConversationID != "cid-3" {
		t.Errorf("round-tripped target = %+v", target)
	}
}

func TestOutboundTarget_FallsBackToChatID(t *testing.T) {
	target := outboundTarget(db.ChannelChatSessionBinding{ChannelChatID: "cid-4"})
	if target.ConversationType != convTypeGroup || target.ConversationID != "cid-4" {
		t.Errorf("missing config must fall back to a group send on chat id: %+v", target)
	}
}
