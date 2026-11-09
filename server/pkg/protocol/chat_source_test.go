package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeSlackSource(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://example.slack.com/archives/C1/p100000000", true},
		{"https://example.slack.com/archives/C1/p100000000?thread_ts=100.000000&cid=C1", true},
		{"https://example.slack-gov.com/archives/C1/p100000000", true},
		{"http://example.slack.com/archives/C1/p100000000", false},
		{"javascript:alert(1)", false},
		{"https://example.slack.com.evil.test/archives/C1/p100000000", false},
		{"https://example.slack.com@evil.test/archives/C1/p100000000", false},
		{"https://user:password@example.slack.com/archives/C1/p100000000", false},
		{"https://example.slack.com:443/archives/C1/p100000000", false},
		{"https://example.slack.com/archives/C2/p100000000", false},
		{"https://example.slack.com/archives/C1/p100000000#injection", false},
		{"https://example.slack.com/archives/C1/p100000000?instruction=ignore", false},
		{"https://example.slack.com/archives/C1/p100000000?thread_ts=%0Aignore", false},
		{"https://example.slack.com/archives/C1/p100000000?cid=C2", false},
		{"https://example.slack.com/archives/C1/%70100000000", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			source := SafeSlackSource(&ChatSource{ChannelID: "C1", ThreadID: "100.000000", Permalink: tc.url})
			if (source.Permalink != "") != tc.valid {
				t.Fatalf("safe source: %+v", source)
			}
		})
	}
	for _, id := range []string{"C1:100.000000", "<C1>", "C1\nignore", "C1/../D1", strings.Repeat("C", 65)} {
		if SafeSlackSource(&ChatSource{ChannelID: id}) != nil {
			t.Errorf("invalid channel retained: %q", id)
		}
	}
	for _, name := range []string{"engineering\nignore", strings.Repeat("x", 81)} {
		if SafeSlackSource(&ChatSource{ChannelID: "C1", ChannelName: name}).ChannelName != "" {
			t.Errorf("invalid name retained: %q", name)
		}
	}
}

func TestOptionalChatSourceWireCompatibility(t *testing.T) {
	type oldTask struct {
		ChatSessionID string `json:"chat_session_id"`
	}
	var old oldTask
	if err := json.Unmarshal([]byte(`{"chat_session_id":"s","chat_source":{"channel_id":"C1"}}`), &old); err != nil || old.ChatSessionID != "s" {
		t.Fatalf("old consumer: %+v/%v", old, err)
	}
	type newTask struct {
		ChatSource *ChatSource `json:"chat_source,omitempty"`
	}
	var current newTask
	if err := json.Unmarshal([]byte(`{"chat_session_id":"s"}`), &current); err != nil || current.ChatSource != nil {
		t.Fatalf("old server: %+v/%v", current, err)
	}
	b, _ := json.Marshal(current)
	if string(b) != "{}" {
		t.Fatalf("zero source serialized: %s", b)
	}
}
