package slack

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/slack-go/slack/slackevents"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestDMReplyPolicyFromInstallation(t *testing.T) {
	for _, tc := range []struct {
		name, config, channelType, thread, wantKey, wantReply string
	}{
		{"default DM", `{}`, "im", "", "D1", ""},
		{"disabled DM", `{"dm_replies_in_threads":false}`, "im", "", "D1", ""},
		{"enabled DM", `{"dm_replies_in_threads":true}`, "im", "", "D1", "111.0"},
		{"enabled DM follow-up", `{"dm_replies_in_threads":true}`, "im", "100.0", "D1", "100.0"},
		{"default DM follow-up", `{}`, "im", "100.0", "D1", "100.0"},
		{"group unaffected", `{"dm_replies_in_threads":true}`, "mpim", "", "D1:111.0", "111.0"},
		{"group follow-up unaffected", `{"dm_replies_in_threads":true}`, "mpim", "100.0", "D1:100.0", "100.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg map[string]any
			if err := json.Unmarshal([]byte(tc.config), &cfg); err != nil {
				t.Fatal(err)
			}
			cfg["app_token_encrypted"] = "dGVzdA=="
			cfg["bot_user_id"] = "UBOT"
			raw, _ := json.Marshal(cfg)
			var got channel.InboundMessage
			calls := 0
			built, err := newSlackFactory(ChannelDeps{})(channel.Config{
				Raw: raw,
				Handler: func(_ context.Context, msg channel.InboundMessage) error {
					got = msg
					calls++
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			c := built.(*slackChannel)
			err = c.dispatchEventsAPI(context.Background(), eventsAPI(&slackevents.MessageEvent{
				User: "UALICE", Text: "<@UBOT> hello", Channel: "D1", ChannelType: tc.channelType,
				TimeStamp: "111.0", ThreadTimeStamp: tc.thread,
			}), compileMentionRe("UBOT"))
			if err != nil || calls != 1 {
				t.Fatalf("dispatch: err=%v calls=%d", err, calls)
			}
			key, _, reply := slackSessionRouting(got)
			if key != tc.wantKey || reply != tc.wantReply {
				t.Fatalf("binding/reply = %q/%q, want %q/%q", key, reply, tc.wantKey, tc.wantReply)
			}
			if got.MessageID != "111.0" {
				t.Fatalf("message identity changed: %q", got.MessageID)
			}
		})
	}
}
