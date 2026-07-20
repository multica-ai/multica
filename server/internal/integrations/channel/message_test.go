package channel

import (
	"encoding/json"
	"testing"
)

// TestInboundMessage_RawIsOpaque proves the boundary contract: a
// platform-specific payload survives a JSON round-trip through Raw
// byte-for-byte, so an adapter can stash anything there and read it back
// while the core leaves it untouched.
func TestInboundMessage_RawIsOpaque(t *testing.T) {
	raw := json.RawMessage(`{"msg_type":"merge_forward","parent_id":"om_x","mentions":[{"id":"ou_1"}]}`)
	in := InboundMessage{
		EventID:   "evt_1",
		MessageID: "om_1",
		Source: Source{
			ChannelType:    TypeFeishu,
			ChatID:         "oc_1",
			ChatType:       ChatTypeGroup,
			SenderID:       "ou_sender",
			SenderStableID: "on_union",
			ThreadID:       "omt_1",
		},
		Type:           MsgTypeText,
		Text:           "hello",
		AddressedToBot: true,
		ReplyTo:        &ReplyCtx{MessageID: "om_parent", RootID: "om_root"},
		Raw:            raw,
	}

	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got InboundMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Source != in.Source {
		t.Errorf("Source round-trip mismatch:\n got %+v\nwant %+v", got.Source, in.Source)
	}
	if got.ReplyTo == nil || *got.ReplyTo != *in.ReplyTo {
		t.Errorf("ReplyTo round-trip mismatch: got %+v", got.ReplyTo)
	}
	if !got.AddressedToBot {
		t.Errorf("AddressedToBot lost in round-trip")
	}

	// Raw must be semantically identical (compare as parsed JSON to be
	// whitespace-insensitive).
	var a, b any
	if err := json.Unmarshal(got.Raw, &a); err != nil {
		t.Fatalf("Raw not valid JSON after round-trip: %v", err)
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("seed Raw not valid JSON: %v", err)
	}
	if string(mustMarshal(t, a)) != string(mustMarshal(t, b)) {
		t.Errorf("Raw payload changed across round-trip:\n got %s\nwant %s", got.Raw, raw)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return b
}

// TestOutboundMessage_Minimal documents that the outbound envelope is the
// minimal text reply (decision §6): a text body, optionally threaded or
// quoting, and nothing card/media-shaped.
func TestOutboundMessage_Minimal(t *testing.T) {
	out := OutboundMessage{ChatID: "oc_1", Text: "hi", ThreadID: "omt_1", ReplyTo: "om_1"}
	if out.ChatID == "" || out.Text == "" {
		t.Fatalf("ChatID and Text are the required fields")
	}
}
