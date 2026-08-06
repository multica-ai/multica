package dingtalk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type captureChatSession struct{ append engine.AppendInput }

func (c *captureChatSession) EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error) {
	return pgtype.UUID{}, nil
}
func (c *captureChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	c.append = in
	return engine.AppendResult{}, nil
}
func (c *captureChatSession) BindMediaRefs(context.Context, engine.BindMediaInput) error { return nil }

func TestNewDingTalkResolverSetUsesDatabaseBackedIssueOrigin(t *testing.T) {
	set := NewDingTalkResolverSet(nil, nil, nil, nil, nil)
	if set.OriginType != originDingTalkChat {
		t.Fatalf("OriginType = %q, want %q", set.OriginType, originDingTalkChat)
	}
}

func TestSessionBinder_MapsCommandTextAndMediaDeadline(t *testing.T) {
	var session, sender, inst, claim pgtype.UUID
	session.Bytes[0], sender.Bytes[0], inst.Bytes[0], claim.Bytes[0] = 2, 3, 4, 5
	session.Valid, sender.Valid, inst.Valid, claim.Valid = true, true, true, true
	capture := &captureChatSession{}
	binder := &sessionBinder{session: capture}
	_, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		SessionID: session, Sender: sender, InstallationID: inst, ClaimToken: claim,
		MediaPendingSeconds: 45,
		Message: channel.InboundMessage{
			MessageID: "m1", Text: "[Image]\n/issue fix login", CommandText: "/issue fix login",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := capture.append
	if in.Body != "[Image]\n/issue fix login" || in.CommandText != "/issue fix login" {
		t.Fatalf("body/command = %q/%q", in.Body, in.CommandText)
	}
	if in.MediaPendingSeconds != 45 || in.SessionID != session || in.Sender != sender || in.InstallationID != inst || in.ClaimToken != claim {
		t.Fatalf("mapped append input = %+v", in)
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
