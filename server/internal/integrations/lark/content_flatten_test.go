package lark

import "testing"

// TestFlattenPostContent_IssueExample pins the exact rich-text `post`
// example from MUL-2951: a title line, a prose paragraph, and a
// paragraph mixing a text span with a hyperlink span. The link must
// render as "text (href)" so the URL survives into the agent's context.
func TestFlattenPostContent_IssueExample(t *testing.T) {
	t.Parallel()
	// Received-side post body.content (NOT locale-wrapped).
	raw := `{
		"title": "周报",
		"content": [
			[{ "tag": "text", "text": "本周完成：" }],
			[
				{ "tag": "text", "text": "Lark 集成" },
				{ "tag": "a", "href": "https://github.com/multica-ai/multica/pull/3277", "text": "PR #3277" }
			]
		]
	}`
	want := "周报\n本周完成：\nLark 集成 PR #3277 (https://github.com/multica-ai/multica/pull/3277)"
	if got := flattenPostContent(raw); got != want {
		t.Errorf("flattenPostContent()\n got = %q\nwant = %q", got, want)
	}
}

func TestFlattenPostContent_NoTitle(t *testing.T) {
	t.Parallel()
	raw := `{"content":[[{"tag":"text","text":"line one"}],[{"tag":"text","text":"line two"}]]}`
	want := "line one\nline two"
	if got := flattenPostContent(raw); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFlattenPostContent_MediaAndMentionSpans(t *testing.T) {
	t.Parallel()
	// at span carries the @_user_N placeholder (resolved later by
	// resolveMentions); media tags degrade to bracket placeholders;
	// emotion is skipped entirely.
	raw := `{"content":[[
		{"tag":"at","user_id":"@_user_1","user_name":""},
		{"tag":"text","text":"look"},
		{"tag":"img","image_key":"img_x"},
		{"tag":"emotion","emoji_type":"SMILE"}
	]]}`
	want := "@_user_1 look [Image]"
	if got := flattenPostContent(raw); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFlattenPostContent_AtPrefersResolvedName(t *testing.T) {
	t.Parallel()
	raw := `{"content":[[{"tag":"at","user_id":"@_user_1","user_name":"Tom"},{"tag":"text","text":"hi"}]]}`
	want := "@Tom hi"
	if got := flattenPostContent(raw); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFlattenPostContent_Malformed(t *testing.T) {
	t.Parallel()
	if got := flattenPostContent("not json"); got != "" {
		t.Errorf("malformed content should flatten to empty, got %q", got)
	}
	if got := flattenPostContent(""); got != "" {
		t.Errorf("empty content should flatten to empty, got %q", got)
	}
}

func TestFlattenContent_DispatchByType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		msgType string
		content string
		want    string
	}{
		{"text", "text", `{"text":"hello"}`, "hello"},
		{"image", "image", `{"image_key":"img_x"}`, "[Image]"},
		{"file", "file", `{"file_key":"f"}`, "[File]"},
		{"audio", "audio", `{"file_key":"f"}`, "[Audio]"},
		{"media", "media", `{"file_key":"f"}`, "[Video]"},
		{"sticker", "sticker", `{"file_key":"f"}`, "[Sticker]"},
		{"interactive", "interactive", `{"title":"t","card_link":{"url":"https://x"}}`, "t (https://x)"},
		{"share_chat", "share_chat", `{"chat_id":"oc"}`, "[Shared Chat]"},
		{"merge_forward", "merge_forward", `{"content":"Merged and Forwarded Message"}`, "[forwarded messages]"},
		{"unknown", "totally_new_type", `{}`, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := flattenContent(tc.msgType, tc.content); got != tc.want {
				t.Errorf("flattenContent(%q) = %q want %q", tc.msgType, got, tc.want)
			}
		})
	}
}

// TestFlattenInteractiveContent_LinkShareCard pins the link-share card
// shape — the exact payload a forwarded Lark message-link renders as.
// title + card_link.url must become "title (url)" so the agent can reach
// the shared resource; this is the regression case for the empty-body
// bug where the decoder left interactive cards unflattened.
func TestFlattenInteractiveContent_LinkShareCard(t *testing.T) {
	t.Parallel()
	raw := `{"title":"Shared project note","card_link":{"url":"https://example.com/shared-note","android_url":"","ios_url":"","pc_url":""},"elements":[]}`
	want := "Shared project note (https://example.com/shared-note)"
	if got := flattenInteractiveContent(raw); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFlattenInteractiveContent_Fallbacks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"url only", `{"card_link":{"url":"https://x"}}`, "https://x"},
		{"title only", `{"title":"t"}`, "t"},
		{"empty elements no title no url", `{"elements":[]}`, "[interactive card]"},
		{"empty string", "", ""},
		{"malformed", "not json", "[interactive card]"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := flattenInteractiveContent(tc.raw); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
