package card_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu/card"
)

// IssueCard renders the issue identifier, title, status, assignee, summary,
// and a "查看 Issue" button into valid Feishu interactive-card JSON.
func TestIssueCard_RendersValidJSON(t *testing.T) {
	t.Parallel()

	c := card.IssueCard(card.IssueData{
		Identifier:   "STA-2",
		Title:        "增加飞书等Channel",
		Status:       "in_progress",
		AssigneeName: "SeniorDev",
		Summary:      "在群里创建 Issue、回复评论等功能",
		URL:          "https://multica.to6.cn/issues/STA-2",
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Render produced invalid JSON: %v\n%s", err, jsonStr)
	}

	for _, want := range []string{
		"STA-2",
		"增加飞书等Channel",
		"in_progress",
		"SeniorDev",
		"查看 Issue",
		"https://multica.to6.cn/issues/STA-2",
	} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("card JSON missing %q\nbody: %s", want, jsonStr)
		}
	}
}

func TestIssueCard_NoAssignee(t *testing.T) {
	t.Parallel()

	c := card.IssueCard(card.IssueData{
		Identifier: "STA-44",
		Title:      "Feishu 卡片渲染",
		Status:     "todo",
		URL:        "https://multica.to6.cn/issues/STA-44",
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if strings.Contains(jsonStr, "负责人") {
		t.Error("card JSON should not mention assignee when empty")
	}
}

func TestIssueCard_MinimalFields(t *testing.T) {
	t.Parallel()

	c := card.IssueCard(card.IssueData{
		Identifier: "STA-1",
		Title:      "最小卡片",
		Status:     "done",
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("minimal card is not valid JSON: %v", err)
	}

	if strings.Contains(jsonStr, "查看 Issue") {
		t.Error("minimal card should not have button when URL is empty")
	}
}

// IssueCard auto-truncates Summary to 200 runes (counting Chinese characters
// as a single rune each). The contract is internal — callers should NOT need
// to truncate themselves.
func TestIssueCard_AutoTruncatesSummary(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("中", 250) // 250 runes of Chinese
	c := card.IssueCard(card.IssueData{
		Identifier: "STA-1",
		Title:      "x",
		Status:     "done",
		Summary:    long,
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	// Card schema escapes nothing for these characters; the rune count
	// should not exceed 200 inside the markdown body.
	// We assert via TruncateRunes directly to avoid a brittle JSON parse.
	want := card.TruncateRunes(long, 200)
	if !strings.Contains(jsonStr, want) {
		t.Errorf("expected truncated summary in card JSON, want %q present", want[:30]+"…")
	}
	if strings.Contains(jsonStr, long) {
		t.Errorf("untruncated summary should not appear verbatim in card JSON")
	}
}

// CommentCard renders the issue context, author, body (auto-truncated), and
// reply button.
func TestCommentCard_RendersValidJSON(t *testing.T) {
	t.Parallel()

	c := card.CommentCard(card.CommentData{
		IssueIdentifier: "STA-2",
		IssueTitle:      "增加飞书等Channel",
		AuthorName:      "Orion",
		Body:            "这是一条评论内容",
		URL:             "https://multica.to6.cn/issues/STA-2",
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Render produced invalid JSON: %v\n%s", err, jsonStr)
	}

	for _, want := range []string{"新评论", "Orion", "回复评论"} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("card JSON missing %q\nbody: %s", want, jsonStr)
		}
	}
}

// CommentCard.Body is auto-truncated to 200 runes by the package.
func TestCommentCard_AutoTruncatesBody(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 500) // 500 ASCII runes
	c := card.CommentCard(card.CommentData{
		IssueIdentifier: "STA-1",
		IssueTitle:      "x",
		AuthorName:      "u",
		Body:            long,
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if strings.Contains(jsonStr, long) {
		t.Errorf("untruncated 500-char body should not appear verbatim")
	}
	want := card.TruncateRunes(long, 200)
	if !strings.Contains(jsonStr, want) {
		t.Errorf("expected truncated body of length 200 to appear in card JSON")
	}
}

// BindPromptCard renders the bind explanation, the configurable TTL, and the
// "立即绑定" button.
func TestBindPromptCard_RendersValidJSON(t *testing.T) {
	t.Parallel()

	c := card.BindPromptCard(card.BindPromptData{
		URL:        "https://multica.to6.cn/bind?token=abc123",
		TTLMinutes: 10,
	})

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Render produced invalid JSON: %v\n%s", err, jsonStr)
	}

	for _, want := range []string{"绑定 Multica 账号", "立即绑定", "10 分钟"} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("card JSON missing %q\nbody: %s", want, jsonStr)
		}
	}
}

// BindPromptCard reflects whatever TTL the caller passes in (no hardcoded
// "10 分钟" string buried in the package).
func TestBindPromptCard_TTLConfigurable(t *testing.T) {
	t.Parallel()

	c := card.BindPromptCard(card.BindPromptData{
		URL:        "https://example.com",
		TTLMinutes: 30,
	})
	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !strings.Contains(jsonStr, "30 分钟") {
		t.Errorf("expected configurable TTL '30 分钟' in card JSON, got %s", jsonStr)
	}
	if strings.Contains(jsonStr, "10 分钟") {
		t.Errorf("hardcoded '10 分钟' should not appear when TTLMinutes=30")
	}
}

// NewCard with empty title falls back to a sane default rather than emitting
// an empty plain_text content (which Feishu rejects or renders as a blank).
func TestNewCard_EmptyTitleFallback(t *testing.T) {
	t.Parallel()

	c := card.NewCard("", "blue")
	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !strings.Contains(jsonStr, "Multica 通知") {
		t.Errorf("expected fallback title 'Multica 通知' when titleText is empty, body=%s", jsonStr)
	}
}

func TestNewCard_WithTemplate(t *testing.T) {
	t.Parallel()

	c := card.NewCard("测试标题", "green")
	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if !strings.Contains(jsonStr, "green") {
		t.Error("card JSON missing template colour")
	}
	if !strings.Contains(jsonStr, "测试标题") {
		t.Error("card JSON missing title")
	}
}

func TestCard_AddPrimitives(t *testing.T) {
	t.Parallel()

	c := card.NewCard("test", "blue")
	c.AddMarkdown("**bold** text")
	c.AddHR()
	c.AddPlainText("plain")
	c.AddButton("click", "https://example.com")

	jsonStr, err := c.Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for _, want := range []string{"lark_md", "bold", "plain_text", "button", "https://example.com"} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("card JSON missing %q\nbody: %s", want, jsonStr)
		}
	}
}

// TruncateRunes counts runes (not bytes), preserving multi-byte characters
// and falling back gracefully on edge cases.
func TestTruncateRunes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"empty", "", 5, ""},
		{"shorter than limit", "abc", 5, "abc"},
		{"exactly limit", "abcde", 5, "abcde"},
		{"ascii longer", "abcdefghij", 5, "abcde"},
		{"chinese", "中文测试abc", 4, "中文测试"},
		{"emoji", "a😀b😀c", 3, "a😀b"},
		{"zero limit", "abc", 0, ""},
		{"negative limit", "abc", -1, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := card.TruncateRunes(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}
