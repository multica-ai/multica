package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
)

// The frames below are the shapes the aibot socket carries, as WeCom documents
// them. Nothing here reaches the real endpoint: the connection is a double.

func notifierInstallationID() pgtype.UUID {
	id, _ := util.ParseUUID("00000000-0000-4000-8000-0000000000a1")
	return id
}

// notifierRig is a live connection the registry holds for the installation.
type notifierRig struct {
	conn    *recordingConn
	senders *sendersRegistry
	inst    pgtype.UUID
}

func newNotifierRig(t *testing.T, conn *recordingConn) notifierRig {
	t.Helper()
	inst := notifierInstallationID()
	reg := newSendersRegistry()
	reg.set(inst, conn.autoAck(newWSSender(conn, nil)))
	return notifierRig{conn: conn, senders: reg, inst: inst}
}

func notifierBinding(inst pgtype.UUID) channel.Binding {
	return channel.Binding{ChannelType: TypeWecom, InstallationID: inst, ChannelUserID: "zhangsan"}
}

// decisionMessage is what renderBriefPush builds for one pending decision: the
// text, then the signed callback buttons and the links.
func decisionMessage() channel.Message {
	const link = "https://app.example/acme/company?decision=p1"
	return channel.Message{
		Text: "Brief ready\n\n1 decision needs you:\n- Hire a designer",
		Buttons: []channel.Button{
			{Label: "Open the brief", URL: "https://app.example/acme/company?brief=b1"},
			{Label: "Approve: Hire a designer", CallbackData: "token-approve", LinkURL: link},
			{Label: "Reject: Hire a designer", CallbackData: "token-reject", LinkURL: link, SameRow: true},
			{Label: "Snooze 7 days: Hire a designer", CallbackData: "token-snooze", LinkURL: link},
			{Label: "Revise: Hire a designer", URL: link, SameRow: true},
		},
	}
}

func TestNotifierSendsTheTextThenOneCardPerDecision(t *testing.T) {
	rig := newNotifierRig(t, &recordingConn{})
	n := NewNotifier(rig.senders)

	if err := n.Send(context.Background(), notifierBinding(rig.inst), decisionMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frames := rig.conn.sendFrames()
	if len(frames) != 2 {
		t.Fatalf("aibot_send_msg frames = %d, want the text and one card", len(frames))
	}

	text := rig.conn.sendBody(t, 0)
	if text["msgtype"] != "markdown" || text["chatid"] != "zhangsan" || text["chat_type"] != float64(chatTypeSingleInt) {
		t.Fatalf("text frame = %+v, want a markdown message to the single chat", text)
	}

	card := rig.conn.sendBody(t, 1)
	if card["msgtype"] != "template_card" || card["chatid"] != "zhangsan" || card["chat_type"] != float64(chatTypeSingleInt) {
		t.Fatalf("card frame = %+v", card)
	}
	tc := card["template_card"].(map[string]any)
	if tc["card_type"] != "button_interaction" {
		t.Fatalf("card_type = %v", tc["card_type"])
	}
	if title := tc["main_title"].(map[string]any)["title"]; title != "Hire a designer" {
		t.Fatalf("card title = %v, want the decision title", title)
	}
	if action := tc["card_action"].(map[string]any); action["url"] != "https://app.example/acme/company?decision=p1" {
		t.Fatalf("card_action = %+v, want the decision page", action)
	}
	if task, _ := tc["task_id"].(string); !strings.HasPrefix(task, wecomCardTaskPrefix) {
		t.Fatalf("task_id = %v", tc["task_id"])
	}

	// Each button carries its signed token as the key, and the text names the action.
	var keys, labels []string
	for _, raw := range tc["button_list"].([]any) {
		b := raw.(map[string]any)
		keys = append(keys, b["key"].(string))
		labels = append(labels, b["text"].(string))
	}
	if strings.Join(keys, ",") != "token-approve,token-reject,token-snooze" {
		t.Fatalf("button keys = %v, want the three signed tokens", keys)
	}
	if strings.Join(labels, "|") != "Approve|Reject|Snooze 7 days" {
		t.Fatalf("button texts = %v", labels)
	}
	// A link is not a WeCom button: the Open and Revise links stay in the text.
	if strings.Contains(strings.Join(keys, ","), "https://") {
		t.Fatalf("a link leaked into a callback key")
	}
}

func TestNotifierMessageWithoutDecisionsIsOnlyTheText(t *testing.T) {
	rig := newNotifierRig(t, &recordingConn{})
	n := NewNotifier(rig.senders)
	msg := channel.Message{Text: "Brief ready", Buttons: []channel.Button{{Label: "Open the brief", URL: "https://app.example/x"}}}

	if err := n.Send(context.Background(), notifierBinding(rig.inst), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := len(rig.conn.sendFrames()); got != 1 {
		t.Fatalf("frames = %d, want only the text", got)
	}
}

func TestNotifierRefusesBeforeAnyFrame(t *testing.T) {
	cases := []struct {
		name    string
		binding channel.Binding
		msg     channel.Message
	}{
		{"another channel's binding", channel.Binding{ChannelType: "slack", ChannelUserID: "zhangsan"}, channel.Message{Text: "x"}},
		{"no user id", channel.Binding{ChannelType: TypeWecom}, channel.Message{Text: "x"}},
		{"no text", channel.Binding{ChannelType: TypeWecom, ChannelUserID: "zhangsan"}, channel.Message{}},
		{"button with neither address", channel.Binding{ChannelType: TypeWecom, ChannelUserID: "zhangsan"}, channel.Message{Text: "x", Buttons: []channel.Button{{Label: "Bad"}}}},
		{"button with both addresses", channel.Binding{ChannelType: TypeWecom, ChannelUserID: "zhangsan"}, channel.Message{Text: "x", Buttons: []channel.Button{{Label: "Bad", URL: "https://a", CallbackData: "t"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newNotifierRig(t, &recordingConn{})
			tc.binding.InstallationID = rig.inst
			err := NewNotifier(rig.senders).Send(context.Background(), tc.binding, tc.msg)
			if !channel.IsPermanentDeliveryError(err) {
				t.Fatalf("Send error = %v, want a permanent failure", err)
			}
			if len(rig.conn.sendFrames()) != 0 {
				t.Fatalf("a refused brief reached the socket")
			}
		})
	}
}

// Without a live connection on this replica the push waits for a retry.
func TestNotifierWithoutALiveConnectionIsRetryable(t *testing.T) {
	n := NewNotifier(newSendersRegistry())
	err := n.Send(context.Background(), channel.Binding{ChannelType: TypeWecom, InstallationID: notifierInstallationID(), ChannelUserID: "zhangsan"}, channel.Message{Text: "x"})
	if err == nil || channel.IsPermanentDeliveryError(err) || !errors.Is(err, errNoLiveConnection) {
		t.Fatalf("Send error = %v, want a retryable no-connection failure", err)
	}
}

// A refused text is retryable: nothing reached the chat.
func TestNotifierRefusedTextIsRetryable(t *testing.T) {
	rig := newNotifierRig(t, &recordingConn{refuseCode: 45002, refuseMsg: "content exceed max length"})
	err := NewNotifier(rig.senders).Send(context.Background(), notifierBinding(rig.inst), decisionMessage())
	if err == nil || channel.IsPermanentDeliveryError(err) {
		t.Fatalf("Send error = %v, want a retryable failure", err)
	}
	if got := len(rig.conn.sendFrames()); got != 1 {
		t.Fatalf("frames = %d, want the text attempted once and no card", got)
	}
}

// Once the text is in the chat a failed card is permanent: a retry would send the
// text again.
func TestNotifierCardFailureAfterTheTextIsPermanent(t *testing.T) {
	rig := newNotifierRig(t, &recordingConn{refuseFromSend: 2})
	err := NewNotifier(rig.senders).Send(context.Background(), notifierBinding(rig.inst), decisionMessage())
	if !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("Send error = %v, want a permanent failure", err)
	}
	if !strings.Contains(err.Error(), "brief text was sent") {
		t.Fatalf("error = %v, want it to say the text went out", err)
	}
	if got := len(rig.conn.sendFrames()); got != 2 {
		t.Fatalf("frames = %d, want the text and the refused card", got)
	}
}

func TestBriefCardsGroupsByDecisionAndSkipsLinksAndLinklessCallbacks(t *testing.T) {
	msg := channel.Message{Buttons: []channel.Button{
		{Label: "Open the brief", URL: "https://a/brief"},
		{Label: "Approve: One", CallbackData: "t1", LinkURL: "https://a/1"},
		{Label: "Reject: One", CallbackData: "t2", LinkURL: "https://a/1"},
		{Label: "Approve: Two", CallbackData: "t3", LinkURL: "https://a/2"},
		{Label: "Approve: No link", CallbackData: "t4"},
	}}
	cards, err := briefCards(msg)
	if err != nil {
		t.Fatalf("briefCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want one per decision link", len(cards))
	}
	if cards[0].link != "https://a/1" || len(cards[0].buttons) != 2 || cards[0].buttons[1].key != "t2" {
		t.Fatalf("first card = %+v", cards[0])
	}
	if cards[1].link != "https://a/2" || len(cards[1].buttons) != 1 {
		t.Fatalf("second card = %+v", cards[1])
	}
}

func TestBriefCardsRefusesMoreButtonsThanACardHolds(t *testing.T) {
	var buttons []channel.Button
	for i := 0; i < wecomCardMaxButtons+1; i++ {
		buttons = append(buttons, channel.Button{Label: "Act: T", CallbackData: fmt.Sprintf("k%d", i), LinkURL: "https://a/1"})
	}
	if _, err := briefCards(channel.Message{Buttons: buttons}); err == nil {
		t.Fatalf("briefCards accepted %d buttons on one card", len(buttons))
	}
}

func TestDecisionTitleAndActionTextReadTheLabel(t *testing.T) {
	cases := []struct{ label, action, title string }{
		{"Approve: Hire a designer", "Approve", "Hire a designer"},
		{"Snooze 7 days: Hire a designer", "Snooze 7 days", "Hire a designer"},
		{"Approve", "Approve", wecomDecisionTitle},
	}
	for _, tc := range cases {
		if got := actionText(tc.label); got != tc.action {
			t.Errorf("actionText(%q) = %q, want %q", tc.label, got, tc.action)
		}
		if got := decisionTitle(tc.label); got != tc.title {
			t.Errorf("decisionTitle(%q) = %q, want %q", tc.label, got, tc.title)
		}
	}
	long := "Approve: " + strings.Repeat("x", 200)
	if got := len([]rune(decisionTitle(long))); got != wecomCardTitleRunes {
		t.Fatalf("title runes = %d, want %d", got, wecomCardTitleRunes)
	}
}

// fakePressHandler records the presses and answers with the configured result.
type fakePressHandler struct {
	mu      sync.Mutex
	result  channel.PressResult
	err     error
	presses []channel.Press
}

func (f *fakePressHandler) Press(_ context.Context, p channel.Press) (channel.PressResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presses = append(f.presses, p)
	return f.result, f.err
}

func (f *fakePressHandler) ActionKey(string) string { return "" }

func (f *fakePressHandler) Command(context.Context, channel.Command) (channel.Message, bool, error) {
	return channel.Message{}, false, nil
}

func (f *fakePressHandler) pressed() []channel.Press {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]channel.Press(nil), f.presses...)
}

func cardPressFrame(t *testing.T, chatType, chatID, userID, key string) frameEnvelope {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"chatid":   chatID,
		"chattype": chatType,
		"from":     map[string]string{"userid": userID},
		"msgtype":  "event",
		"event": map[string]string{
			"eventtype": eventTemplateCard,
			"event_key": key,
			"task_id":   "brief-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return frameEnvelope{Cmd: cmdEventCallback, Body: body}
}

// waitForSendFrames polls the double until it holds n aibot_send_msg frames.
func waitForSendFrames(t *testing.T, conn *recordingConn, n int) []frameEnvelope {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if frames := conn.sendFrames(); len(frames) >= n {
			return frames
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d reply frames; have %d", n, len(conn.sendFrames()))
	return nil
}

func TestCardPressReachesTheHandlerAndRepliesToTheChat(t *testing.T) {
	conn := &recordingConn{}
	rig := newNotifierRig(t, conn)
	handler := &fakePressHandler{result: channel.PressResult{Toast: "Approved", Reply: "Approved: Hire a designer.", Settled: true}}
	c := &wecomChannel{installationID: rig.inst, actions: handler}
	sender := rig.senders.get(rig.inst)

	if err := c.dispatchFrame(context.Background(), cardPressFrame(t, "single", "", "zhangsan", "token-approve"), sender, slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	frames := waitForSendFrames(t, conn, 1)
	var body map[string]any
	_ = json.Unmarshal(frames[0].Body, &body)
	if body["chatid"] != "zhangsan" || body["chat_type"] != float64(chatTypeSingleInt) {
		t.Fatalf("reply addressed to %+v, want the presser's chat", body)
	}
	if !strings.Contains(fmt.Sprint(body["markdown"]), "Approved: Hire a designer.") {
		t.Fatalf("reply = %+v, want the decision", body)
	}

	presses := handler.pressed()
	if len(presses) != 1 {
		t.Fatalf("presses = %d, want 1", len(presses))
	}
	got := presses[0]
	if got.ChannelType != TypeWecom || got.ChannelUserID != "zhangsan" || got.Data != "token-approve" || got.IsBot {
		t.Fatalf("press = %+v, want the wecom presser with the signed token", got)
	}
	if got.InstallationID != rig.inst {
		t.Fatalf("installation = %v, want the connection's", got.InstallationID)
	}
}

func TestCardPressInAGroupRepliesToTheGroup(t *testing.T) {
	conn := &recordingConn{}
	rig := newNotifierRig(t, conn)
	handler := &fakePressHandler{result: channel.PressResult{Reply: "Rejected: X."}}
	c := &wecomChannel{installationID: rig.inst, actions: handler}

	if err := c.dispatchFrame(context.Background(), cardPressFrame(t, "group", "room-7", "zhangsan", "token-reject"), rig.senders.get(rig.inst), slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	frames := waitForSendFrames(t, conn, 1)
	var body map[string]any
	_ = json.Unmarshal(frames[0].Body, &body)
	if body["chatid"] != "room-7" || body["chat_type"] != float64(chatTypeGroupInt) {
		t.Fatalf("group reply addressed to %+v, want the room", body)
	}
}

func TestCardPressFailureTellsThePresserNothingChanged(t *testing.T) {
	conn := &recordingConn{}
	rig := newNotifierRig(t, conn)
	handler := &fakePressHandler{err: errors.New("database down")}
	c := &wecomChannel{installationID: rig.inst, actions: handler}

	if err := c.dispatchFrame(context.Background(), cardPressFrame(t, "single", "", "zhangsan", "token-approve"), rig.senders.get(rig.inst), slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	frames := waitForSendFrames(t, conn, 1)
	var body map[string]any
	_ = json.Unmarshal(frames[0].Body, &body)
	if !strings.Contains(fmt.Sprint(body["markdown"]), "nothing changed") {
		t.Fatalf("reply = %+v, want the nothing-changed copy", body)
	}
}

// A press with no handler wired is left unanswered, and never reaches the socket.
func TestCardPressWithoutAHandlerIsIgnored(t *testing.T) {
	conn := &recordingConn{}
	rig := newNotifierRig(t, conn)
	c := &wecomChannel{installationID: rig.inst}

	if err := c.dispatchFrame(context.Background(), cardPressFrame(t, "single", "", "zhangsan", "token-approve"), rig.senders.get(rig.inst), slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := len(conn.sendFrames()); got != 0 {
		t.Fatalf("frames = %d, want none", got)
	}
}
