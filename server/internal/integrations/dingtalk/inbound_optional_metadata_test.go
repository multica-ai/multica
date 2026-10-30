package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDingTalkCallbackIgnoresUnusedWebhookMetadata(t *testing.T) {
	for _, metadata := range []string{
		`"sessionWebhook":"unused bearer","sessionWebhookExpiredTime":"unknown"`,
		`"sessionWebhook":{"unsupported":"shape"},"sessionWebhookExpiredTime":{}`,
		`"sessionWebhook":"unused bearer","sessionWebhookExpiredTime":2000000000000`,
		`"sessionWebhook":null,"sessionWebhookExpiredTime":null`,
	} {
		t.Run(metadata, func(t *testing.T) {
			wire := `{"msgId":"source","senderStaffId":"sender","conversationId":"group","conversationType":"2","isInAtList":true,"msgtype":"text","text":{"content":"/issue current question"},` + metadata + `}`
			var callback botCallbackData
			if err := json.Unmarshal([]byte(wire), &callback); err != nil {
				t.Fatalf("unused reply capability rejected the current input: %v", err)
			}
			msg, ok := inboundFromCallback(&callback, "app")
			if !ok || msg.Text != "/issue current question" || msg.CommandText != msg.Text || msg.MessageID != "source" {
				t.Fatalf("unused reply capability changed the input: %+v", msg)
			}
			if strings.Contains(string(msg.Raw), "webhook") || strings.Contains(string(msg.Raw), "unused bearer") {
				t.Fatal("unused reply capability survived into the normalized input")
			}
		})
	}
}
