package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Use the sealed-input reader and the real outbound transport. The expected
// wire text deliberately retains history: current-turn boundaries are not
// available in the sealed Markdown, and must not be guessed from quote syntax.
func TestOutboundSealedImagesUseVisiblePlaceholders(t *testing.T) {
	const attachment = "![](/api/attachments/01a08f95-e516-7c96-8164-5856dbee3a47/download)"
	const screenshot = "> 111\n> ![](/api/attachments/01a08f95-e512-79a0-bb86-29ce6b43cbdf/download)\n> 结合这两张图，你能联想到什么？\n\n@YYClaw\n\n" + attachment + "\n\n222"
	for _, tc := range []struct{ name, input, wantPrefix string }{
		{"screenshot", screenshot, "> \\> 111  \n> \\> \\[Image\\]  \n> \\> 结合这两张图，你能联想到什么？  \n>   \n> @YYClaw  \n>   \n> \\[Image\\]  \n>   \n> 222\n\n---\n\n"},
		{"image only", attachment, "> \\[Image\\]\n\n---\n\n"},
		{"images before preview cutoff", strings.Repeat(attachment+"\n", 4) + "current question", "> \\[Image\\]  \n> \\[Image\\]  \n> \\[Image\\]  \n> \\[Image\\]  \n> current question\n\n---\n\n"},
	} {
		for _, private := range []bool{false, true} {
			name := tc.name + "/group"
			if private {
				name = tc.name + "/private"
			}
			t.Run(name, func(t *testing.T) {
				d := newDingtalkSendServer(t)
				config, err := json.Marshal(installConfig{AppID: "app", RobotCode: "robot", AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret"))})
				if err != nil {
					t.Fatal(err)
				}
				taskID, sid := sessionUUID(46), sessionUUID(45)
				q := deliveryOnlyOutboundQueries{
					delivery:     db.ChannelTaskDelivery{ChannelType: string(TypeDingTalk), ChannelChatID: "conversation", ChatType: string(channel.ChatTypeGroup)},
					installation: db.ChannelInstallation{ID: sessionUUID(47), Status: "active", Config: config},
					task:         db.AgentTaskQueue{ChatInputTaskID: taskID}, channelIngested: true,
					input: []db.ChatMessage{{ID: sessionUUID(48), ChannelIngested: true, Content: tc.input}},
				}
				if private {
					q.delivery.ChatType = string(channel.ChatTypeP2P)
					q.delivery.Config = []byte(`{"conversation_type":"1","staff_id":"staff"}`)
				}
				const answer = "answer with its own image: " + attachment
				err = NewOutbound(q, nil, NewClient(nil, d.srv.URL), nil, nil).processEvent(context.Background(), events.Event{
					Type: protocol.EventChatDone, TaskID: util.UUIDToString(taskID), ChatSessionID: util.UUIDToString(sid),
					Payload: protocol.ChatDonePayload{Content: answer},
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(d.sendBodies) != 1 {
					t.Fatalf("sent %d messages, want one", len(d.sendBodies))
				}
				var param markdownParam
				if err := json.Unmarshal([]byte(d.sendBodies[0]["msgParam"].(string)), &param); err != nil {
					t.Fatal(err)
				}
				want := tc.wantPrefix + answer
				if private {
					want = answer
				}
				if param.Text != want {
					t.Fatalf("wire text = %q, want %q", param.Text, want)
				}
				if q.input[0].Content != tc.input {
					t.Fatal("source display changed sealed model input")
				}
			})
		}
	}
}
