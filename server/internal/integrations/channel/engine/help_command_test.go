package engine

import (
	"context"
	"testing"
)

func TestIsHelpCommand(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{body: "/help", want: true},
		{body: "\n\t /help  \n", want: true},
		{body: "/help commands", want: true},
		{body: "/help\n/issue example", want: true},
		{body: "/helper"},
		{body: "/Help"},
		{body: "please /help"},
		{body: "quoted text\n/help"},
		{body: ""},
	} {
		t.Run(tc.body, func(t *testing.T) {
			if got := IsHelpCommand(tc.body); got != tc.want {
				t.Fatalf("IsHelpCommand(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestRouter_HelpRepliesWithoutChangingChatOrRunningAgent(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "group"}[group], func(t *testing.T) {
			h := newHarness(t)
			msg := p2pMessage(t)
			msg.CommandText = "/help"
			msg.Text = "[quoted context]\n/help\n[Image]"
			if group {
				msg.Source.ChatType = "group"
				msg.AddressedToBot = true
			}
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			h.router.Drain(context.Background())
			results := h.replier.calls()
			if len(results) != 1 || results[0].Outcome != OutcomeHelp {
				t.Fatalf("results = %+v, want one help reply", results)
			}
			if results[0].ChatSessionID.Valid || h.binder.ensureCalls != 0 || h.binder.startCalls != 0 || h.binder.appendedParams().SessionID.Valid || h.binder.pendingFreshCalls() != 0 {
				t.Fatal("help created, appended to, or reset a Chat")
			}
			if h.tasks.called || h.issues.called || h.media.calls() != 0 || h.typing.calls() != 0 {
				t.Fatal("help scheduled agent work, created an issue, downloaded media, or started typing")
			}
			if h.dedup.marks() != 1 || h.dedup.releases() != 0 {
				t.Fatal("help must finalize its dedup claim")
			}
		})
	}
}

func TestRouter_HelpRespectsChannelAccessChecks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*harness)
		want  Outcome
	}{
		{name: "disconnected", setup: func(h *harness) { h.inst.inst.Active = false }, want: OutcomeDropped},
		{name: "duplicate", setup: func(h *harness) { h.dedup.claimErr = ErrDuplicate }, want: OutcomeDropped},
		{name: "unbound", setup: func(h *harness) { h.ident.err = ErrSenderUnbound }, want: OutcomeNeedsBinding},
		{name: "nonmember", setup: func(h *harness) { h.ident.err = ErrSenderNotMember }, want: OutcomeDropped},
		{name: "invoke denied", setup: func(h *harness) { h.tasks.denyInvoke = true }, want: OutcomeInvokeDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.setup(h)
			msg := p2pMessage(t)
			msg.Text = "/help"
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			h.router.Drain(context.Background())
			results := h.replier.calls()
			if len(results) != 1 || results[0].Outcome != tc.want {
				t.Fatalf("results = %+v, want %s", results, tc.want)
			}
		})
	}
}

func TestRouter_NewAndClearDoNotReinterpretHelpBody(t *testing.T) {
	for _, body := range []string{"/new /help", "/clear /help"} {
		t.Run(body, func(t *testing.T) {
			h := newHarness(t)
			h.media.noMedia = true
			msg := p2pMessage(t)
			msg.Text = body
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			h.router.Drain(context.Background())
			if got := h.binder.appendedParams().Message.Text; got != "/help" {
				t.Fatalf("agent prompt = %q, want literal /help", got)
			}
			for _, result := range h.replier.calls() {
				if result.Outcome == OutcomeHelp {
					t.Fatal("session command body was reinterpreted as help")
				}
			}
		})
	}
}
