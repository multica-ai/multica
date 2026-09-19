package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// gatedTelegramAPI records every Bot API call and holds the first sendMessage
// open until the returned release func runs, so a test can act while Telegram
// has the placeholder but its id has not come back yet.
func gatedTelegramAPI(t *testing.T) (server *httptest.Server, calls func() ([]string, []map[string]any), sendStarted <-chan struct{}, release func()) {
	t.Helper()
	var mu sync.Mutex
	var methods []string
	var bodies []map[string]any
	started := make(chan struct{}, 1)
	gate := make(chan struct{})
	var releaseOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		methods = append(methods, method)
		bodies = append(bodies, body)
		first := len(methods) == 1
		mu.Unlock()
		if method == "sendMessage" && first {
			started <- struct{}{}
			<-gate
		}
		w.Header().Set("Content-Type", "application/json")
		if method == "sendMessage" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"x"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(srv.Close)

	return srv, func() ([]string, []map[string]any) {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), methods...), append([]map[string]any(nil), bodies...)
		}, started, func() {
			releaseOnce.Do(func() { close(gate) })
		}
}

func telegramPartialEvent(taskID, content string) events.Event {
	return events.Event{
		TaskID: taskID,
		Type:   protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{
			TaskID: taskID, Type: "text", Content: content,
		},
	}
}

// A chat:done that lands while the placeholder's sendMessage is still in
// flight must wait for that id and edit the placeholder. Reading the
// not-yet-recorded id used to yield 0, which posted the whole reply a second
// time while the placeholder stayed in the chat (GH #8049).
func TestOutboundChatDoneWaitsForInFlightPlaceholderSend(t *testing.T) {
	api, calls, sendStarted, release := gatedTelegramAPI(t)
	defer release()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	taskID := telegramTestEvent().TaskID

	partialDone := make(chan struct{})
	go func() {
		defer close(partialDone)
		o.handleTaskMessage(telegramPartialEvent(taskID, "hello world"))
	}()
	<-sendStarted // Telegram has the placeholder; its id is still in the air.

	done := telegramTestEvent()
	done.Payload = protocol.ChatDonePayload{
		TaskID: taskID, ChatSessionID: done.ChatSessionID, Content: "hello world",
	}
	reply := &terminalReply{event: done, byteSize: len(chatDoneContent(done.Payload))}

	// Explicit handshake rather than a scheduling window: terminal delivery
	// runs here, with the send provably mid-flight, and must defer instead of
	// initializing against an id of 0.
	result := o.sendNextTerminalRequest(context.Background(), reply)
	if result.done || result.err != nil {
		t.Fatalf("terminal delivery finished during the in-flight send: %+v", result)
	}
	if !result.retryAt.After(o.now()) {
		t.Fatalf("terminal delivery did not defer: retryAt = %v", result.retryAt)
	}
	if reply.initialized {
		t.Fatal("terminal delivery initialized against an unrecorded placeholder id")
	}
	o.mu.Lock()
	_, streaming := o.streams[taskID]
	o.mu.Unlock()
	if !streaming {
		t.Fatal("terminal delivery consumed the stream while its send was in flight")
	}

	release()
	<-partialDone

	// The placeholder has landed. Clear the chat's edit cooldown so the final
	// edit is not paced by wall-clock time in a test.
	o.mu.Lock()
	schedule := o.streams[taskID].schedule
	o.mu.Unlock()
	schedule.mu.Lock()
	schedule.lastEdit = time.Time{}
	schedule.mu.Unlock()

	for {
		result = o.sendNextTerminalRequest(context.Background(), reply)
		if result.done {
			break
		}
		if result.retryAt.After(o.now()) {
			t.Fatalf("unexpected wait after the placeholder settled: %+v", result)
		}
	}
	if result.err != nil {
		t.Fatalf("terminal delivery: %v", result.err)
	}

	methods, bodies := calls()
	if len(methods) != 2 || methods[0] != "sendMessage" || methods[1] != "editMessageText" {
		t.Fatalf("reply was not delivered as one placeholder plus one edit: %v", methods)
	}
	if bodies[1]["message_id"] != float64(99) || bodies[1]["text"] != "hello world" {
		t.Fatalf("final edit body = %#v", bodies[1])
	}
}

// The mirror case: terminal delivery gets the stream first, between the
// partial registering it and that partial claiming the send. The partial no
// longer owns the reply and must not post its placeholder on top of the
// terminal one.
func TestOutboundPartialSkipsSendAfterTerminalConsumedTheStream(t *testing.T) {
	api, calls, _, release := gatedTelegramAPI(t)
	release() // no gating needed here
	ctx := context.Background()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	taskID := telegramTestEvent().TaskID

	target, err := o.resolveTarget(ctx, telegramPartialEvent(taskID, "hello world"), true)
	if err != nil || target == nil {
		t.Fatalf("resolve target: %v (target %v)", err, target)
	}
	// Register the stream exactly as handleTaskMessage does, and stop there:
	// this is the state a partial is in when it is about to claim the send.
	o.mu.Lock()
	st := &streamState{
		chatID: target.chatID, threadID: target.threadID, replyTo: target.replyTo,
		accumulated: "hello world",
		schedule:    o.retainChatLocked(target.botKey, target.chatID),
	}
	o.streams[target.streamKey] = st
	o.mu.Unlock()

	done := telegramTestEvent()
	done.Payload = protocol.ChatDonePayload{
		TaskID: taskID, ChatSessionID: done.ChatSessionID, Content: "hello world",
	}
	if err := sendTerminalReplySynchronouslyForTest(ctx, o, done); err != nil {
		t.Fatalf("terminal delivery: %v", err)
	}
	if methods, _ := calls(); len(methods) != 1 || methods[0] != "sendMessage" {
		t.Fatalf("terminal delivery calls = %v", methods)
	}

	// Clear the cooldown the terminal send just set, so the only thing that
	// can hold this partial back is the ownership check under test.
	st.schedule.mu.Lock()
	st.schedule.lastEdit = time.Time{}
	st.schedule.setBackoffTill(time.Time{})
	st.schedule.mu.Unlock()

	o.pushPartial(ctx, target, st, 0, "hello world")

	if methods, _ := calls(); len(methods) != 1 {
		t.Fatalf("partial posted a placeholder for a reply it no longer owned: %v", methods)
	}
}

// A reply waiting for chat-schedule capacity waits without a bound, so it must
// re-resolve its delivery target each attempt: an installation revoked during
// that wait must stop the reply, not be delivered to from a target resolved
// before the revocation.
func TestOutboundCapacityRetryRechecksInstallation(t *testing.T) {
	api, calls, _, release := gatedTelegramAPI(t)
	release() // nothing to gate here
	ctx := context.Background()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)

	// Saturate the schedule cache with in-use entries so this reply's chat
	// cannot be retained and delivery has to wait for capacity.
	o.mu.Lock()
	for chatID := int64(1); chatID <= maxChatSchedules; chatID++ {
		o.retainChatLocked("bot-filler", chatID)
	}
	o.mu.Unlock()

	done := telegramTestEvent()
	done.Payload = protocol.ChatDonePayload{
		TaskID: done.TaskID, ChatSessionID: done.ChatSessionID, Content: "hello world",
	}
	reply := &terminalReply{event: done, byteSize: len(chatDoneContent(done.Payload))}

	result := o.sendNextTerminalRequest(ctx, reply)
	if result.done || !result.retryAt.After(o.now()) {
		t.Fatalf("delivery did not wait for capacity: %+v", result)
	}
	if reply.initialized {
		t.Fatal("delivery initialized without a chat schedule")
	}

	// The installation is revoked while the reply waits, and a slot frees up.
	q.installation.Status = "revoked"
	o.mu.Lock()
	delete(o.chats, chatScheduleKey{botKey: "bot-filler", chatID: 1})
	o.mu.Unlock()

	result = o.sendNextTerminalRequest(ctx, reply)
	if reply.initialized {
		t.Fatal("delivery initialized against a revoked installation")
	}
	if !result.done || result.err != nil {
		t.Fatalf("delivery to a revoked installation was not dropped: %+v", result)
	}
	if methods, _ := calls(); len(methods) != 0 {
		t.Fatalf("delivered to a revoked installation: %v", methods)
	}
}
