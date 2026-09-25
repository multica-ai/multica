package wecom

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
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
		t.Errorf("the visible text was eaten: %q", out)
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

// TestInboxCardKeepsAnOrdinaryBracketedTitleVerbatim pins the reason the
// mechanism changed. A live tenant showed backslash escaping is unusable here:
// "\[Bug\]" came back as an italic serif "Bug" with the brackets consumed —
// the client reads "\[...\]" as a display-math delimiter — and the
// conversation-list preview, which renders no markdown at all, showed the
// backslashes raw. "[Bug] ..." and "[WIP] ..." are everyday title forms, so
// the card must carry them through untouched.
func TestInboxCardKeepsAnOrdinaryBracketedTitleVerbatim(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	item := map[string]any{
		"type":  "status_changed",
		"title": "[Bug] 登录失败",
		"body":  "从 (todo) 到 (in_review)!",
	}
	out := buildInboxMarkdown(item, "ws-uuid", "acme")
	want := "**[状态变更] [Bug] 登录失败**\n从 (todo) 到 (in_review)!"
	if out != want {
		t.Fatalf("member text did not survive verbatim:\n got %q\nwant %q", out, want)
	}
	if strings.Contains(out, `\`) {
		t.Fatalf("a backslash reached the card: %q — WeCom either shows it raw in the list preview or reads it as a math delimiter in the bubble", out)
	}
}

// TestInboxCardNeverPutsCloseBracketNextToOpenParen pins the property the
// card's safety rests on: "](" never comes out adjacent. An inline link needs
// that adjacency under CommonMark ("immediately followed by") and under a
// naive `\[([^\]]+)\]\(([^)]+)\)` rewriter alike, and "![" needs it too, so
// neither a link nor an image can form.
//
// The app URL is cleared so the builder emits no link of its own; any "]("
// in the output would then be member-authored.
func TestInboxCardNeverPutsCloseBracketNextToOpenParen(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	cases := []struct {
		name  string
		title string
		body  string
	}{
		{"link in title", "[click here](http://evil.example)", "body"},
		{"link in body", "title", "and the body [too](http://evil.example)"},
		{"image", "![img](http://evil.example/x.png)", "body"},
		{"nested brackets", "[a[b]](http://evil.example)", "body"},
		// A member putting a backslash in front of the pair, betting the
		// separator lands somewhere it can be undone.
		{"member-written backslash", `x\](http://evil.example)`, `y\](http://evil.example)`},
		// Both truncation branches: the cut must not fuse a "]" and a "("
		// that were separated.
		{"body truncated at the seam", "t", strings.Repeat("a", 3900) + strings.Repeat("](x)", 100)},
		{"title truncated at the seam", strings.Repeat("标题](x)", 2000), "body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := buildInboxMarkdown(map[string]any{
				"type": "mentioned", "title": tc.title, "body": tc.body,
			}, "ws-uuid", "acme")
			if i := strings.Index(out, "]("); i >= 0 {
				t.Fatalf("\"](\" came out adjacent at byte %d — that is a working link in a card the bot signs: %q",
					i, window(out, i))
			}
		})
	}
}

// TestInboxCardSeamSurvivesEveryCutOffset sweeps the truncation point across a
// dense run of "](" so some iteration cuts at every offset inside the pattern.
// The two fixed seam cases above happen to land on one offset each; this is
// the same property without the luck.
func TestInboxCardSeamSurvivesEveryCutOffset(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	for pad := 0; pad < 12; pad++ {
		body := strings.Repeat("a", 3900+pad) + strings.Repeat("](x)", 60)
		title := strings.Repeat("标", 3900+pad) + strings.Repeat("](x)", 60)
		for _, tc := range []struct {
			name string
			item map[string]any
		}{
			{"body", map[string]any{"type": "mentioned", "title": "t", "body": body}},
			{"title", map[string]any{"type": "mentioned", "title": title, "body": "b"}},
		} {
			out := buildInboxMarkdown(tc.item, "ws-uuid", "acme")
			if i := strings.Index(out, "]("); i >= 0 {
				t.Fatalf("%s cut at pad=%d fused a \"]\" onto a \"(\" at byte %d: %q", tc.name, pad, i, window(out, i))
			}
			if n := utf8.RuneCountInString(out); n > inboxMarkdownMaxLen {
				t.Fatalf("%s cut at pad=%d: card is %d runes, cap is %d — WeCom refuses the frame and the push is lost",
					tc.name, pad, n, inboxMarkdownMaxLen)
			}
		}
	}
}

// TestInboxCardBudgetsTheSpacesItInserts: breaking the adjacency inserts a
// visible character, so a body dense in "](" is longer going out than it was
// coming in — 1.5x in the limit. The budget has to be computed on the grown
// text. Measured after truncation, the card would ship over the cap and WeCom
// would refuse the whole frame while the send path reports success.
func TestInboxCardBudgetsTheSpacesItInserts(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://multica.example")
	item := map[string]any{
		"type":  "mentioned",
		"title": "t",
		"body":  strings.Repeat("](", 4000), // 8000 runes in, 12000 if broken
	}
	out := buildInboxMarkdown(item, "ws-uuid", "acme")
	if n := utf8.RuneCountInString(out); n > inboxMarkdownMaxLen {
		t.Fatalf("card is %d runes, cap is %d — the inserted spaces were not budgeted, so WeCom refuses the frame and the push is lost",
			n, inboxMarkdownMaxLen)
	}
}

// TestInboxCardDefinesNoResolvableLinkReference is the card-level half of the
// reference-definition guard: it proves the builder actually runs it, on both
// fields, and that neither truncation branch hands the definition back.
//
// The card is the whole document — one buildInboxMarkdown result becomes the
// "markdown.content" of exactly one aibot_send_msg frame, and nothing batches
// two items into one frame — so the definition and the label that uses it both
// have to be inside a single member's title or body for the attack to work.
// Which is what the live tenant showed: one comment body carried both.
//
// The app URL is cleared so the builder emits no link of its own; any link in
// the parsed output is then member-authored.
func TestInboxCardDefinesNoResolvableLinkReference(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	for _, tc := range linkReferenceAttacks {
		t.Run(tc.name, func(t *testing.T) {
			// A definition cannot begin mid-paragraph, so put a blank line
			// between the title's line and the attack — the shape a comment
			// with a second paragraph has anyway.
			body := "看这里\n\n" + tc.body

			// The same card without the guard, composed the way
			// buildInboxMarkdown composes it. Asserting it resolves keeps
			// this case from quietly becoming a test of nothing.
			unguarded := "**[提及你] t**\n" + body
			if !hasDestinationTo(markdownDestinations(unguarded), "evil.example") {
				t.Fatalf("unguarded card %q resolves no link to evil.example — this case proves nothing", unguarded)
			}

			out := buildInboxMarkdown(map[string]any{
				"type": "mentioned", "title": "t", "body": body,
			}, "ws-uuid", "acme")
			if dests := markdownDestinations(out); hasDestinationTo(dests, "evil.example") {
				t.Fatalf("body: a member-defined link survived into the card: %q resolves %v", out, dests)
			}

			// Same through the title. A title with a line break can carry a
			// definition too, and it goes through a different length branch.
			out = buildInboxMarkdown(map[string]any{
				"type": "mentioned", "title": body, "body": "b",
			}, "ws-uuid", "acme")
			if dests := markdownDestinations(out); hasDestinationTo(dests, "evil.example") {
				t.Fatalf("title: a member-defined link survived into the card: %q resolves %v", out, dests)
			}
		})
	}
}

// TestInboxCardKeepsAReferenceLikeTitleVerbatim is the constraint that shaped
// the rule. "[Bug]: 登录失败" is an everyday title, and a guard that broke every
// "]:" would trade it away to close an attack that needs a real destination —
// the same trade the backslash escaping already lost on this renderer. Byte
// for byte, or the guard is too blunt.
func TestInboxCardKeepsAReferenceLikeTitleVerbatim(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	item := map[string]any{
		"type":  "status_changed",
		"title": "[Bug]: 登录失败",
		"body":  "[WIP]: 明天再看\n\n[复现步骤]: 见 (todo) 到 (in_review)!",
	}
	out := buildInboxMarkdown(item, "ws-uuid", "acme")
	want := "**[状态变更] [Bug]: 登录失败**\n[WIP]: 明天再看\n\n[复现步骤]: 见 (todo) 到 (in_review)!"
	if out != want {
		t.Fatalf("member text did not survive verbatim:\n got %q\nwant %q", out, want)
	}
}

// TestInboxCardSeamKeepsDefinitionsBrokenAtEveryCutOffset sweeps the
// truncation point across a dense run of definitions. Cutting drops a suffix,
// so it cannot fuse a "]" back onto the ":" the guard separated — this is that
// claim under test rather than in a comment, at every offset inside the
// pattern, and it holds the card inside the cap while it is at it.
func TestInboxCardSeamKeepsDefinitionsBrokenAtEveryCutOffset(t *testing.T) {
	t.Setenv("WECOM_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	for pad := 0; pad < 12; pad++ {
		filler := strings.Repeat("a", 3900+pad)
		dense := strings.Repeat("[x]: https://evil.example\n\n[x]\n\n", 40)
		for _, tc := range []struct {
			name string
			item map[string]any
		}{
			{"body", map[string]any{"type": "mentioned", "title": "t", "body": filler + "\n\n" + dense}},
			{"title", map[string]any{"type": "mentioned", "title": filler + "\n\n" + dense, "body": "b"}},
		} {
			out := buildInboxMarkdown(tc.item, "ws-uuid", "acme")
			if dests := markdownDestinations(out); hasDestinationTo(dests, "evil.example") {
				t.Fatalf("%s cut at pad=%d let a member-defined link through: resolves %v", tc.name, pad, dests)
			}
			if n := utf8.RuneCountInString(out); n > inboxMarkdownMaxLen {
				t.Fatalf("%s cut at pad=%d: card is %d runes, cap is %d — WeCom refuses the frame and the push is lost",
					tc.name, pad, n, inboxMarkdownMaxLen)
			}
		}
	}
}

// TestInboxCardBudgetsTheSpacesTheDefinitionBreakInserts is the "](" budget
// case for the other break: a body that is nothing but definitions grows by
// one rune per line, and the budget has to be computed on the grown text or
// the card ships over the cap and WeCom drops the whole frame while the send
// path reports success.
func TestInboxCardBudgetsTheSpacesTheDefinitionBreakInserts(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://multica.example")
	item := map[string]any{
		"type":  "mentioned",
		"title": "t",
		"body":  strings.Repeat("[a]: https://evil.example\n", 500),
	}
	out := buildInboxMarkdown(item, "ws-uuid", "acme")
	if n := utf8.RuneCountInString(out); n > inboxMarkdownMaxLen {
		t.Fatalf("card is %d runes, cap is %d — the inserted spaces were not budgeted, so WeCom refuses the frame and the push is lost",
			n, inboxMarkdownMaxLen)
	}
}

// window returns a short readable slice around i for a failure message.
func window(s string, i int) string {
	lo, hi := i-10, i+10
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}
