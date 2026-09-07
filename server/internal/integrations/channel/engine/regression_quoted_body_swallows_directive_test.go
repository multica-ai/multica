package engine

// regression_quoted_body_swallows_directive_test.go — a bare /new or /clear
// sent as a reply to another message must still be only a directive.
//
// Router strips a directive from the agent-readable body along one of two
// routes: it re-parses Text itself, or it falls back to Text == CommandText.
// An adapter that renders quoted context ahead of the sender's words breaks
// both — the first line is now the quote, and Text no longer equals the
// command source — so a bare directive behind a quote survives into the body.
//
// The two failures are not symmetric, which is why both are pinned here:
//
//   - /new: neither route fires, "/new" is persisted as the first turn of the
//     session it just opened, and every later turn inherits it as context.
//   - /clear: the FreshSession branch rewrites Text to the (empty) command
//     body unless the adapter already stripped it, so the quote — the only
//     thing the person actually sent — is dropped without a word.
//
// The wecom adapter is the one that renders quotes today
// (wecom/ws_frame.go quotedContext); the messages below are the exact shape it
// emits, pinned from the other side in
// wecom/regression_quoted_directive_test.go. Router is what has to be driven,
// because neither failure is visible in the adapter's own output.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// quotedDirectiveMessage is what an adapter hands Router for a bare directive
// sent as a reply: the quote is the whole visible body, and the directive
// survives only as the command source.
func quotedDirectiveMessage(t *testing.T, directive string, forceFresh bool) channel.InboundMessage {
	t.Helper()
	msg := p2pMessage(t)
	msg.Text = "> [Quote] Q3 毛利率 42.1%"
	msg.CommandText = directive
	msg.ForceFresh = forceFresh
	return msg
}

// TestBareNewBehindAQuoteDoesNotPersistTheDirective: what a person experiences
// when this regresses is an agent that answers every later turn as if they had
// opened with the word "/new".
func TestBareNewBehindAQuoteDoesNotPersistTheDirective(t *testing.T) {
	h := newHarness(t)
	h.media.noMedia = true

	if err := h.router.Handle(context.Background(), quotedDirectiveMessage(t, "/new", false)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if h.binder.startCalls != 1 {
		t.Fatalf("start calls=%d, want 1 — a bare /new opens a session", h.binder.startCalls)
	}
	if !h.binder.lastStart.PersistMessage {
		t.Fatal("the quote is the only thing the person sent; it has to be the session's first turn, " +
			"not discarded as if the message were empty")
	}
	got := h.binder.lastStart.Message.Text
	if strings.Contains(got, "/new") {
		t.Fatalf("first turn = %q — the directive was persisted as prompt text and every later turn "+
			"in this session inherits it", got)
	}
	if got != "> [Quote] Q3 毛利率 42.1%" {
		t.Fatalf("first turn = %q, want the quoted context intact", got)
	}
}

// TestBareClearBehindAQuoteKeepsTheQuote: the other direction. /clear must
// still force a fresh session, but the quote is the turn — dropping it leaves
// the person watching the bot answer a question it was never given.
func TestBareClearBehindAQuoteKeepsTheQuote(t *testing.T) {
	h := newHarness(t)
	h.media.noMedia = true

	// ForceFresh is set by the adapter: it consumed the directive itself
	// because the message carried content besides it.
	if err := h.router.Handle(context.Background(), quotedDirectiveMessage(t, "", true)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !waitFor(time.Second, h.tasks.wasCalled) || !h.tasks.freshArg() {
		t.Fatal("/clear behind a quote must still enqueue a fresh provider session")
	}
	got := h.binder.lastAppend.Message.Text
	if got != "> [Quote] Q3 毛利率 42.1%" {
		t.Fatalf("appended text = %q, want the quoted context intact — an empty body here is the quote "+
			"being dropped silently", got)
	}
}

// TestABareDirectiveWithNothingElseIsStillTheSentinel guards the other side of
// the same gate: the fix must not make every bare directive look like a turn.
// With no quote and no media there is nothing to say, and Router's pending
// sentinel is what keeps the session from opening with an empty message.
func TestABareDirectiveWithNothingElseIsStillTheSentinel(t *testing.T) {
	h := newHarness(t)
	h.media.noMedia = true
	msg := p2pMessage(t)
	msg.Text = "/new"

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if h.binder.startCalls != 1 || h.binder.lastStart.PersistMessage {
		t.Fatalf("start calls=%d persist=%v — a bare /new with nothing else must open the session "+
			"without persisting an empty first turn", h.binder.startCalls, h.binder.lastStart.PersistMessage)
	}
}
