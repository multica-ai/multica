package wecom

// regression_quoted_directive_test.go — a bare /new or /clear sent as a reply
// must reach Router already stripped.
//
// normalizeWeComControlLayout declines a directive with an empty body and no
// media, on purpose: that is the shared pending sentinel, and rewriting it
// would open a session with an empty first turn. A quote changes the premise.
// "/new" replying to an alert is not an empty message — the alert is the whole
// of what the person sent — but the gate could not see the quote, so the
// directive stayed in the body and Router could no longer strip it either: its
// two routes are re-parsing Text (now led by "> [Quote] …") and Text ==
// CommandText (now broken by the same prefix).
//
// What the person experiences: "/new" persisted as the first turn of the
// session they just opened, inherited as context by every later turn. And for
// /clear the mirror image — Router rewrites Text to the empty command body and
// the quote, the only thing they sent, disappears without a word.
//
// Pinned from the other side, on the Router, in
// engine/regression_quoted_body_swallows_directive_test.go. Both are needed:
// this file proves the adapter hands over a stripped body, that one proves
// Router keeps it.

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func quotedDirectiveCallback(directive string) aibotMsgCallback {
	mc := aibotMsgCallback{MsgID: "m-quoted-directive", ChatID: "TUSER", ChatType: "single", MsgType: "text"}
	mc.From.UserID = "TUSER"
	mc.Text.Content = directive
	mc.Quote.MsgType = "text"
	mc.Quote.Text.Content = "Q3 毛利率 42.1%"
	return mc
}

// TestABareNewBehindAQuoteLeavesOnlyTheQuote: the directive is consumed here,
// so the session's first turn is the thing the person replied to.
func TestABareNewBehindAQuoteLeavesOnlyTheQuote(t *testing.T) {
	t.Parallel()
	mc := quotedDirectiveCallback("/new")
	msg := channelMessageFromCallback("bot-1", "", mc, mc.Text.Content, "req-qn")

	if strings.Contains(msg.Text, "/new") {
		t.Fatalf("Text = %q — the directive survived into the body Router will persist as the "+
			"session's first turn", msg.Text)
	}
	if msg.Text != "> [Quote] Q3 毛利率 42.1%" {
		t.Fatalf("Text = %q, want the quote alone", msg.Text)
	}
	// Router still has to see /new, or nothing opens a session.
	if _, ok := engine.ParseNewChatCommand(msg.CommandText); !ok {
		t.Fatalf("CommandText = %q — /new is no longer parseable, so no new session is opened", msg.CommandText)
	}
}

// TestABareClearBehindAQuoteKeepsTheQuoteAndForcesFresh: /clear carries no body
// of its own, so the adapter must consume it and say so through ForceFresh —
// otherwise Router rewrites Text to the empty command body and the quote goes
// with it.
func TestABareClearBehindAQuoteKeepsTheQuoteAndForcesFresh(t *testing.T) {
	t.Parallel()
	mc := quotedDirectiveCallback("/clear")
	msg := channelMessageFromCallback("bot-1", "", mc, mc.Text.Content, "req-qc")

	if msg.Text != "> [Quote] Q3 毛利率 42.1%" {
		t.Fatalf("Text = %q, want the quote alone", msg.Text)
	}
	if !msg.ForceFresh {
		t.Fatal("ForceFresh = false — the adapter consumed /clear without saying so, so the fresh " +
			"session the person asked for never happens")
	}
	// The directive must not survive as a command source either: Router would
	// rewrite Text to its empty body and drop the quote.
	if _, ok := engine.ParseFreshSessionCommand(msg.CommandText); ok {
		t.Fatalf("CommandText = %q still parses as /clear; Router would strip Text to the empty "+
			"command body and the quote would be lost", msg.CommandText)
	}
}

// TestABareDirectiveWithNoQuoteIsStillTheSentinel is the gate's other side: no
// quote, no media, nothing to say. Consuming the directive here would open a
// session with an empty turn — a worse bug than the one being fixed.
func TestABareDirectiveWithNoQuoteIsStillTheSentinel(t *testing.T) {
	t.Parallel()
	for _, directive := range []string{"/new", "/clear"} {
		t.Run(directive, func(t *testing.T) {
			t.Parallel()
			mc := aibotMsgCallback{MsgID: "m-bare", ChatID: "TUSER", ChatType: "single", MsgType: "text"}
			mc.From.UserID = "TUSER"
			mc.Text.Content = directive

			msg := channelMessageFromCallback("bot-1", "", mc, mc.Text.Content, "req-bare")

			if msg.Text != directive {
				t.Fatalf("Text = %q, want %q left intact for Router's sentinel path", msg.Text, directive)
			}
			if msg.ForceFresh {
				t.Fatal("ForceFresh set for a bare directive with nothing else; the adapter consumed " +
					"a directive it was supposed to pass through")
			}
		})
	}
}

// TestAQuotedDocumentDoesNotBecomeTheBody: the quote is a pointer, not a
// resend. Unbounded, a quoted document pushes the sender's own words — which
// follow it — out of anything reading the front of the body.
func TestAQuotedDocumentDoesNotBecomeTheBody(t *testing.T) {
	t.Parallel()
	mc := aibotMsgCallback{MsgID: "m-long", ChatID: "TUSER", ChatType: "single", MsgType: "text"}
	mc.From.UserID = "TUSER"
	mc.Text.Content = "这个怎么处理"
	mc.Quote.MsgType = "text"
	mc.Quote.Text.Content = strings.Repeat("很长的报告内容", 400) // 2800 runes

	msg := channelMessageFromCallback("bot-1", "", mc, mc.Text.Content, "req-long")

	if !strings.HasSuffix(msg.Text, "…\n\n这个怎么处理") {
		t.Fatalf("Text does not end with an elided quote followed by the sender's words; got tail %q",
			lastRunes(msg.Text, 24))
	}
	quoteLine := strings.SplitN(msg.Text, "\n\n", 2)[0]
	if got := len([]rune(quoteLine)); got > maxQuotedRunes+len([]rune("> [Quote] …")) {
		t.Fatalf("quoted block = %d runes, want it bounded near %d", got, maxQuotedRunes)
	}
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
