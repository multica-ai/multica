package chatstream

import (
	"log/slog"
	"sync"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Event kinds delivered to stream subscribers.
const (
	EventDone      = "done"
	EventFailed    = "failed"
	EventCancelled = "cancelled"
)

// subscriberBuffer sizes each subscription channel. A chat session emits a
// handful of terminal events per run, so a small buffer plus non-blocking
// send keeps the synchronous bus from ever stalling on a slow SSE client.
const subscriberBuffer = 16

// Event is the transport-neutral run event a stream subscriber receives.
type Event struct {
	Type      string
	SessionID string
	TaskID    string
	MessageID string
	Content   string
	ElapsedMs int64
	CreatedAt string
}

// Broker fans chat-run bus events out to per-session SSE subscribers. One
// Broker is created at router setup and subscribes to the event bus once;
// stream handlers register per-request subscriptions keyed by session id.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewBroker wires a broker onto the bus. It listens for chat:done plus the
// task:failed / task:cancelled broadcasts (which carry chat_session_id in
// their map payload rather than the Event scope hint).
func NewBroker(bus *events.Bus) *Broker {
	b := &Broker{subs: make(map[string]map[chan Event]struct{})}
	bus.Subscribe(protocol.EventChatDone, b.onChatDone)
	bus.Subscribe(protocol.EventTaskFailed, b.onTaskTerminal(EventFailed))
	bus.Subscribe(protocol.EventTaskCancelled, b.onTaskTerminal(EventCancelled))
	return b
}

// Subscribe registers a listener for one chat session. The returned cancel
// func must be called when the stream ends; it closes the channel.
func (b *Broker) Subscribe(sessionID string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	b.mu.Lock()
	set := b.subs[sessionID]
	if set == nil {
		set = make(map[chan Event]struct{})
		b.subs[sessionID] = set
	}
	set[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if set, ok := b.subs[sessionID]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(b.subs, sessionID)
				}
			}
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

func (b *Broker) onChatDone(e events.Event) {
	payload, ok := e.Payload.(protocol.ChatDonePayload)
	if !ok {
		return
	}
	sessionID := e.ChatSessionID
	if sessionID == "" {
		sessionID = payload.ChatSessionID
	}
	b.deliver(sessionID, Event{
		Type:      EventDone,
		SessionID: sessionID,
		TaskID:    payload.TaskID,
		MessageID: payload.MessageID,
		Content:   payload.Content,
		ElapsedMs: payload.ElapsedMs,
		CreatedAt: payload.CreatedAt,
	})
}

// onTaskTerminal adapts broadcastTaskEvent's map payload (task_id, status,
// chat_session_id, ...) into a typed subscriber event.
func (b *Broker) onTaskTerminal(kind string) events.Handler {
	return func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		sessionID, _ := payload["chat_session_id"].(string)
		if sessionID == "" {
			return // not a chat task
		}
		taskID, _ := payload["task_id"].(string)
		b.deliver(sessionID, Event{
			Type:      kind,
			SessionID: sessionID,
			TaskID:    taskID,
		})
	}
}

func (b *Broker) deliver(sessionID string, ev Event) {
	if sessionID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[sessionID] {
		select {
		case ch <- ev:
		default:
			// Never block the synchronous bus. A full buffer means the SSE
			// client stopped reading; it will recover via the replay path.
			slog.Warn("chatstream: dropping event for slow subscriber",
				"chat_session_id", sessionID, "event", ev.Type, "task_id", ev.TaskID)
		}
	}
}
