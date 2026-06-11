package transport

import (
	"encoding/json"
	"testing"
)

// TestMessagePayload_Extra verifies the forward-compatibility contract: known
// fields decode into their typed slots, and any unmodeled keys are preserved in
// Extra (mirroring the TS `[key: string]: unknown` index signature) so a future
// server-added field is never silently dropped.
func TestMessagePayload_Extra(t *testing.T) {
	raw := `{
		"type": 1,
		"content": "hi",
		"some_new_flag": true,
		"server_added": {"nested": 7},
		"count": 42
	}`
	var p MessagePayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if p.Type != MsgText {
		t.Errorf("Type = %d, want %d", p.Type, MsgText)
	}
	if p.Content != "hi" {
		t.Errorf("Content = %q, want %q", p.Content, "hi")
	}

	// Extra must contain exactly the unknown keys, and none of the known ones.
	wantExtra := map[string]bool{"some_new_flag": true, "server_added": true, "count": true}
	if len(p.Extra) != len(wantExtra) {
		t.Fatalf("Extra has %d keys (%v), want %d", len(p.Extra), keysOf(p.Extra), len(wantExtra))
	}
	for k := range wantExtra {
		if _, ok := p.Extra[k]; !ok {
			t.Errorf("Extra missing unknown key %q", k)
		}
	}
	for _, known := range []string{"type", "content", "url", "name", "mention", "reply"} {
		if _, leaked := p.Extra[known]; leaked {
			t.Errorf("known key %q leaked into Extra", known)
		}
	}

	// Extra values are the raw bytes and remain decodable.
	var flag bool
	if err := json.Unmarshal(p.Extra["some_new_flag"], &flag); err != nil || !flag {
		t.Errorf("some_new_flag did not round-trip: %v / %v", err, flag)
	}
}

// TestMessagePayload_NoExtra verifies a payload with only known fields leaves
// Extra nil (not an empty allocated map).
func TestMessagePayload_NoExtra(t *testing.T) {
	var p MessagePayload
	if err := json.Unmarshal([]byte(`{"type":1,"content":"x"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Extra != nil {
		t.Errorf("Extra should be nil when no unknown fields present, got %v", p.Extra)
	}
}

// TestMessagePayload_NestedMentionReply verifies the nested mention/reply objects
// decode through the custom UnmarshalJSON (the single-pass rewrite must still
// route these known keys to their typed targets).
func TestMessagePayload_NestedMentionReply(t *testing.T) {
	raw := `{
		"type": 1,
		"content": "@bob hello",
		"mention": {"uids": ["bob"], "entities": [{"uid":"bob","offset":0,"length":4}]},
		"reply": {"from_uid": "alice", "from_name": "Alice", "payload": {"type":1,"content":"orig"}}
	}`
	var p MessagePayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Mention == nil || len(p.Mention.UIDs) != 1 || p.Mention.UIDs[0] != "bob" {
		t.Fatalf("mention not decoded: %+v", p.Mention)
	}
	if len(p.Mention.Entities) != 1 || p.Mention.Entities[0].Length != 4 {
		t.Errorf("mention entity not decoded: %+v", p.Mention.Entities)
	}
	if p.Reply == nil || p.Reply.FromUID != "alice" || p.Reply.Payload == nil {
		t.Fatalf("reply not decoded: %+v", p.Reply)
	}
	if p.Reply.Payload.Content != "orig" {
		t.Errorf("nested reply payload content = %q, want %q", p.Reply.Payload.Content, "orig")
	}
	if p.Extra != nil {
		t.Errorf("Extra should be nil, got %v", p.Extra)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
