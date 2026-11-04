package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// fakeSlackActions stands in for the server's ChannelActions. A press's data is
// "<decision>:<action>", so the decision key is the part before the colon. Each
// press is recorded, and the press answers with result, or err when set.
type fakeSlackActions struct {
	mu      sync.Mutex
	presses []channel.Press
	result  channel.PressResult
	err     error
}

func (f *fakeSlackActions) Press(_ context.Context, p channel.Press) (channel.PressResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presses = append(f.presses, p)
	return f.result, f.err
}

func (f *fakeSlackActions) ActionKey(data string) string {
	if i := strings.Index(data, ":"); i > 0 {
		return data[:i]
	}
	return ""
}

func (f *fakeSlackActions) Command(context.Context, channel.Command) (channel.Message, bool, error) {
	return channel.Message{}, false, nil
}

func (f *fakeSlackActions) pressed() []channel.Press {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]channel.Press(nil), f.presses...)
}

// pressCall is one request the adapter made: the path, and the body as sent.
type pressCall struct {
	path string
	body []byte
}

// pressServer answers the two calls a press makes: the response_url reply and
// chat.update. Its replies are the shapes Slack returns for each.
type pressServer struct {
	mu    sync.Mutex
	calls []pressCall
}

func newPressServer(t *testing.T) (*pressServer, *httptest.Server) {
	t.Helper()
	s := &pressServer{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		s.mu.Lock()
		s.calls = append(s.calls, pressCall{path: r.URL.Path, body: raw})
		s.mu.Unlock()
		if r.URL.Path == "/chat.update" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = io.WriteString(w, `{"ok":true,"channel":"D0BRIEF","ts":"1767225600.000100","text":"Brief ready"}`)
			return
		}
		// The response_url reply: Slack answers a webhook with the literal "ok".
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(ts.Close)
	return s, ts
}

func (s *pressServer) callsTo(path string) []pressCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []pressCall
	for _, c := range s.calls {
		if c.path == path {
			out = append(out, c)
		}
	}
	return out
}

// pressBlocks is a brief push's message as Slack stores it: the section, then
// one row for the first decision (Approve, Reject, and the Revise link that
// names it) and one row for the second.
func pressBlocks() []any {
	return []any{
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "Brief ready\n2 decisions need you:"}},
		map[string]any{"type": "actions", "block_id": "brief_actions_1", "elements": []any{
			map[string]any{"type": "button", "action_id": "brief_action_1", "text": map[string]any{"type": "plain_text", "text": "Approve: Hire"}, "value": "p1:approve"},
			map[string]any{"type": "button", "action_id": "brief_action_2", "text": map[string]any{"type": "plain_text", "text": "Reject: Hire"}, "value": "p1:reject"},
			map[string]any{"type": "button", "action_id": "brief_action_3", "text": map[string]any{"type": "plain_text", "text": "Revise: Hire"}, "url": "https://app.example/acme/company?decision=p1"},
		}},
		map[string]any{"type": "actions", "block_id": "brief_actions_2", "elements": []any{
			map[string]any{"type": "button", "action_id": "brief_action_4", "text": map[string]any{"type": "plain_text", "text": "Approve: Ship"}, "value": "p2:approve"},
			map[string]any{"type": "button", "action_id": "brief_action_5", "text": map[string]any{"type": "plain_text", "text": "Reject: Ship"}, "value": "p2:reject"},
		}},
	}
}

// pressPayload is a block_actions interaction as Slack sends it over Socket
// Mode for a press of value by user. The pressed action is the first button of
// the message's blocks that carries that value, or a bare link when value is "".
func pressPayload(t *testing.T, responseURL, user, value string) slack.InteractionCallback {
	t.Helper()
	action := map[string]any{"action_id": "brief_action_1", "block_id": "brief_actions_1", "type": "button", "action_ts": "1767225601.000200"}
	if value != "" {
		action["value"] = value
	}
	raw, err := json.Marshal(map[string]any{
		"type":         "block_actions",
		"api_app_id":   "A1",
		"token":        "verification-unused",
		"trigger_id":   "trigger-1",
		"response_url": responseURL,
		"user":         map[string]any{"id": user, "username": "ada", "team_id": "T1"},
		"team":         map[string]any{"id": "T1", "domain": "acme"},
		"channel":      map[string]any{"id": "D0BRIEF", "name": "directmessage"},
		"container":    map[string]any{"type": "message", "message_ts": "1767225600.000100", "channel_id": "D0BRIEF", "is_ephemeral": false},
		"message": map[string]any{
			"type": "message", "user": "UBOT", "text": "Brief ready\n2 decisions need you:",
			"ts": "1767225600.000100", "blocks": pressBlocks(),
		},
		"actions": []any{action},
	})
	if err != nil {
		t.Fatal(err)
	}
	var cb slack.InteractionCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		t.Fatalf("payload does not parse as an interaction: %v", err)
	}
	return cb
}

// pressChannel is a slackChannel wired to the fake actions and the recording
// server. botUser is the installation's bot identity.
func pressChannel(t *testing.T, actions channel.ActionHandler, ts *httptest.Server, botUser string) *slackChannel {
	t.Helper()
	return &slackChannel{
		appID:          "A1",
		botUserID:      botUser,
		installationID: uid(1),
		actions:        actions,
		logger:         slog.Default(),
		botAPI:         slack.New("xoxb-press-test", slack.OptionAPIURL(ts.URL+"/")),
	}
}

// webhookText reads the text and response_type of an ephemeral reply.
func webhookText(t *testing.T, call pressCall) (string, string) {
	t.Helper()
	var msg struct {
		Text         string `json:"text"`
		ResponseType string `json:"response_type"`
	}
	if err := json.Unmarshal(call.body, &msg); err != nil {
		t.Fatalf("reply is not JSON: %v", err)
	}
	return msg.Text, msg.ResponseType
}

func TestSlackPressSendsTheSendersIdentityAndRepliesToThePresser(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{Reply: "Approved: Hire.", Settled: false}}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:approve"))

	pressed := actions.pressed()
	if len(pressed) != 1 {
		t.Fatalf("presses = %d, want 1", len(pressed))
	}
	want := channel.Sender{ChannelType: TypeSlack, InstallationID: uid(1), ChannelUserID: "UMEMBER1"}
	if pressed[0].Sender != want || pressed[0].Data != "p1:approve" {
		t.Fatalf("press = %+v, want sender %+v and the button's token", pressed[0], want)
	}

	replies := server.callsTo("/response")
	if len(replies) != 1 {
		t.Fatalf("replies = %d, want 1", len(replies))
	}
	text, kind := webhookText(t, replies[0])
	if text != "Approved: Hire." || kind != "ephemeral" {
		t.Fatalf("reply = %q (%s), want the ephemeral approval", text, kind)
	}
}

// A decided proposal loses its buttons. The other decision's buttons, the
// section and the Revise link of the other decision stay.
func TestSlackPressSettledRemovesOnlyThatDecisionsButtons(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{Reply: "Rejected: Hire.", Settled: true}}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:reject"))

	updates := server.callsTo("/chat.update")
	if len(updates) != 1 {
		t.Fatalf("chat.update calls = %d, want 1", len(updates))
	}
	form, err := url.ParseQuery(string(updates[0].body))
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("channel") != "D0BRIEF" || form.Get("ts") != "1767225600.000100" {
		t.Fatalf("chat.update addressed %s %s, want the press's message", form.Get("channel"), form.Get("ts"))
	}
	if form.Get("text") != "Brief ready\n2 decisions need you:" {
		t.Fatalf("chat.update text = %q, want the message text kept", form.Get("text"))
	}

	var blocks []map[string]any
	if err := json.Unmarshal([]byte(form.Get("blocks")), &blocks); err != nil {
		t.Fatalf("blocks are not JSON: %v", err)
	}
	if len(blocks) != 2 || blocks[0]["type"] != "section" || blocks[1]["block_id"] != "brief_actions_2" {
		t.Fatalf("blocks after settling = %v, want the section and the second decision's row", blocks)
	}
	values := mustJSON(t, blocks[1])
	if !strings.Contains(values, "p2:approve") || !strings.Contains(values, "p2:reject") || strings.Contains(values, "p1:") {
		t.Fatalf("second row = %s, want only the second decision's buttons", values)
	}
	if strings.Contains(form.Get("blocks"), "decision=p1") {
		t.Fatalf("the first decision's Revise link survived: %s", form.Get("blocks"))
	}
}

// A press that changes nothing keeps the buttons on the message.
func TestSlackPressUnsettledKeepsTheButtons(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{Reply: "Open the app to decide.", Settled: false}}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:approve"))

	if n := len(server.callsTo("/chat.update")); n != 0 {
		t.Fatalf("chat.update calls = %d, want none for an unsettled press", n)
	}
	if n := len(server.callsTo("/response")); n != 1 {
		t.Fatalf("replies = %d, want the refusal reply", n)
	}
}

// A replay on a decided proposal answers "already decided" and clears the
// buttons of that decision, which the first press already did.
func TestSlackPressReplayOnDecidedProposalSaysAlreadyDecided(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{
		Toast:   "Already decided",
		Reply:   "Already decided. Nothing changed.",
		Settled: true,
	}}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:approve"))

	replies := server.callsTo("/response")
	if len(replies) != 1 {
		t.Fatalf("replies = %d, want 1", len(replies))
	}
	if text, _ := webhookText(t, replies[0]); text != "Already decided. Nothing changed." {
		t.Fatalf("reply = %q, want the already-decided text", text)
	}
	if n := len(server.callsTo("/chat.update")); n != 1 {
		t.Fatalf("chat.update calls = %d, want 1 to clear the stale buttons", n)
	}
}

// The installation's own bot is an agent's Slack identity. The press reaches the
// server marked as a bot, which refuses it.
func TestSlackPressFromTheInstallationsBotIsMarkedAsAnAgent(t *testing.T) {
	t.Parallel()
	_, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{Reply: "Agents cannot decide proposals."}}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UBOT", "p1:approve"))

	pressed := actions.pressed()
	if len(pressed) != 1 || !pressed[0].IsBot {
		t.Fatalf("presses = %+v, want one press marked as a bot", pressed)
	}
}

// A link button has no value. Slack still sends its click over the socket, and
// it is not a decision, so nothing is asked of the server.
func TestSlackPressOnALinkButtonIsNotAPress(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", ""))

	if n := len(actions.pressed()); n != 0 {
		t.Fatalf("presses = %d, want none for a link", n)
	}
	if n := len(server.calls); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

// When the server cannot answer, the presser is told nothing changed, and no
// buttons are removed.
func TestSlackPressFailureAnswersSafelyAndChangesNothing(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{err: fmt.Errorf("database unavailable")}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:approve"))

	replies := server.callsTo("/response")
	if len(replies) != 1 {
		t.Fatalf("replies = %d, want 1", len(replies))
	}
	if text, _ := webhookText(t, replies[0]); text != msgSlackPressFailed {
		t.Fatalf("reply = %q, want the failure text", text)
	}
	if n := len(server.callsTo("/chat.update")); n != 0 {
		t.Fatalf("chat.update calls = %d, want none on a failed press", n)
	}
}

// The text of a settled reply is escaped as mrkdwn, since it may carry a title.
func TestSlackPressReplyIsEscapedMrkdwn(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{Reply: "Approved: <Ship> & Co.", Settled: false}}
	c := pressChannel(t, actions, ts, "UBOT")

	c.press(context.Background(), pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:approve"))

	replies := server.callsTo("/response")
	if len(replies) != 1 {
		t.Fatalf("replies = %d, want 1", len(replies))
	}
	if text, _ := webhookText(t, replies[0]); text != "Approved: &lt;Ship&gt; &amp; Co." {
		t.Fatalf("reply = %q, want the title escaped", text)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The Socket Mode loop routes an interactive envelope to the press path. The
// press runs on its own goroutine, so the test waits for it to land.
func TestSlackSocketInteractiveEnvelopeReachesThePress(t *testing.T) {
	t.Parallel()
	server, ts := newPressServer(t)
	actions := &fakeSlackActions{result: channel.PressResult{Reply: "Approved: Hire.", Settled: false}}
	c := pressChannel(t, actions, ts, "UBOT")
	sm := socketmode.New(slack.New("", slack.OptionAppLevelToken("xapp-test")))

	evt := socketmode.Event{
		Type: socketmode.EventTypeInteractive,
		Data: pressPayload(t, ts.URL+"/response", "UMEMBER1", "p1:approve"),
	}
	if err := c.handleSocketEvent(context.Background(), sm, evt, nil); err != nil {
		t.Fatalf("handleSocketEvent: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(actions.pressed()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(actions.pressed()); n != 1 {
		t.Fatalf("presses = %d, want the envelope to reach the press once", n)
	}
	deadline = time.Now().Add(5 * time.Second)
	for len(server.callsTo("/response")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(server.callsTo("/response")); n != 1 {
		t.Fatalf("replies = %d, want 1", n)
	}
}
