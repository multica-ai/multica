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

// An agent often writes one short sentence and then works in tools for
// minutes. The sentence's first chunk opens the message; the rest arrives
// inside the edit interval. Dropping it there left the chat showing a single
// word until the final answer, because no later frame came to carry it.
func TestOutboundFlushesTextAThrottledFrameLeftBehind(t *testing.T) {
	type request struct {
		method string
		body   map[string]any
	}
	var mu sync.Mutex
	var requests []request
	snapshot := func() []request {
		mu.Lock()
		defer mu.Unlock()
		return append([]request(nil), requests...)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		requests = append(requests, request{method: method, body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if method == "sendMessage" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"I"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries(t)
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		cancel()
		if !o.WaitWithTimeout(time.Second) {
			t.Error("outbound workers did not stop")
		}
	}()

	taskID := telegramTestEvent().TaskID
	frame := func(content string) events.Event {
		return events.Event{
			TaskID: taskID, Type: protocol.EventTaskMessage,
			Payload: protocol.TaskMessagePayload{TaskID: taskID, Type: "text", Content: content},
		}
	}

	o.handleTaskMessage(frame("I"))
	if got := snapshot(); len(got) != 1 || got[0].method != "sendMessage" {
		t.Fatalf("first frame requests = %+v, want one sendMessage", got)
	}

	// Leave 150ms of the edit interval, so the test does not wait out all of it.
	o.mu.Lock()
	schedule := o.streams[taskID].schedule
	o.mu.Unlock()
	schedule.mu.Lock()
	schedule.lastEdit = time.Now().Add(150*time.Millisecond - privateEditInterval)
	schedule.mu.Unlock()

	o.handleTaskMessage(frame(" will check the open pull requests"))
	if got := snapshot(); len(got) != 1 {
		t.Fatalf("frame inside the edit interval sent %d requests, want it held back", len(got)-1)
	}

	// No further frame arrives: the agent is in a tool call.
	deadline := time.Now().Add(3 * time.Second)
	for len(snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := snapshot()
	if len(got) != 2 {
		t.Fatalf("held-back text never reached Telegram: requests = %+v", got)
	}
	if got[1].method != "editMessageText" || got[1].body["message_id"] != float64(99) ||
		got[1].body["text"] != "I will check the open pull requests" {
		t.Fatalf("flush request = %+v, want an edit of message 99 with the whole text", got[1])
	}

	// One flush per held-back text, not a timer that keeps firing.
	time.Sleep(300 * time.Millisecond)
	if got := snapshot(); len(got) != 2 {
		t.Fatalf("requests after the flush = %d, want 2", len(got))
	}
}
