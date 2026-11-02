package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

const (
	cardPressMessageID = "om_card_1"
	cardPressOwnerOpen = "ou_owner_1"
)

// fakeCardActions stands in for the shared chat action handler. It records each
// press and answers with a fixed result. Its ActionKey reads the decision out of
// the token's suffix, so "approve:p1" acts on decision "p1".
type fakeCardActions struct {
	mu      sync.Mutex
	presses []channel.Press
	result  channel.PressResult
	err     error
}

func (f *fakeCardActions) Press(_ context.Context, p channel.Press) (channel.PressResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presses = append(f.presses, p)
	return f.result, f.err
}

func (f *fakeCardActions) pressed() []channel.Press {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]channel.Press(nil), f.presses...)
}

func (f *fakeCardActions) ActionKey(data string) string {
	if i := strings.LastIndex(data, ":"); i >= 0 {
		return data[i+1:]
	}
	return ""
}

func (f *fakeCardActions) Command(context.Context, channel.Command) (channel.Message, bool, error) {
	return channel.Message{}, false, nil
}

// cardPressInstallation is the installation a press arrives on.
func cardPressInstallation(t *testing.T) Installation {
	t.Helper()
	return Installation{
		ID:        uuidFromString(t, notifierInstallationID),
		AppID:     "cli_app_xx",
		BotOpenID: "ou_bot_1",
	}
}

// cardPressPayload is the card.action.trigger event Lark sends for one press.
func cardPressPayload(appID, openID, token, messageID string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"schema": "2.0",
		"header": map[string]any{"event_type": cardActionEventType, "app_id": appID, "event_id": "evt_1"},
		"event": map[string]any{
			"operator": map[string]any{"open_id": openID, "tenant_key": "tenant_1"},
			"action": map[string]any{
				"tag":   "button",
				"value": map[string]string{cardActionValueKey: token},
			},
			"context": map[string]any{"open_message_id": messageID, "open_chat_id": "oc_dm_1"},
		},
	})
	return raw
}

// cardPressSent is what the fake Lark server recorded for a press.
type cardPressSent struct {
	mu      sync.Mutex
	dms     []map[string]string
	patches []map[string]string
	reads   int
}

// newCardPressFake serves the three calls a press makes: the direct message to
// the presser, the read of the card, and the card's patch. The card the read
// returns is cardContent.
func newCardPressFake(t *testing.T, cardContent string) (*larkFakeServer, *cardPressSent) {
	t.Helper()
	f := newLarkFake(t)
	f.stubToken("t-press", 7200)
	sent := &cardPressSent{}
	f.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent.mu.Lock()
		sent.dms = append(sent.dms, body)
		sent.mu.Unlock()
		writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{"message_id": "om_reply_1"}})
	})
	f.mux.HandleFunc("/open-apis/im/v1/messages/"+cardPressMessageID, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			sent.mu.Lock()
			sent.patches = append(sent.patches, body)
			sent.mu.Unlock()
			writeJSON(w, map[string]any{"code": 0, "msg": "success"})
			return
		}
		sent.mu.Lock()
		sent.reads++
		sent.mu.Unlock()
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{"items": []any{map[string]any{
				"message_id": cardPressMessageID,
				"msg_type":   "interactive",
				"body":       map[string]string{"content": cardContent},
			}}},
		})
	})
	return f, sent
}

// briefCardWithTwoDecisions is the card a brief push sends for two decisions:
// the brief link, then per decision Approve and Reject with Revise beside them.
func briefCardWithTwoDecisions(t *testing.T) string {
	t.Helper()
	card, err := briefCard(channel.Message{
		Text: "Weekly plan.\n\nBrief: https://app.example/acme/office?brief=b1",
		Buttons: []channel.Button{
			{Label: "Open the brief", URL: "https://app.example/acme/office?brief=b1"},
			{Label: "Approve: Hire", CallbackData: "approve:p1"},
			{Label: "Reject: Hire", CallbackData: "reject:p1", SameRow: true},
			{Label: "Revise: Hire", URL: "https://app.example/acme/office?decision=p1", SameRow: true},
			{Label: "Approve: Budget", CallbackData: "approve:p2"},
		},
	})
	if err != nil {
		t.Fatalf("briefCard: %v", err)
	}
	return card
}

func newPressHandler(t *testing.T, f *larkFakeServer, actions channel.ActionHandler) CardActionHandler {
	t.Helper()
	return NewCardActionHandler(newTestClient(f, time.Now), fakeNotifierCreds{}, actions, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestCardPressSettlesTheDecisionRepliesAndRemovesItsButtons(t *testing.T) {
	fake, sent := newCardPressFake(t, briefCardWithTwoDecisions(t))
	actions := &fakeCardActions{result: channel.PressResult{Toast: "Approved", Reply: "Approved: Hire.", Settled: true}}
	h := newPressHandler(t, fake, actions)

	h.HandleCardAction(context.Background(), cardPressInstallation(t),
		cardPressPayload("cli_app_xx", cardPressOwnerOpen, "approve:p1", cardPressMessageID))

	pressed := actions.pressed()
	if len(pressed) != 1 {
		t.Fatalf("presses = %d, want 1", len(pressed))
	}
	want := channel.Sender{
		ChannelType:    channel.TypeFeishu,
		InstallationID: uuidFromString(t, notifierInstallationID),
		ChannelUserID:  cardPressOwnerOpen,
	}
	if pressed[0].Sender != want || pressed[0].Data != "approve:p1" {
		t.Fatalf("press = %+v, want the owner's open id and the token", pressed[0])
	}

	sent.mu.Lock()
	defer sent.mu.Unlock()
	if len(sent.dms) != 1 || sent.dms[0]["receive_id"] != cardPressOwnerOpen || !strings.Contains(sent.dms[0]["content"], "Approved: Hire.") {
		t.Fatalf("direct messages = %v, want the result to the presser", sent.dms)
	}
	if len(sent.patches) != 1 {
		t.Fatalf("card patches = %d, want 1", len(sent.patches))
	}
	card := sent.patches[0]["content"]
	if strings.Contains(card, "approve:p1") || strings.Contains(card, "reject:p1") || strings.Contains(card, "decision=p1") {
		t.Fatalf("settled decision kept buttons: %s", card)
	}
	for _, kept := range []string{"approve:p2", "brief=b1", "Weekly plan."} {
		if !strings.Contains(card, kept) {
			t.Fatalf("card lost %q, which must stay: %s", kept, card)
		}
	}
}

func TestCardPressRefusalRepliesAndLeavesTheCardAlone(t *testing.T) {
	fake, sent := newCardPressFake(t, briefCardWithTwoDecisions(t))
	actions := &fakeCardActions{result: channel.PressResult{
		Toast: "Only an owner or admin can decide",
		Reply: "Only an owner or admin can decide from chat. Open the app: https://app.example/acme/office",
	}}
	h := newPressHandler(t, fake, actions)

	h.HandleCardAction(context.Background(), cardPressInstallation(t),
		cardPressPayload("cli_app_xx", "ou_member_1", "approve:p1", cardPressMessageID))

	sent.mu.Lock()
	defer sent.mu.Unlock()
	if len(sent.dms) != 1 || !strings.Contains(sent.dms[0]["content"], "Open the app: https://app.example/acme/office") {
		t.Fatalf("refusal = %v, want the app link to the presser", sent.dms)
	}
	if len(sent.patches) != 0 {
		t.Fatalf("a refused press patched the card: %v", sent.patches)
	}
}

func TestCardPressFromTheBotIsMarkedAsAnAgent(t *testing.T) {
	fake, _ := newCardPressFake(t, briefCardWithTwoDecisions(t))
	actions := &fakeCardActions{result: channel.PressResult{Reply: "Agents cannot decide proposals."}}
	h := newPressHandler(t, fake, actions)

	h.HandleCardAction(context.Background(), cardPressInstallation(t),
		cardPressPayload("cli_app_xx", "ou_bot_1", "approve:p1", cardPressMessageID))

	pressed := actions.pressed()
	if len(pressed) != 1 || !pressed[0].Sender.IsBot {
		t.Fatalf("press from the bot = %+v, want it marked as an agent", pressed)
	}
}

func TestCardPressFromAnotherAppOrWithoutATokenIsIgnored(t *testing.T) {
	cases := map[string][]byte{
		"another app":  cardPressPayload("other_app", cardPressOwnerOpen, "approve:p1", cardPressMessageID),
		"no token":     []byte(`{"header":{"app_id":"cli_app_xx","event_type":"card.action.trigger"},"event":{"operator":{"open_id":"ou_owner_1"},"action":{"value":{}},"context":{"open_message_id":"om_card_1"}}}`),
		"no open id":   []byte(`{"header":{"app_id":"cli_app_xx","event_type":"card.action.trigger"},"event":{"operator":{},"action":{"value":{"token":"approve:p1"}},"context":{"open_message_id":"om_card_1"}}}`),
		"not an event": []byte(`{not json`),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			fake, sent := newCardPressFake(t, briefCardWithTwoDecisions(t))
			actions := &fakeCardActions{result: channel.PressResult{Reply: "x", Settled: true}}
			newPressHandler(t, fake, actions).HandleCardAction(context.Background(), cardPressInstallation(t), payload)

			if got := len(actions.pressed()); got != 0 {
				t.Fatalf("presses = %d, want none", got)
			}
			sent.mu.Lock()
			defer sent.mu.Unlock()
			if len(sent.dms) != 0 || len(sent.patches) != 0 {
				t.Fatalf("an ignored press sent %d messages and %d patches", len(sent.dms), len(sent.patches))
			}
		})
	}
}

func TestCardPressFailureTellsThePresserNothingChanged(t *testing.T) {
	fake, sent := newCardPressFake(t, briefCardWithTwoDecisions(t))
	actions := &fakeCardActions{err: fmt.Errorf("database down")}
	h := newPressHandler(t, fake, actions)

	h.HandleCardAction(context.Background(), cardPressInstallation(t),
		cardPressPayload("cli_app_xx", cardPressOwnerOpen, "approve:p1", cardPressMessageID))

	sent.mu.Lock()
	defer sent.mu.Unlock()
	if len(sent.dms) != 1 || !strings.Contains(sent.dms[0]["content"], "nothing changed") {
		t.Fatalf("failure reply = %v, want the nothing-changed text", sent.dms)
	}
	if len(sent.patches) != 0 {
		t.Fatalf("a failed press patched the card")
	}
}

func TestCardPressReplayOnAnAlreadyDecidedCardChangesNothing(t *testing.T) {
	// The card the first press left behind: p1's buttons are gone, p2's remain.
	card, _, err := withoutDecisionCard(briefCardWithTwoDecisions(t), &fakeCardActions{}, "p1")
	if err != nil {
		t.Fatalf("withoutDecisionCard: %v", err)
	}
	fake, sent := newCardPressFake(t, card)
	actions := &fakeCardActions{result: channel.PressResult{Toast: "Already decided", Reply: "Already decided. Nothing changed.", Settled: true}}
	h := newPressHandler(t, fake, actions)

	h.HandleCardAction(context.Background(), cardPressInstallation(t),
		cardPressPayload("cli_app_xx", cardPressOwnerOpen, "approve:p1", cardPressMessageID))

	sent.mu.Lock()
	defer sent.mu.Unlock()
	if len(sent.dms) != 1 || !strings.Contains(sent.dms[0]["content"], "Already decided") {
		t.Fatalf("replay reply = %v, want already decided", sent.dms)
	}
	if len(sent.patches) != 0 {
		t.Fatalf("a replay patched a card with nothing left to remove")
	}
}

func TestWithoutDecisionCardRemovesOnlyThatDecisionsButtons(t *testing.T) {
	out, removed, err := withoutDecisionCard(briefCardWithTwoDecisions(t), &fakeCardActions{}, "p1")
	if err != nil || !removed {
		t.Fatalf("withoutDecisionCard = (removed %v, err %v), want removed", removed, err)
	}
	var doc struct {
		Body struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	// The markdown, the brief link row and Approve for p2 remain: three elements.
	if len(doc.Body.Elements) != 3 {
		t.Fatalf("elements = %d, want 3 (markdown, brief link, approve p2); got %s", len(doc.Body.Elements), out)
	}
}

func TestWithoutDecisionCardReportsNothingToRemove(t *testing.T) {
	card := briefCardWithTwoDecisions(t)
	out, removed, err := withoutDecisionCard(card, &fakeCardActions{}, "p9")
	if err != nil || removed || out != "" {
		t.Fatalf("withoutDecisionCard(p9) = (%q, %v, %v), want nothing removed", out, removed, err)
	}
}

func TestConnectorAnswersAPressWithoutDecodingIt(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func([]byte, Installation) (InboundMessage, bool, error) {
		t.Errorf("a card press reached the message decoder")
		return InboundMessage{}, false, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour)
	handled := make(chan []byte, 1)
	c.cfg.CardActions = cardActionFunc(func(_ context.Context, _ Installation, payload []byte) {
		handled <- payload
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, cardPressInstallation(t), func(context.Context, InboundMessage) (DispatchResult, error) {
			t.Errorf("a card press was emitted as a message")
			return DispatchResult{}, nil
		})
	}()

	payload := cardPressPayload("cli_app_xx", cardPressOwnerOpen, "approve:p1", cardPressMessageID)
	pushDataFrame(conn, payload, "press-1")

	select {
	case got := <-handled:
		if string(got) != string(payload) {
			t.Fatalf("handler got a different payload")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the press never reached the handler")
	}
	cancel()
	<-done

	acked := false
	for _, raw := range conn.snapshot() {
		frame, err := UnmarshalFrame(raw)
		if err != nil {
			continue
		}
		if frame.Method == FrameMethodData && strings.Contains(string(frame.Payload), `"code":200`) {
			acked = true
		}
	}
	if !acked {
		t.Fatalf("the press was not answered with a 200 ACK")
	}
}

// cardActionFunc adapts a function to CardActionHandler for the connector test.
type cardActionFunc func(context.Context, Installation, []byte)

func (f cardActionFunc) HandleCardAction(ctx context.Context, inst Installation, payload []byte) {
	f(ctx, inst, payload)
}
