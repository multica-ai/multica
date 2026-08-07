package wecom

import (
	"os"
	"strings"
	"unicode/utf8"
	"testing"
)

func TestBuildInboxMarkdown_TitleBodyLink(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "https://example.com")
	item := map[string]any{
		"type":     "status_changed",
		"title":    "登录页 500 错误",
		"body":     "from: todo\nto: in_review",
		"issue_id": "9194c058-e8a4-4c15-9c65-86d1784ba715",
	}
	got := buildInboxMarkdown(item, "ws-uuid", "acme")
	if !strings.Contains(got, "**[状态变更] 登录页 500 错误**") {
		t.Fatalf("missing typed title header: %q", got)
	}
	if !strings.Contains(got, "from: todo\nto: in_review") {
		t.Fatalf("missing body: %q", got)
	}
	if !strings.Contains(got, "[查看详情](https://example.com/acme/inbox?issue=9194c058-e8a4-4c15-9c65-86d1784ba715)") {
		t.Fatalf("missing detail link: %q", got)
	}
}

func TestBuildInboxMarkdown_UnknownTypeFallsBackToDefault(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "https://example.com")
	item := map[string]any{"type": "some_new_type", "title": "hi"}
	got := buildInboxMarkdown(item, "ws-uuid", "acme")
	if !strings.Contains(got, "**[新消息] hi**") {
		t.Fatalf("expected fallback type label 新消息, got %q", got)
	}
}

func TestBuildInboxMarkdown_FallsBackToWorkspaceUUIDWhenSlugMissing(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "https://example.com")
	item := map[string]any{"type": "new_comment", "title": "t", "issue_id": "iid"}
	got := buildInboxMarkdown(item, "ws-uuid", "")
	if !strings.Contains(got, "https://example.com/ws-uuid/inbox?issue=iid") {
		t.Fatalf("expected workspace uuid path segment, got %q", got)
	}
}

func TestBuildInboxMarkdown_NoAppURLDropsLink(t *testing.T) {
	// Unset every var buildInboxMarkdown looks at.
	for _, k := range []string{"WECOM_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, v) })
		}
		os.Unsetenv(k)
	}
	item := map[string]any{"type": "new_comment", "title": "t"}
	got := buildInboxMarkdown(item, "ws-uuid", "acme")
	if strings.Contains(got, "查看详情") {
		t.Fatalf("link section must be omitted when no app url is configured: %q", got)
	}
}

func TestBuildInboxMarkdown_NonHTTPSAppURLIsRejected(t *testing.T) {
	for _, k := range []string{"WECOM_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, v) })
		}
		os.Unsetenv(k)
	}
	t.Setenv("WECOM_APP_URL", "http://insecure.example.com")
	item := map[string]any{"type": "new_comment", "title": "t"}
	got := buildInboxMarkdown(item, "ws-uuid", "acme")
	if strings.Contains(got, "insecure.example.com") {
		t.Fatalf("http:// override must be dropped, got %q", got)
	}
}

func TestBuildInboxMarkdown_TruncatesLongBody(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "https://example.com")
	body := strings.Repeat("我", 5000) // 5000 runes, exceeds 4000 cap
	item := map[string]any{"type": "new_comment", "title": "hi", "body": body, "issue_id": "iid"}
	got := buildInboxMarkdown(item, "ws-uuid", "acme")
	if !strings.Contains(got, "...") {
		t.Fatalf("expected truncation marker, got tail %q", got[len(got)-50:])
	}
	if !strings.HasSuffix(got, "acme/inbox?issue=iid)") {
		t.Fatalf("link must survive truncation, got tail %q", got[len(got)-80:])
	}
}

func TestBuildInboxMarkdown_HandlesPointerBodyAndIssueID(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "https://example.com")
	bodyStr := "详情"
	issueIDStr := "iid"
	item := map[string]any{
		"type":     "new_comment",
		"title":    "hi",
		"body":     &bodyStr,
		"issue_id": &issueIDStr,
	}
	got := buildInboxMarkdown(item, "ws-uuid", "acme")
	if !strings.Contains(got, "详情") {
		t.Fatalf("expected pointer body to be dereferenced, got %q", got)
	}
	if !strings.Contains(got, "issue=iid") {
		t.Fatalf("expected pointer issue_id to be dereferenced, got %q", got)
	}
}

func TestBuildInboxMarkdown_EmptyItemReturnsEmpty(t *testing.T) {
	got := buildInboxMarkdown(map[string]any{}, "ws", "slug")
	if got != "" {
		t.Fatalf("expected empty output for empty item, got %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in     string
		max    int
		expect string
	}{
		{"abc", 0, ""},
		{"abc", 3, "abc"},
		{"abc", 2, "ab"},
		{"你好世界", 2, "你好"},
		{"你好世界", 4, "你好世界"},
		{"你好世界", 5, "你好世界"},
	}
	for _, tc := range cases {
		if got := truncateRunes(tc.in, tc.max); got != tc.expect {
			t.Errorf("truncateRunes(%q,%d)=%q; want %q", tc.in, tc.max, got, tc.expect)
		}
	}
}

// TestInboxCardDoesNotRenderMemberAuthoredLinks: the card carries the bot's
// authority — it arrives from the bot, not from whoever wrote the issue — so
// link syntax in a member-written title must not render as a link.
func TestInboxCardDoesNotRenderMemberAuthoredLinks(t *testing.T) {
	item := map[string]any{
		"type":  "mentioned",
		"title": "[click here](http://evil.example)",
		"body":  "and the body [too](http://evil.example)",
	}
	out := buildInboxMarkdown(item, "ws-uuid", "acme")
	if strings.Contains(out, "](http://evil.example)") {
		t.Fatalf("member-authored link syntax rendered as a link in a bot-authored card: %q", out)
	}
	if !strings.Contains(out, "click here") {
		t.Errorf("escaping ate the visible text: %q", out)
	}
}

// TestInboxCardFitsTheCapEvenWithAHugeTitle: WeCom refuses the whole frame
// past the cap, and the sender is told it was delivered — so a card that does
// not fit is a message silently lost, not a message truncated.
func TestInboxCardFitsTheCapEvenWithAHugeTitle(t *testing.T) {
	huge := strings.Repeat("标题", 4000) // far past inboxMarkdownMaxLen on its own
	t.Setenv("MULTICA_APP_URL", "https://multica.example")
	item := map[string]any{"type": "mentioned", "title": huge, "body": "body"}
	out := buildInboxMarkdown(item, "ws-uuid", "acme")
	if n := utf8.RuneCountInString(out); n > inboxMarkdownMaxLen {
		t.Fatalf("card is %d runes, cap is %d — WeCom refuses the frame and the push is lost", n, inboxMarkdownMaxLen)
	}
	if !strings.Contains(out, "查看详情") {
		t.Errorf("the view-detail affordance was dropped: %q", out[:120])
	}
}
