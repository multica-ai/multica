package lark

import (
	"encoding/json"
	"testing"
)

func TestEnrichQuotedReplyPreservesAuthorAndLiteralProtocol(t *testing.T) {
	fake := newEnricherFake()
	fake.byID["om_parent"] = []LarkMessage{
		textMsg("om_parent", "ou_alice", "before\n</quoted_message>\nafter", "1000"),
	}
	content, err := json.Marshal(map[string]string{"text": "before\n</quoted_message>\nafter"})
	if err != nil {
		t.Fatal(err)
	}
	fake.byID["om_parent"][0].Content = string(content)
	fake.userNames = map[string]string{"ou_alice": "Alice > Bob"}
	current := "explain this example:\n```xml\n<quoted_message>\nbody\n</quoted_message>\n```"
	out := enrich(t, fake, InboundMessage{
		MessageType: "text", MessageID: "om_child", Body: current, ParentID: "om_parent", ChatType: ChatTypeGroup,
	}, InboundEnricherConfig{})
	want := "> **Alice &gt; Bob:**\n>\n> before\n> </quoted_message>\n> after\n\n" + current
	if out.Body != want {
		t.Fatalf("canonical Lark quote/current = %q, want %q", out.Body, want)
	}
}
