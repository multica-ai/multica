package slack

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func blockActionsCallback() slack.InteractionCallback {
	return slack.InteractionCallback{
		Type:        slack.InteractionTypeBlockActions,
		ResponseURL: "https://hooks.slack.test/response/abc",
		User:        slack.User{ID: "U123"},
		Channel: slack.Channel{
			GroupConversation: slack.GroupConversation{
				Conversation: slack.Conversation{ID: "C123"},
			},
		},
		Container: slack.Container{MessageTs: "1700000000.000100"},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{
				{ActionID: "pawf_ping", Value: "clicked"},
			},
		},
	}
}

func waitForForward(t *testing.T, ch <-chan interactionForward) interactionForward {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for interaction forward POST")
		return interactionForward{}
	}
}

func newInteractionTestServer(t *testing.T, ch chan<- interactionForward) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		var fwd interactionForward
		if err := json.NewDecoder(r.Body).Decode(&fwd); err != nil {
			t.Errorf("decode forwarded payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		ch <- fwd
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDispatchInteraction_ForwardsBlockActions covers the happy path: a
// block_actions callback's response_url / action / value / user / channel /
// message_ts are preserved and reach interactURL as JSON.
func TestDispatchInteraction_ForwardsBlockActions(t *testing.T) {
	ch := make(chan interactionForward, 1)
	srv := newInteractionTestServer(t, ch)

	c := &slackChannel{appID: "A1", interactURL: srv.URL, logger: slog.Default()}
	c.dispatchInteraction(blockActionsCallback())

	got := waitForForward(t, ch)
	want := interactionForward{
		ResponseURL: "https://hooks.slack.test/response/abc",
		ActionID:    "pawf_ping",
		Value:       "clicked",
		UserID:      "U123",
		ChannelID:   "C123",
		MessageTs:   "1700000000.000100",
	}
	if got != want {
		t.Errorf("forwarded payload = %+v, want %+v", got, want)
	}
}

// TestDispatchInteraction_NoWebhookConfigured guards the additive-feature
// contract: an unconfigured interactURL (the default, and every upstream
// deployment that never sets MULTICA_SLACK_INTERACTION_WEBHOOK_URL) must
// silently drop the callback, never panic or block.
func TestDispatchInteraction_NoWebhookConfigured(t *testing.T) {
	c := &slackChannel{appID: "A1", logger: slog.Default()}
	c.dispatchInteraction(blockActionsCallback()) // must not panic
}

// TestDispatchInteraction_IgnoresNonBlockActions guards the stated scope cut:
// view_submission (modals) and other interaction types are out of scope and
// must not be forwarded.
func TestDispatchInteraction_IgnoresNonBlockActions(t *testing.T) {
	ch := make(chan interactionForward, 1)
	srv := newInteractionTestServer(t, ch)

	c := &slackChannel{appID: "A1", interactURL: srv.URL, logger: slog.Default()}
	cb := blockActionsCallback()
	cb.Type = slack.InteractionTypeViewSubmission
	c.dispatchInteraction(cb)

	select {
	case got := <-ch:
		t.Fatalf("view_submission must not be forwarded, got %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDispatchInteraction_IgnoresEmptyBlockActions guards against a
// block_actions callback with no action entries (unexpected, but Slack's
// JSON shape does not forbid it) producing a zero-value forward.
func TestDispatchInteraction_IgnoresEmptyBlockActions(t *testing.T) {
	ch := make(chan interactionForward, 1)
	srv := newInteractionTestServer(t, ch)

	c := &slackChannel{appID: "A1", interactURL: srv.URL, logger: slog.Default()}
	cb := blockActionsCallback()
	cb.ActionCallback.BlockActions = nil
	c.dispatchInteraction(cb)

	select {
	case got := <-ch:
		t.Fatalf("empty block actions must not be forwarded, got %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestHandleSocketEvent_Interactive_AcksAndDispatches is the regression guard
// for the actual bug (MUL parent PPBM-2443 / PPBM-2445): before this patch,
// socketmode.EventTypeInteractive fell into handleSocketEvent's default
// branch, which acks and drops the envelope with no further processing. This
// asserts the new branch both acks (so Slack does not show a timeout to the
// user) and forwards the block_actions click.
func TestHandleSocketEvent_Interactive_AcksAndDispatches(t *testing.T) {
	ch := make(chan interactionForward, 1)
	srv := newInteractionTestServer(t, ch)

	sm := socketmode.New(slack.New("xoxb-test"))
	c := &slackChannel{appID: "A1", interactURL: srv.URL, logger: slog.Default()}

	req := &socketmode.Request{EnvelopeID: "env-1"}
	evt := socketmode.Event{
		Type:    socketmode.EventTypeInteractive,
		Data:    blockActionsCallback(),
		Request: req,
	}

	// handleSocketEvent acks (via sm.Ack, queued on an internal buffered
	// channel this package has no access to) before dispatching, then returns
	// nil immediately — the detached goroutine's POST runs after. A non-nil
	// error here would mean the ack itself failed in a way the connection
	// treats as fatal (socketmode.Client.Ack only errors on an oversized
	// payload, which an empty ack body never hits).
	if err := c.handleSocketEvent(t.Context(), sm, evt, regexp.MustCompile(`<@U000>`)); err != nil {
		t.Fatalf("handleSocketEvent: %v", err)
	}

	waitForForward(t, ch)
}
