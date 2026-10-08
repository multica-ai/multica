package engine

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// The normalized inputs match the DingTalk adapter's selected-reply contract.
func TestRouter_SelectedQuoteSurvivesControl(t *testing.T) {
	for _, command := range []string{"/new", "/clear"} {
		for _, input := range []struct {
			name, body string
			media      bool
		}{
			{"markdown", "> /issue selected body", false},
			{"xml", "<quoted_message message_id=\"selected\" type=\"text\">/issue selected body</quoted_message>", false},
			{"unavailable", "> [quoted content unavailable]", false},
			{"image", "> [Image]", true},
		} {
			name := command + "/" + input.name
			media := input.media
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				defer h.router.Drain(context.Background())
				h.media.noMedia = !media
				msg := p2pMessage(t)
				msg.Text = input.body
				if media {
					msg.Text = "> [Image]"
					msg.Type = channel.MsgTypeImage
				}
				msg.CommandText = command
				msg.ForceFresh = command == "/clear"
				msg.HasSelectedContext = true
				msg.ReplyTo = &channel.ReplyCtx{MessageID: "selected"}
				if err := h.router.Handle(context.Background(), msg); err != nil {
					t.Fatal(err)
				}
				if command == "/new" {
					if !h.binder.lastStart.PersistMessage {
						t.Error("selected quote not persisted in new chat")
					}
					if h.binder.lastStart.Message.Text != msg.Text {
						t.Error("quote body changed")
					}
				} else {
					if h.binder.pendingFreshCalls() != 0 {
						t.Error("selected quote incorrectly treated as empty fresh command")
					}
					if !h.binder.appendedParams().Message.ForceFresh {
						t.Error("selected quote lost fresh-session intent")
					}
					if h.binder.appendedParams().Message.Text != msg.Text {
						t.Error("selected quote never appended")
					}
				}
				if !waitFor(time.Second, h.tasks.wasCalled) {
					t.Error("selected quote never schedules an agent turn")
				}
			})
		}
	}
}

func TestRouter_BareControlIgnoresUnselectedAndEmptyContext(t *testing.T) {
	for _, command := range []string{"/new", "/clear"} {
		for _, selected := range []bool{false, true} {
			h := newHarness(t)
			h.media.noMedia = true
			msg := p2pMessage(t)
			msg.Text = "<recent_context>automatic history</recent_context>"
			if selected {
				msg.Text = "  "
			}
			msg.CommandText = command
			msg.ForceFresh = command == "/clear"
			msg.HasSelectedContext = selected
			msg.ReplyTo = &channel.ReplyCtx{MessageID: "thread"}
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			h.router.Drain(context.Background())
			if h.tasks.wasCalled() || h.binder.lastStart.PersistMessage || (command == "/clear" && h.binder.appendedParams().Message.MessageID != "") {
				t.Fatalf("bare control generated a turn: command=%q selected=%v", command, selected)
			}
		}
	}
}

func TestRouter_SelectedNewRetryEnqueuesOnce(t *testing.T) {
	h := newHarness(t)
	defer h.router.Drain(context.Background())
	h.media.noMedia = true
	h.binder.startErrs = []error{ErrRouteChanged, nil}
	msg := p2pMessage(t)
	msg.Text = "> selected body"
	msg.CommandText = "/new"
	msg.HasSelectedContext = true
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if h.binder.startCalls != 2 || !h.binder.lastStart.PersistMessage || h.tasks.calls() != 1 || len(h.lifecycle.started) != 1 {
		t.Fatalf("selected quote was dropped or duplicated: starts=%d tasks=%d", h.binder.startCalls, h.tasks.calls())
	}
}

func TestRouter_SelectedNewSkipRunStillPersists(t *testing.T) {
	h := newHarness(t)
	defer h.router.Drain(context.Background())
	h.media.noMedia = true
	msg := p2pMessage(t)
	msg.Text = "> selected body"
	msg.CommandText = "/new"
	msg.HasSelectedContext = true
	msg.SkipAgentRun = true
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if !h.binder.lastStart.PersistMessage || h.binder.lastStart.Message.Text != msg.Text || h.tasks.wasCalled() || h.tasks.wasPrepared() {
		t.Fatal("skip-run must retain selected input without starting a task")
	}
}
