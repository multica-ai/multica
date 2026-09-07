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

// A chat:done that lands while the streaming placeholder's sendMessage is
// still in flight must still edit that placeholder. Reading the not-yet-
// recorded message id used to yield 0, which sent the whole reply a second
// time while the placeholder stayed in the chat — two identical messages
// (GH #8049).
func TestOutboundChatDoneRacingPlaceholderSendDoesNotDuplicate(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var texts []string
	release := make(chan struct{})
	sendStarted := make(chan struct{}, 1)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		methods = append(methods, method)
		text, _ := body["text"].(string)
		texts = append(texts, text)
		first := len(methods) == 1
		mu.Unlock()
		if method == "sendMessage" && first {
			// Hold the placeholder mid-flight: Telegram has accepted the
			// request but the id has not come back yet.
			sendStarted <- struct{}{}
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		if method == "sendMessage" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"hello world"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	taskID := telegramTestEvent().TaskID

	go o.handleTaskMessage(events.Event{
		TaskID: taskID,
		Type:   protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{
			TaskID: taskID, Type: "text", Content: "hello world",
		},
	})
	<-sendStarted

	done := telegramTestEvent()
	done.Payload = protocol.ChatDonePayload{
		TaskID: taskID, ChatSessionID: done.ChatSessionID, Content: "hello world",
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- sendTerminalReplySynchronouslyForTest(context.Background(), o, done)
	}()

	// Give terminal delivery time to reach the placeholder-id read before the
	// send is allowed to complete; that read is the raced one. It must now
	// wait for the id rather than observe 0.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		_, streaming := o.streams[taskID]
		o.mu.Unlock()
		if !streaming {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("terminal delivery: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("terminal delivery hung")
	}

	mu.Lock()
	defer mu.Unlock()
	sends := 0
	for _, m := range methods {
		if m == "sendMessage" {
			sends++
		}
	}
	if sends != 1 {
		t.Fatalf("reply delivered %d times, calls=%v texts=%q", sends, methods, texts)
	}
	if len(methods) != 2 || methods[1] != "editMessageText" {
		t.Fatalf("final reply did not edit the placeholder: calls=%v", methods)
	}
}
