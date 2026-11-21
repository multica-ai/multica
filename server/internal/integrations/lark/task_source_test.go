package lark

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskSource(t *testing.T) {
	text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
	for _, tc := range []struct {
		name     string
		delivery db.ChannelTaskDelivery
		want     *protocol.FeishuSource
	}{
		{
			name: "direct chat",
			delivery: db.ChannelTaskDelivery{
				ChannelType: "feishu", ChannelChatID: "oc_dm1", ChatType: "p2p",
				ChannelMessageID: text("om_1"), ChannelSenderID: text("ou_1"),
			},
			want: &protocol.FeishuSource{ChatID: "oc_dm1", ChatType: "p2p", MessageID: "om_1", SenderOpenID: "ou_1"},
		},
		{
			name: "topic session reads the real chat id from the config",
			delivery: db.ChannelTaskDelivery{
				ChannelType: "feishu", ChannelChatID: "oc_g1:omt_t1", ChatType: "group",
				Config:           []byte(`{"chat_id":"oc_g1"}`),
				ChannelMessageID: text("om_2"), ChannelThreadID: text("omt_t1"), ChannelSenderID: text("ou_2"),
			},
			want: &protocol.FeishuSource{ChatID: "oc_g1", ChatType: "group", MessageID: "om_2", ThreadID: "omt_t1", SenderOpenID: "ou_2"},
		},
		{
			name: "no recorded trigger keeps only the chat",
			delivery: db.ChannelTaskDelivery{
				ChannelType: "feishu", ChannelChatID: "oc_g2", ChatType: "group", Config: []byte(`{}`),
			},
			want: &protocol.FeishuSource{ChatID: "oc_g2", ChatType: "group"},
		},
		{
			name:     "other platform",
			delivery: db.ChannelTaskDelivery{ChannelType: "slack", ChannelChatID: "C1", ChatType: "group"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TaskSource(tc.delivery)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("TaskSource = %+v, want %+v", got, tc.want)
			}
		})
	}
}
