package engine

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestParseUnbindCommand(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"/unbind", true},
		{"  /unbind  ", true},
		{"\n\n/unbind", true},
		{"/unbind\nfollowing text", true}, // first non-empty line decides
		{"/unbind now", false},            // no arguments allowed
		{"/Unbind", false},                // case-sensitive
		{"/unbindx", false},
		{"please /unbind me", false},
		{"hello\n/unbind", false}, // must be the first non-empty line
		{"", false},
		{"   \n  ", false},
	}
	for _, c := range cases {
		if got := ParseUnbindCommand(c.body); got != c.want {
			t.Errorf("ParseUnbindCommand(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}

func TestRouter_UnbindCommand_RemovesBinding(t *testing.T) {
	h := newHarness(t)
	// Identity resolution would fail — proving the unbind branch runs
	// BEFORE identity (an auto-binder must never fire on /unbind).
	h.ident.err = ErrSenderUnbound

	msg := p2pMessage(t)
	msg.Text = "/unbind"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.unbinder.calls() != 1 {
		t.Fatalf("expected 1 unbind call, got %d", h.unbinder.calls())
	}
	if h.dedup.marks() != 1 {
		t.Fatalf("unbind must finalize Mark, got %d", h.dedup.marks())
	}
	if h.tasks.wasCalled() {
		t.Fatalf("/unbind must not trigger a run")
	}
	if h.binder.lastAppend.SessionID.Valid {
		t.Fatalf("/unbind must not append to the session")
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeUnbound && r.UnbindExisted && r.Sender == "ou_user_a" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected an Unbound reply with UnbindExisted=true")
	}
	// The identity error must NOT have produced a binding prompt.
	for _, r := range h.replier.calls() {
		if r.Outcome == OutcomeNeedsBinding {
			t.Fatalf("/unbind must not trigger the binding prompt")
		}
	}
}

func TestRouter_UnbindCommand_NotBound(t *testing.T) {
	h := newHarness(t)
	h.unbinder.existed = false

	msg := p2pMessage(t)
	msg.Text = "/unbind"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeUnbound && !r.UnbindExisted {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected an Unbound reply with UnbindExisted=false")
	}
}

func TestRouter_NoUnbinder_UnbindTextIsPlainMessage(t *testing.T) {
	h := newHarness(t)
	// Re-register the set without the Unbind seam: the command is disabled
	// and "/unbind" flows through the normal chat pipeline.
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		OriginType:   "lark_chat",
	})

	msg := p2pMessage(t)
	msg.Text = "/unbind"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.unbinder.calls() != 0 {
		t.Fatalf("unbinder must not be consulted when the seam is absent")
	}
	if !h.tasks.wasCalled() {
		t.Fatalf("without the seam, /unbind is a plain message and must trigger a run")
	}
}
