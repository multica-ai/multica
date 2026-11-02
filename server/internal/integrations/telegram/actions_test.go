package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// The Bot API responses below are the shapes Telegram returns for the calls the
// actions make. None of these tests reach the real Telegram host.
const (
	actionsOKBody        = `{"ok":true,"result":true}`
	actionsSentBody      = `{"ok":true,"result":{"message_id":900,"chat":{"id":4242,"type":"private"},"date":1767225600,"text":"sent"}}`
	actionsNotModified   = `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`
	actionsPressCallback = `{"update_id":10,"callback_query":{"id":"cbq-1","from":{"id":4242,"is_bot":false,"first_name":"Ada"},"data":"TOKEN_A","message":{"message_id":501,"from":{"id":111,"is_bot":true,"first_name":"Secretary"},"chat":{"id":4242,"type":"private"},"date":1767225600,"text":"Brief ready","reply_markup":{"inline_keyboard":[[{"text":"Approve: A","callback_data":"TOKEN_A"}],[{"text":"Reject: A","callback_data":"TOKEN_A2"}],[{"text":"Approve: B","callback_data":"TOKEN_B"}],[{"text":"Open the brief","url":"https://app.example/acme/office?brief=b1"}],[{"text":"Revise: A","url":"https://app.example/acme/office?decision=pA"}]]}}}}`
)

// fakeActions is a channel.ActionHandler the tests script. Its keys map a
// button's data to the decision it acts on, as the server's signed tokens do.
type fakeActions struct {
	mu         sync.Mutex
	press      channel.PressResult
	pressErr   error
	presses    []channel.Press
	reply      channel.Message
	handled    bool
	commandErr error
	commands   []channel.Command
}

func (f *fakeActions) Press(_ context.Context, p channel.Press) (channel.PressResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presses = append(f.presses, p)
	return f.press, f.pressErr
}

func (f *fakeActions) ActionKey(data string) string {
	switch data {
	case "TOKEN_A", "TOKEN_A2":
		return "pA"
	case "TOKEN_B":
		return "pB"
	}
	return ""
}

func (f *fakeActions) Command(_ context.Context, c channel.Command) (channel.Message, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, c)
	return f.reply, f.handled, f.commandErr
}

// actionsServer records every Bot API call the channel makes and answers each
// with the recorded body for its method.
type actionsServer struct {
	t        *testing.T
	mu       sync.Mutex
	calls    map[string][]map[string]any
	editBody string
}

func (s *actionsServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				s.t.Errorf("%s: body is not JSON: %v", method, err)
			}
		}
		s.mu.Lock()
		s.calls[method] = append(s.calls[method], body)
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "sendMessage":
			_, _ = io.WriteString(w, actionsSentBody)
		case "editMessageReplyMarkup":
			if s.editBody != "" {
				_, _ = io.WriteString(w, s.editBody)
				return
			}
			_, _ = io.WriteString(w, actionsOKBody)
		default:
			_, _ = io.WriteString(w, actionsOKBody)
		}
	}
}

// actionsChannel is a Telegram channel wired to a recording Bot API and a
// scripted action handler. engineCalls counts the messages that reach the agent
// path.
type actionsChannel struct {
	ch          *telegramChannel
	server      *actionsServer
	actions     *fakeActions
	engineCalls *int
	mu          *sync.Mutex
}

func newActionsChannel(t *testing.T, actions *fakeActions) actionsChannel {
	t.Helper()
	srv := &actionsServer{t: t, calls: map[string][]map[string]any{}}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	var calls int
	var mu sync.Mutex
	c := &telegramChannel{
		botID:          111,
		botUsername:    "secretary_bot",
		api:            newBotAPI(ts.URL, notifierBotToken, ts.Client()),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		installationID: telegramTestUUID(1),
		handler: func(context.Context, channel.InboundMessage) error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return nil
		},
	}
	if actions != nil {
		c.actions = actions
	}
	return actionsChannel{ch: c, server: srv, actions: actions, engineCalls: &calls, mu: &mu}
}

func (a actionsChannel) engineReached() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *a.engineCalls
}

// sent returns the bodies of one method, in call order.
func (a actionsChannel) sent(method string) []map[string]any {
	a.server.mu.Lock()
	defer a.server.mu.Unlock()
	return a.server.calls[method]
}

func dispatchRaw(t *testing.T, c *telegramChannel, raw string) {
	t.Helper()
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("fixture is not an update: %v", err)
	}
	if err := c.dispatch(context.Background(), u); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

func TestPressSettledDecisionAnswersAndRemovesOnlyItsButtons(t *testing.T) {
	actions := &fakeActions{press: channel.PressResult{Toast: "Approved", Reply: "Approved: Hire a designer.", Settled: true}}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, actionsPressCallback)

	if len(actions.presses) != 1 {
		t.Fatalf("presses = %d, want 1", len(actions.presses))
	}
	p := actions.presses[0]
	if p.ChannelUserID != "4242" || p.InstallationID != telegramTestUUID(1) || p.Data != "TOKEN_A" || p.IsBot {
		t.Fatalf("press = %+v", p)
	}

	answers := a.sent("answerCallbackQuery")
	if len(answers) != 1 || answers[0]["callback_query_id"] != "cbq-1" || answers[0]["text"] != "Approved" {
		t.Fatalf("answerCallbackQuery = %v", answers)
	}

	replies := a.sent("sendMessage")
	if len(replies) != 1 || replies[0]["text"] != "Approved: Hire a designer." {
		t.Fatalf("reply = %v", replies)
	}
	if params, _ := replies[0]["reply_parameters"].(map[string]any); params["message_id"] != float64(501) {
		t.Fatalf("reply does not quote the pressed message: %v", replies[0])
	}

	edits := a.sent("editMessageReplyMarkup")
	if len(edits) != 1 {
		t.Fatalf("editMessageReplyMarkup calls = %d, want 1", len(edits))
	}
	rows, _ := edits[0]["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	if len(rows) != 2 {
		t.Fatalf("keyboard rows after removal = %d, want 2 (decision B and the brief link): %v", len(rows), rows)
	}
	gotB := rows[0].([]any)[0].(map[string]any)
	if gotB["callback_data"] != "TOKEN_B" {
		t.Fatalf("decision B's button was removed: %v", gotB)
	}
	gotBrief := rows[1].([]any)[0].(map[string]any)
	if gotBrief["url"] != "https://app.example/acme/office?brief=b1" {
		t.Fatalf("the brief link was removed: %v", gotBrief)
	}
	if engine := a.engineReached(); engine != 0 {
		t.Fatalf("a button press reached the agent path %d times", engine)
	}
}

func TestPressRefusalIsAnsweredButChangesNoKeyboard(t *testing.T) {
	actions := &fakeActions{press: channel.PressResult{Toast: "Only an owner or admin can decide", Reply: "Open the app: https://app.example"}}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, actionsPressCallback)

	if answers := a.sent("answerCallbackQuery"); len(answers) != 1 || answers[0]["text"] != "Only an owner or admin can decide" {
		t.Fatalf("answerCallbackQuery = %v", answers)
	}
	if replies := a.sent("sendMessage"); len(replies) != 1 || !strings.Contains(replies[0]["text"].(string), "https://app.example") {
		t.Fatalf("refusal reply with the app link = %v", replies)
	}
	if edits := a.sent("editMessageReplyMarkup"); len(edits) != 0 {
		t.Fatalf("a refused press edited the keyboard: %v", edits)
	}
}

func TestPressInfrastructureFailureStillAnswersThePresser(t *testing.T) {
	actions := &fakeActions{pressErr: errors.New("database is down")}
	a := newActionsChannel(t, actions)

	if err := a.ch.dispatch(context.Background(), mustUpdate(t, actionsPressCallback)); err != nil {
		t.Fatalf("an infrastructure failure must not stop the polling loop: %v", err)
	}
	if answers := a.sent("answerCallbackQuery"); len(answers) != 1 || answers[0]["text"] != msgPressFailedToast {
		t.Fatalf("answerCallbackQuery = %v", answers)
	}
	if edits := a.sent("editMessageReplyMarkup"); len(edits) != 0 {
		t.Fatalf("a failed press edited the keyboard: %v", edits)
	}
}

func TestPressFromABotIsPassedThroughAsAnAgent(t *testing.T) {
	actions := &fakeActions{press: channel.PressResult{Toast: "Agents cannot decide"}}
	a := newActionsChannel(t, actions)
	bot := strings.Replace(actionsPressCallback, `"from":{"id":4242,"is_bot":false`, `"from":{"id":4242,"is_bot":true`, 1)

	dispatchRaw(t, a.ch, bot)

	if len(actions.presses) != 1 || !actions.presses[0].IsBot {
		t.Fatalf("presses = %+v, want one bot press", actions.presses)
	}
}

func TestCommandBriefRepliesInTheChatAndSkipsTheAgent(t *testing.T) {
	actions := &fakeActions{
		handled: true,
		reply:   channel.Message{Text: "Hire a designer, approve the pilot.\n\nBrief: https://app.example/acme/office?brief=b1"},
	}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, `{"update_id":20,"message":{"message_id":77,"from":{"id":4242,"is_bot":false,"first_name":"Ada"},"chat":{"id":4242,"type":"private"},"date":1767225600,"text":"/brief"}}`)

	if len(actions.commands) != 1 || actions.commands[0].Name != channel.CommandBrief || actions.commands[0].ChannelUserID != "4242" {
		t.Fatalf("commands = %+v", actions.commands)
	}
	replies := a.sent("sendMessage")
	if len(replies) != 1 || !strings.Contains(replies[0]["text"].(string), "Hire a designer") {
		t.Fatalf("reply = %v", replies)
	}
	if params, _ := replies[0]["reply_parameters"].(map[string]any); params["message_id"] != float64(77) {
		t.Fatalf("reply does not quote the command: %v", replies[0])
	}
	if engine := a.engineReached(); engine != 0 {
		t.Fatalf("a handled command reached the agent path %d times", engine)
	}
}

func TestCommandDecisionsWithButtonsSendsTheKeyboard(t *testing.T) {
	actions := &fakeActions{
		handled: true,
		reply: channel.Message{
			Text:    "1 decision needs you:\n- Hire a designer https://app.example/acme/office?decision=pA",
			Buttons: []channel.Button{{Label: "Open the brief", URL: "https://app.example/acme/office?brief=b1"}},
		},
	}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, `{"update_id":21,"message":{"message_id":78,"from":{"id":4242,"is_bot":false,"first_name":"Ada"},"chat":{"id":4242,"type":"private"},"date":1767225600,"text":"/decisions"}}`)

	replies := a.sent("sendMessage")
	if len(replies) != 1 {
		t.Fatalf("replies = %d", len(replies))
	}
	if _, ok := replies[0]["reply_markup"]; !ok {
		t.Fatalf("reply carries no keyboard: %v", replies[0])
	}
	if actions.commands[0].Name != channel.CommandDecisions {
		t.Fatalf("command = %q", actions.commands[0].Name)
	}
}

// An unlinked sender is not answered by the command path; the message takes the
// ordinary path, which is the binding prompt.
func TestCommandFromAnUnlinkedSenderTakesTheOrdinaryPath(t *testing.T) {
	actions := &fakeActions{handled: false}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, `{"update_id":22,"message":{"message_id":79,"from":{"id":5555,"is_bot":false,"first_name":"Eve"},"chat":{"id":5555,"type":"private"},"date":1767225600,"text":"/brief"}}`)

	if engine := a.engineReached(); engine != 1 {
		t.Fatalf("the unlinked command reached the agent path %d times, want 1", engine)
	}
	if replies := a.sent("sendMessage"); len(replies) != 0 {
		t.Fatalf("the adapter answered an unlinked sender: %v", replies)
	}
}

// In a group, /brief is a command only when the bot is addressed. Without the
// mention it is group chatter: no command runs and no reply is sent.
func TestCommandInAGroupNeedsTheBotToBeAddressed(t *testing.T) {
	actions := &fakeActions{handled: true, reply: channel.Message{Text: "should not be sent"}}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, `{"update_id":23,"message":{"message_id":80,"from":{"id":4242,"is_bot":false,"first_name":"Ada"},"chat":{"id":-100,"type":"supergroup"},"date":1767225600,"text":"/brief"}}`)

	if len(actions.commands) != 0 {
		t.Fatalf("an unaddressed group /brief ran a command: %+v", actions.commands)
	}
	if replies := a.sent("sendMessage"); len(replies) != 0 {
		t.Fatalf("an unaddressed group /brief was answered: %v", replies)
	}
}

// A command with more on its line is a message for the agent, not /brief.
func TestCommandMustBeTheWholeLine(t *testing.T) {
	actions := &fakeActions{handled: true, reply: channel.Message{Text: "nope"}}
	a := newActionsChannel(t, actions)

	dispatchRaw(t, a.ch, `{"update_id":24,"message":{"message_id":81,"from":{"id":4242,"is_bot":false,"first_name":"Ada"},"chat":{"id":4242,"type":"private"},"date":1767225600,"text":"/brief me on the pilot"}}`)

	if len(actions.commands) != 0 {
		t.Fatalf("a sentence starting with /brief ran the command: %+v", actions.commands)
	}
	if engine := a.engineReached(); engine != 1 {
		t.Fatalf("the sentence reached the agent path %d times, want 1", engine)
	}
}

// The keyboard removal is best effort: Telegram's "not modified" means the keyboard
// already has that shape, which is not a failure.
func TestEditMessageNotModifiedIsNotAFailure(t *testing.T) {
	srv := &actionsServer{t: t, calls: map[string][]map[string]any{}, editBody: actionsNotModified}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	api := newBotAPI(ts.URL, notifierBotToken, ts.Client())

	if err := api.EditMessageReplyMarkup(context.Background(), 4242, 501, inlineKeyboard{InlineKeyboard: [][]inlineKeyboardButton{}}); err != nil {
		t.Fatalf("not modified surfaced as an error: %v", err)
	}
}

func mustUpdate(t *testing.T, raw string) Update {
	t.Helper()
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	return u
}
