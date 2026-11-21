package lark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

type fakeQuoteHistory struct {
	shown bool
	err   error
	calls []string // "<routeKey> <marker>"
}

func (f *fakeQuoteHistory) QuoteInCurrentContext(_ context.Context, _ pgtype.UUID, routeKey, marker string) (bool, error) {
	f.calls = append(f.calls, routeKey+" "+marker)
	return f.shown, f.err
}

func topicReply(body string) InboundMessage {
	return InboundMessage{
		InstallationID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		MessageType:    "text",
		MessageID:      "om_reply",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		ThreadID:       "omt_1",
		AddressedToBot: true,
		Body:           body,
		CommandBody:    body,
		ParentID:       "om_root",
		RootID:         "om_root",
	}
}

func quoteFake() *enricherFakeClient {
	fake := newEnricherFake()
	fake.byID["om_root"] = []LarkMessage{textMsg("om_root", "ou_alice", "the forwarded report", "1000")}
	return fake
}

const fullRootQuote = `<quoted_message message_id="om_root" sender="User 1" type="text">
the forwarded report
</quoted_message>`

// TestEnrichQuoteShownEarlierIsNotRepeated covers a reply inside a topic
// whose first message the conversation context already holds: the quote is
// neither fetched nor inlined again.
func TestEnrichQuoteShownEarlierIsNotRepeated(t *testing.T) {
	t.Parallel()
	fake := quoteFake()
	history := &fakeQuoteHistory{shown: true}

	out := enrich(t, fake, topicReply("and now?"), InboundEnricherConfig{Quotes: history})

	want := `<quoted_message message_id="om_root" type="shown_earlier">[Shown earlier in this conversation; not repeated.]</quoted_message>

and now?`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.calls) != 0 {
		t.Errorf("a quote shown earlier must not be fetched, got GetMessage%v", fake.calls)
	}
	if !out.HasSelectedContext {
		t.Error("the quote reference is still selected context")
	}
	wantCall := `oc_g:omt_1 <quoted_message message_id="om_root" sender=`
	if len(history.calls) != 1 || history.calls[0] != wantCall {
		t.Errorf("history lookup = %v, want [%s]", history.calls, wantCall)
	}
}

// TestEnrichQuoteInlinedUnlessShownEarlier pins every case that must still
// inline the quote in full: not shown yet, a failed lookup, no history
// wired, and /clear or /new, which start a context that has seen nothing.
func TestEnrichQuoteInlinedUnlessShownEarlier(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		body      string
		history   *fakeQuoteHistory
		wantBody  string
		wantCalls int
	}{
		{name: "not shown yet", body: "first", history: &fakeQuoteHistory{}, wantBody: "first", wantCalls: 1},
		{name: "lookup error", body: "first", history: &fakeQuoteHistory{shown: true, err: errors.New("db down")}, wantBody: "first", wantCalls: 1},
		{name: "no history", body: "first", wantBody: "first"},
		{name: "clear", body: "/clear start over", history: &fakeQuoteHistory{shown: true}, wantBody: "start over"},
		{name: "new chat", body: "/new start over", history: &fakeQuoteHistory{shown: true}, wantBody: "start over"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := InboundEnricherConfig{}
			if tc.history != nil {
				cfg.Quotes = tc.history
			}
			out := enrich(t, quoteFake(), topicReply(tc.body), cfg)
			if want := fullRootQuote + "\n\n" + tc.wantBody; out.Body != want {
				t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
			}
			if tc.history != nil && len(tc.history.calls) != tc.wantCalls {
				t.Errorf("history lookups = %d, want %d", len(tc.history.calls), tc.wantCalls)
			}
		})
	}
}

// TestQuotedFullMarkerMatchesOnlyFullRender guards the lookup marker: only a
// fully rendered quote counts as shown, never an unavailable placeholder or
// an earlier shown-earlier reference.
func TestQuotedFullMarkerMatchesOnlyFullRender(t *testing.T) {
	t.Parallel()
	marker := quotedFullMarker("om_root")
	if !strings.HasPrefix(wrapQuoted("om_root", "Alice", "text", "hi"), marker) {
		t.Error("a full quote must match the marker")
	}
	for _, block := range []string{quotedErrorBlock("om_root"), quotedShownBlock("om_root"), wrapQuoted("om_root2", "Alice", "text", "hi")} {
		if strings.Contains(block, marker) {
			t.Errorf("block must not match the marker: %s", block)
		}
	}
}
