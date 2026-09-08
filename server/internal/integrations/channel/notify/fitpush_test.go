package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPushDesktopLink(t *testing.T) {
	const commentID = "66666666-6666-6666-6666-666666666666"
	item := map[string]any{
		"issue_id": testIssue,
		"details":  map[string]any{"comment_id": commentID},
	}
	want := "multica://issue/" + testIssue + "?comment=" + commentID + "&workspace=" + testWorkspace
	if got := pushDesktopLink(item, testWorkspace); got != want {
		t.Errorf("pushDesktopLink() = %q, want %q", got, want)
	}

	delete(item, "details")
	want = "multica://issue/" + testIssue + "?workspace=" + testWorkspace
	if got := pushDesktopLink(item, testWorkspace); got != want {
		t.Errorf("pushDesktopLink() without comment = %q, want %q", got, want)
	}
	if got := pushDesktopLink(map[string]any{}, testWorkspace); got != "" {
		t.Errorf("pushDesktopLink() without issue = %q, want empty", got)
	}
}

// FitPush and renderPush are two halves of one format: this reads the real
// renderer's output back and asserts the parts the recipient acts on survive a
// body long enough to blow any platform's budget. A plain tail cut passes a
// total-length assertion and fails this one.
func TestFitPushKeepsWhatTheRecipientActsOn(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com")
	issueID := "33333333-3333-3333-3333-333333333333"
	item := map[string]any{
		"type":     "status_changed",
		"title":    "Ship the thing",
		"issue_id": &issueID,
		"body":     strings.Repeat("蒜", 5000),
	}
	for _, replyable := range []bool{true, false} {
		text := renderPush(item, testWorkspace, "acme", "in_review", replyable)
		link := pushLink(item, testWorkspace, "acme")
		if link == "" {
			t.Fatal("pushLink returned empty; the app URL fixture is not taking effect")
		}

		const max = 4000
		got := FitPush(text, max)
		if n := utf8.RuneCountInString(got); n > max {
			t.Errorf("replyable=%v: %d runes, want at most %d", replyable, n, max)
		}
		if !strings.Contains(got, "Ship the thing") {
			t.Errorf("replyable=%v: dropped the title: %q", replyable, first(got))
		}
		if !strings.Contains(got, link) {
			t.Errorf("replyable=%v: dropped the deep link", replyable)
		}
		if strings.Contains(got, reviewReplyHint) != replyable {
			t.Errorf("replyable=%v: reply hint presence = %v", replyable, !replyable)
		}
		if !strings.Contains(got, reviewHandoffLabel) {
			t.Errorf("replyable=%v: dropped the review action label: %q", replyable, first(got))
		}
		if !strings.Contains(got, "…") {
			t.Errorf("replyable=%v: a truncated push carries no ellipsis", replyable)
		}
	}
}

func TestFitPushKeepsBlockedReplyHint(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com")
	issueID := "33333333-3333-3333-3333-333333333333"
	item := map[string]any{
		"type":     "status_changed",
		"title":    "Blocked task",
		"issue_id": &issueID,
		"body":     strings.Repeat("阻", 5000),
	}
	got := FitPush(renderPush(item, testWorkspace, "acme", "blocked", true), 4000)
	if !strings.Contains(got, blockedLabel) {
		t.Errorf("dropped the blocked action label: %q", first(got))
	}
	if !strings.HasSuffix(got, blockedReplyHint) {
		t.Errorf("blocked reply hint is not preserved at the tail: %q", got)
	}
}

// A push already inside the budget must come back byte-identical — no stray
// ellipsis, no reflowed tail.
func TestFitPushLeavesAShortPushAlone(t *testing.T) {
	text := "**[状态变更] Ship it**\nbody\nhttps://app.example.com/acme/issues/x\n" + replyHint
	if got := FitPush(text, 4000); got != text {
		t.Errorf("FitPush rewrote a push that already fits:\n got %q\nwant %q", got, text)
	}
}

// The degenerate case: a title alone over budget. There is no body to spend, so
// the title is what gets cut — and cutting it is not licence to drop the deep
// link, which still fits and is still the recipient's only route to the
// notification.
func TestFitPushCapsATitleThatAloneExceedsTheBudget(t *testing.T) {
	const link = "https://app.example.com/a/issues/x"
	text := "**[状态变更] " + strings.Repeat("蒜", 500) + "**\n" + link
	got := FitPush(text, 100)
	if n := utf8.RuneCountInString(got); n > 100 {
		t.Errorf("%d runes, want at most 100", n)
	}
	if got == "" {
		t.Fatal("FitPush returned nothing for an over-long title")
	}
	if !strings.Contains(got, link) {
		t.Errorf("a long title crowded out the deep link that still fits: %q", got)
	}
	if !strings.Contains(got, "蒜") {
		t.Errorf("kept the tail but dropped the title entirely: %q", got)
	}
}

// The tail is only worth protecting while it can share the budget with
// something. A budget too small for even one rune of title plus the ellipsis
// leaves nothing to identify the notification by, so the title is what wins.
func TestFitPushDropsATailThatCannotFitAtAll(t *testing.T) {
	text := "**[状态变更] Ship it**\nbody\nhttps://app.example.com/acme/issues/abc\n" + replyHint
	got := FitPush(text, 8)
	if n := utf8.RuneCountInString(got); n > 8 {
		t.Errorf("%d runes, want at most 8", n)
	}
	if strings.Contains(got, "https://") {
		t.Errorf("spent a budget too small for the title on the link: %q", got)
	}
	if !strings.Contains(got, "状态变更") {
		t.Errorf("dropped what identifies the notification: %q", got)
	}
}

// A body line may be byte-identical to the reply hint, or a bare https:// URL of
// its own. The tail is renderPush's grammar — at most one link line then at most
// one hint, at the very end — not whatever the body happens to look like.
func TestFitPushIsNotFooledByBodyLinesThatLookLikeTheTail(t *testing.T) {
	const link = "https://app.example.com/acme/issues/abc"
	text := "**[状态变更] Ship it**\n" +
		replyHint + "\nhttps://app.example.com/acme/issues/decoy\n" +
		strings.Repeat("蒜", 200) + "\n" + link + "\n" + replyHint
	const max = 120
	got := FitPush(text, max)
	if n := utf8.RuneCountInString(got); n > max {
		t.Errorf("%d runes, want at most %d", n, max)
	}
	if !strings.Contains(got, link) {
		t.Errorf("a decoy line displaced the real deep link: %q", got)
	}
	if !strings.HasSuffix(got, replyHint) {
		t.Errorf("the reply hint is not last: %q", got)
	}
}

// A body of nothing but blank lines is still a body by strings.Join's reckoning,
// and a title long enough to leave it no room sends this down the fallback. The
// deep link has to come through that path too.
func TestFitPushHandlesABlankBody(t *testing.T) {
	const link = "https://app.example.com/acme/issues/abc"
	text := "**[状态变更] " + strings.Repeat("蒜", 200) + "**\n\n\n\n" + link
	const max = 100
	got := FitPush(text, max)
	if n := utf8.RuneCountInString(got); n > max {
		t.Errorf("%d runes, want at most %d", n, max)
	}
	if !strings.Contains(got, link) {
		t.Errorf("blank body lines crowded out the deep link: %q", got)
	}
}

// The smallest budgets have no room for a cut marker plus content. They are not
// reachable from any real platform, but the <= maxRunes guarantee is
// unconditional, so they may not panic or overflow either.
func TestFitPushSurvivesATinyBudget(t *testing.T) {
	text := "**[状态变更] Ship it**\nbody\nhttps://app.example.com/acme/issues/abc\n" + replyHint
	for _, max := range []int{1, 2, 3} {
		got := FitPush(text, max)
		if n := utf8.RuneCountInString(got); n > max {
			t.Errorf("FitPush(_, %d) = %d runes, want at most %d", max, n, max)
		}
	}
}

// A member-authored body may end in a bare URL of its own. Recognising the
// tail by value alone walks straight past it into the body, preserving it whole
// in front of the real deep link — and at adversarial length it crowds the real
// link out of the budget entirely, which is the exact failure FitPush exists to
// prevent.
func TestFitPushDoesNotMistakeAMemberURLForTheDeepLink(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com")
	issueID := "33333333-3333-3333-3333-333333333333"
	memberURL := "https://evil.example/" + strings.Repeat("a", 3950)
	item := map[string]any{
		"type":     "status_changed",
		"title":    "Ship the thing",
		"issue_id": &issueID,
		"body":     strings.Repeat("蒜", 100) + "\n" + memberURL,
	}
	text := renderPush(item, testWorkspace, "acme", "", true)
	link := pushLink(item, testWorkspace, "acme")

	const max = 4000
	got := FitPush(text, max)
	if n := utf8.RuneCountInString(got); n > max {
		t.Errorf("%d runes, want at most %d", n, max)
	}
	if !strings.Contains(got, link) {
		t.Error("a member's trailing bare URL crowded out the real deep link")
	}
	if !strings.Contains(got, replyHint) {
		t.Error("a member's trailing bare URL crowded out the reply hint")
	}
	if !strings.Contains(got, "Ship the thing") {
		t.Error("dropped the title")
	}
}

// The +2 boundary, from both sides. This is the whole of the guard's condition,
// so it is what holds the constant in place: one rune more of budget than the
// tail plus the cut marker is where the deep link starts surviving. Widening the
// margin without meaning to would move this flip and silently drop links at
// budgets that used to keep them.
func TestFitPushFlipsAtTheTailPlusMarkerBoundary(t *testing.T) {
	head := "**[状态变更] Hello world**"
	link := "https://a.io/" + strings.Repeat("q", 40)
	text := head + "\nsome body\n" + link
	tailRunes := 1 + utf8.RuneCountInString(link)

	justUnder := FitPush(text, tailRunes+1)
	if strings.Contains(justUnder, link) {
		t.Errorf("kept a tail with no room for a title beside it: %q", justUnder)
	}
	if !strings.Contains(justUnder, "状态变更") {
		t.Errorf("gave up the title too: %q", justUnder)
	}

	justOver := FitPush(text, tailRunes+2)
	if !strings.Contains(justOver, link) {
		t.Errorf("dropped a link that had exactly enough room: %q", justOver)
	}
	if n := utf8.RuneCountInString(justOver); n > tailRunes+2 {
		t.Errorf("%d runes, want at most %d", n, tailRunes+2)
	}
}

// An unconfigured app URL makes renderPush emit no deep link, so a replyable
// push's tail is the hint alone. It is still a tail and still has to survive.
func TestFitPushKeepsAHintOnlyTail(t *testing.T) {
	text := "**[状态变更] Ship it**\n" + strings.Repeat("蒜", 500) + "\n" + replyHint
	const max = 100
	got := FitPush(text, max)
	if n := utf8.RuneCountInString(got); n > max {
		t.Errorf("%d runes, want at most %d", n, max)
	}
	if !strings.HasSuffix(got, replyHint) {
		t.Errorf("dropped a reply hint that had room: %q", got)
	}
}

func TestFitPushRejectsANonPositiveBudget(t *testing.T) {
	for _, max := range []int{0, -1} {
		if got := FitPush("anything", max); got != "" {
			t.Errorf("FitPush(_, %d) = %q, want empty", max, got)
		}
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

func first(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// PlainHead undoes renderPush's title emphasis for platforms that render no
// markdown. It must touch that and nothing else: the body is the member's own
// words, and the deep link is the recipient's only route in on a platform
// whose pushes are not replyable.
func TestPlainHead(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"renderPush's full shape",
			"**[状态变更] Ship it**\nbody\nhttps://app.example.com/a/issues/x\n直接回复本条消息即可处理。",
			"[状态变更] Ship it\nbody\nhttps://app.example.com/a/issues/x\n直接回复本条消息即可处理。",
		},
		{"title only", "**[状态变更] Ship it**", "[状态变更] Ship it"},
		{
			// The body is the member's text; whatever they wrote stays.
			"a body of its own asterisks is left alone",
			"**[状态变更] T**\nsee **this**",
			"[状态变更] T\nsee **this**",
		},
		{"an unemphasised head is untouched", "plain\nbody", "plain\nbody"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlainHead(tt.in); got != tt.want {
				t.Errorf("PlainHead(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
