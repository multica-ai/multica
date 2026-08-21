package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeTelegramOutboundQueries struct {
	task          db.AgentTaskQueue
	taskErr       error
	channelOrigin bool
	originErr     error
	binding       db.ChannelChatSessionBinding
	bindings      map[[16]byte]db.ChannelChatSessionBinding
	installation  db.ChannelInstallation
}

func (f *fakeTelegramOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return f.task, f.taskErr
}

func (f *fakeTelegramOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return f.channelOrigin, f.originErr
}

func (f *fakeTelegramOutboundQueries) GetChannelChatSessionBindingBySession(_ context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	if binding, ok := f.bindings[arg.ChatSessionID.Bytes]; ok {
		return binding, nil
	}
	return f.binding, nil
}

func (f *fakeTelegramOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.installation, nil
}

func telegramTestUUID(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Bytes[0] = b
	id.Valid = true
	return id
}

func telegramTestTask() db.AgentTaskQueue {
	return db.AgentTaskQueue{
		ChatSessionID:   telegramTestUUID(3),
		ChatInputTaskID: telegramTestUUID(4),
	}
}

func telegramTestEvent() events.Event {
	return events.Event{
		TaskID:        "00000000-0000-0000-0000-000000000002",
		ChatSessionID: "00000000-0000-0000-0000-000000000003",
		Type:          protocol.EventChatDone,
		Payload: protocol.ChatDonePayload{
			TaskID:        "00000000-0000-0000-0000-000000000002",
			ChatSessionID: "00000000-0000-0000-0000-000000000003",
			Content:       "reply",
		},
	}
}

func telegramTestEventFor(sessionByte, taskByte byte, content string) events.Event {
	sessionID := fmt.Sprintf("00000000-0000-0000-0000-%012x", sessionByte)
	taskID := fmt.Sprintf("00000000-0000-0000-0000-%012x", taskByte)
	return events.Event{
		TaskID: taskID, ChatSessionID: sessionID, Type: protocol.EventChatDone,
		Payload: protocol.ChatDonePayload{TaskID: taskID, ChatSessionID: sessionID, Content: content},
	}
}

func sendTerminalReplySynchronouslyForTest(ctx context.Context, o *Outbound, e events.Event) error {
	reply := &terminalReply{event: e, byteSize: len(chatDoneContent(e.Payload))}
	for {
		result := o.sendNextTerminalRequest(ctx, reply)
		if result.done {
			o.cleanupTerminalReply(reply)
			return result.err
		}
		if delay := result.retryAt.Sub(o.now()); delay > 0 {
			if err := o.wait(ctx, delay); err != nil {
				o.cleanupTerminalReply(reply)
				return err
			}
		}
	}
}

func telegramTestBinding(chatID int64) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		InstallationID: telegramTestUUID(1),
		ChannelChatID:  strconv.FormatInt(chatID, 10),
		Config:         []byte(fmt.Sprintf(`{"chat_id":"%d"}`, chatID)),
		LastMessageID:  pgtype.Text{String: fmt.Sprintf("%d:17", chatID), Valid: true},
	}
}

func newTelegramOutboundQueries() *fakeTelegramOutboundQueries {
	return &fakeTelegramOutboundQueries{
		task: telegramTestTask(),
		binding: db.ChannelChatSessionBinding{
			InstallationID: telegramTestUUID(1),
			ChannelChatID:  "42",
			Config:         []byte(`{"chat_id":"42"}`),
			LastMessageID:  pgtype.Text{String: "42:17", Valid: true},
		},
		installation: db.ChannelInstallation{
			ID:     telegramTestUUID(1),
			Status: "active",
			Config: []byte(`{"bot_token_encrypted":"MTIzOnNlY3JldA=="}`),
		},
	}
}

func TestResolveTargetFailsClosedWhenTaskLookupFails(t *testing.T) {
	q := newTelegramOutboundQueries()
	q.taskErr = errors.New("database unavailable")
	o := NewOutbound(q, nil, "", nil, nil)

	if _, err := o.resolveTarget(context.Background(), telegramTestEvent(), false); err == nil {
		t.Fatal("expected task lookup error")
	}
}

func TestResolveTargetSkipsDirectTaskOnBoundTelegramSession(t *testing.T) {
	q := newTelegramOutboundQueries()
	o := NewOutbound(q, nil, "", nil, nil)

	target, err := o.resolveTarget(context.Background(), telegramTestEvent(), false)
	if err != nil {
		t.Fatal(err)
	}
	if target != nil {
		t.Fatalf("target = %+v, want nil for a direct task", target)
	}
}

func TestResolveTargetDeliversChannelTaskReply(t *testing.T) {
	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, "", nil, nil)

	target, err := o.resolveTarget(context.Background(), telegramTestEvent(), false)
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.chatID != 42 || target.replyTo != 17 || target.botToken != "123:secret" || target.streamKey != telegramTestEvent().TaskID {
		t.Fatalf("target = %+v", target)
	}
}

func TestResolveTargetIsolatesConcurrentTasksInOneSession(t *testing.T) {
	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, "", nil, nil)

	first := telegramTestEvent()
	second := telegramTestEvent()
	second.TaskID = "00000000-0000-0000-0000-000000000005"
	second.Payload = protocol.ChatDonePayload{
		TaskID: second.TaskID, ChatSessionID: second.ChatSessionID, Content: "second",
	}

	target1, err := o.resolveTarget(context.Background(), first, false)
	if err != nil {
		t.Fatal(err)
	}
	target2, err := o.resolveTarget(context.Background(), second, false)
	if err != nil {
		t.Fatal(err)
	}
	if target1.streamKey == target2.streamKey {
		t.Fatalf("concurrent tasks share stream key %q", target1.streamKey)
	}
}

func TestValidateBindingTokenChannel(t *testing.T) {
	if err := validateBindingTokenChannel(db.ChannelBindingToken{ChannelType: string(TypeTelegram)}); err != nil {
		t.Fatalf("telegram token rejected: %v", err)
	}
	if err := validateBindingTokenChannel(db.ChannelBindingToken{ChannelType: "slack"}); !errors.Is(err, ErrBindingTokenInvalid) {
		t.Fatalf("foreign token error = %v, want ErrBindingTokenInvalid", err)
	}
}

func TestOutboundStreamsBySendingThenEditingTheSameQuotedMessage(t *testing.T) {
	type requestRecord struct {
		method string
		body   map[string]any
	}
	var requests []requestRecord
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %s request: %v", r.URL.Path, err)
			http.Error(w, "bad test request", http.StatusBadRequest)
			return
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		requests = append(requests, requestRecord{method: method, body: body})
		w.Header().Set("Content-Type", "application/json")
		if method == "sendMessage" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"hello"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	taskID := telegramTestEvent().TaskID
	partial := events.Event{
		TaskID: taskID,
		Type:   protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{
			TaskID:  taskID,
			Type:    "text",
			Content: "hello",
		},
	}

	o.handleTaskMessage(partial)
	if len(requests) != 1 || requests[0].method != "sendMessage" {
		t.Fatalf("first partial requests = %+v", requests)
	}
	reply, ok := requests[0].body["reply_parameters"].(map[string]any)
	if !ok || reply["message_id"] != float64(17) {
		t.Fatalf("first partial reply_parameters = %#v", requests[0].body["reply_parameters"])
	}

	o.mu.Lock()
	schedule := o.streams[taskID].schedule
	o.mu.Unlock()
	schedule.mu.Lock()
	schedule.lastEdit = time.Time{}
	schedule.mu.Unlock()
	partial.Payload = protocol.TaskMessagePayload{
		TaskID:  taskID,
		Type:    "text",
		Content: " world",
	}
	o.handleTaskMessage(partial)
	if len(requests) != 2 || requests[1].method != "editMessageText" {
		t.Fatalf("second partial requests = %+v", requests)
	}
	if requests[1].body["message_id"] != float64(99) || requests[1].body["text"] != "hello world" {
		t.Fatalf("second partial body = %#v", requests[1].body)
	}

	done := telegramTestEvent()
	done.Payload = protocol.ChatDonePayload{
		TaskID:        taskID,
		ChatSessionID: done.ChatSessionID,
		Content:       "hello world!",
	}
	schedule.mu.Lock()
	schedule.lastEdit = time.Time{}
	schedule.mu.Unlock()
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), o, done); err != nil {
		t.Fatalf("finish chat: %v", err)
	}
	if len(requests) != 3 || requests[2].method != "editMessageText" {
		t.Fatalf("final requests = %+v", requests)
	}
	if requests[2].body["message_id"] != float64(99) || requests[2].body["text"] != "hello world!" {
		t.Fatalf("final body = %#v", requests[2].body)
	}
	if _, exists := o.streams[taskID]; exists {
		t.Fatal("completed stream state was not cleared")
	}
}

func TestOutboundThrottlesConcurrentTasksPerChat(t *testing.T) {
	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"hello"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	first := telegramTestEvent().TaskID
	second := "00000000-0000-0000-0000-000000000005"
	for _, taskID := range []string{first, second} {
		o.handleTaskMessage(events.Event{
			TaskID: taskID, Type: protocol.EventTaskMessage,
			Payload: protocol.TaskMessagePayload{TaskID: taskID, Type: "text", Content: taskID},
		})
	}
	if requests != 1 {
		t.Fatalf("same-chat concurrent partial requests = %d, want 1", requests)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.streams[first] == nil || o.streams[second] == nil ||
		o.streams[first].schedule != o.streams[second].schedule {
		t.Fatal("concurrent tasks did not share a chat schedule")
	}
}

func TestOutboundCancellationClearsStateAndKeepsPartialMessage(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	taskID := telegramTestEvent().TaskID
	o.mu.Lock()
	schedule := o.retainChatLocked("bot-a", 42)
	o.streams[taskID] = &streamState{chatID: 42, messageID: 99, accumulated: "partial", schedule: schedule}
	o.mu.Unlock()

	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{Type: protocol.EventTaskCancelled, TaskID: taskID})

	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.streams[taskID]; exists {
		t.Fatal("cancelled stream state was not cleared")
	}
	if got := o.chats[chatScheduleKey{botKey: "bot-a", chatID: 42}]; got != schedule || got.refs != 0 {
		t.Fatalf("cancelled chat schedule = %+v, want retained idle schedule", got)
	}
}

func TestOutboundRetainsScheduleAcrossSequentialTasks(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }

	o.mu.Lock()
	first := o.retainChatLocked("bot-a", 42)
	o.releaseChatLocked(first, 42)
	second := o.retainChatLocked("bot-a", 42)
	o.mu.Unlock()

	if first != second {
		t.Fatal("sequential tasks in one chat did not reuse the rate-limit schedule")
	}
}

func TestOutboundExpiresIdleScheduleAfterTTL(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }

	o.mu.Lock()
	first := o.retainChatLocked("bot-a", 42)
	o.releaseChatLocked(first, 42)
	current = current.Add(chatScheduleIdleTTL)
	second := o.retainChatLocked("bot-a", 42)
	o.mu.Unlock()

	if first == second {
		t.Fatal("expired idle chat schedule was reused")
	}
}

func TestOutboundCancellationPreservesActiveRetryAfter(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }
	taskID := telegramTestEvent().TaskID
	backoffTill := current.Add(30 * time.Second)

	o.mu.Lock()
	schedule := o.retainChatLocked("bot-a", 42)
	o.mu.Unlock()
	schedule.mu.Lock()
	schedule.setBackoffTill(backoffTill)
	schedule.mu.Unlock()
	o.mu.Lock()
	o.streams[taskID] = &streamState{chatID: 42, schedule: schedule}
	o.mu.Unlock()

	o.handleTaskCancelled(events.Event{Type: protocol.EventTaskCancelled, TaskID: taskID})

	o.mu.Lock()
	reused := o.retainChatLocked("bot-a", 42)
	o.mu.Unlock()
	if reused != schedule || !reused.backoffTill.Equal(backoffTill) {
		t.Fatalf("retry_after schedule was discarded on cancellation: got %+v", reused)
	}
}

func TestOutboundChatScheduleCacheIsBounded(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }

	o.mu.Lock()
	for chatID := int64(1); chatID <= maxChatSchedules+10; chatID++ {
		schedule := o.retainChatLocked("bot-a", chatID)
		o.releaseChatLocked(schedule, chatID)
	}
	idleCount := len(o.chats)
	o.mu.Unlock()
	if idleCount > maxChatSchedules {
		t.Fatalf("idle chat schedule cache size = %d, want <= %d", idleCount, maxChatSchedules)
	}
}

func TestOutboundIdleSchedulePreservesBackoffBeyondTTL(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }

	o.mu.Lock()
	schedule := o.retainChatLocked("bot-a", 42)
	o.mu.Unlock()
	schedule.mu.Lock()
	schedule.setBackoffTill(current.Add(chatScheduleIdleTTL + time.Minute))
	schedule.mu.Unlock()
	o.mu.Lock()
	o.releaseChatLocked(schedule, 42)
	current = current.Add(chatScheduleIdleTTL)
	reused := o.retainChatLocked("bot-a", 42)
	o.mu.Unlock()

	if reused != schedule {
		t.Fatal("active retry_after schedule was evicted at the idle TTL")
	}
}

func TestOutboundCapacityPruningPreservesActiveBackoff(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }

	o.mu.Lock()
	protected := o.retainChatLocked("bot-a", 1)
	o.releaseChatLocked(protected, 1)
	current = current.Add(time.Nanosecond)
	for chatID := int64(2); chatID <= maxChatSchedules; chatID++ {
		schedule := o.retainChatLocked("bot-a", chatID)
		if schedule == nil {
			o.mu.Unlock()
			t.Fatalf("fill chat %d returned nil", chatID)
		}
		o.releaseChatLocked(schedule, chatID)
	}
	o.mu.Unlock()
	protected.mu.Lock()
	protected.setBackoffTill(current.Add(time.Hour))
	protected.mu.Unlock()
	current = current.Add(editInterval)

	o.mu.Lock()
	for chatID := int64(maxChatSchedules + 1); chatID <= maxChatSchedules+20; chatID++ {
		schedule := o.retainChatLocked("bot-a", chatID)
		if schedule == nil {
			o.mu.Unlock()
			t.Fatalf("retain chat %d returned nil", chatID)
		}
		o.releaseChatLocked(schedule, chatID)
	}
	retained := o.chats[chatScheduleKey{botKey: "bot-a", chatID: 1}]
	o.mu.Unlock()

	if retained != protected {
		t.Fatal("capacity pruning evicted a schedule with active retry_after")
	}
	if got := len(o.chats); got != maxChatSchedules {
		t.Fatalf("chat schedule cache size = %d, want %d", got, maxChatSchedules)
	}
}

func TestOutboundCapacityEvictionPreservesRecentChatCooldown(t *testing.T) {
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		idleFor    time.Duration
		backoffFor time.Duration
		wantRoom   bool
	}{
		{name: "cooldown without retry after", idleFor: editInterval - time.Millisecond},
		{name: "retry after shorter than cooldown", idleFor: time.Second, backoffFor: time.Second},
		{name: "cooldown elapsed", idleFor: editInterval, backoffFor: time.Second, wantRoom: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
			candidateKey := chatScheduleKey{botKey: "bot-a", chatID: 1}
			candidate := &chatSchedule{key: candidateKey, idleSince: current.Add(-tt.idleFor)}
			if tt.backoffFor > 0 {
				candidate.mu.Lock()
				candidate.setBackoffTill(current.Add(tt.backoffFor))
				candidate.mu.Unlock()
			}
			o.chats[candidateKey] = candidate
			for chatID := int64(2); chatID <= maxChatSchedules; chatID++ {
				key := chatScheduleKey{botKey: "bot-a", chatID: chatID}
				o.chats[key] = &chatSchedule{key: key, refs: 1}
			}

			o.mu.Lock()
			gotRoom := o.makeChatScheduleRoomLocked(current)
			_, retained := o.chats[candidateKey]
			o.mu.Unlock()
			if gotRoom != tt.wantRoom {
				t.Fatalf("make room = %v, want %v", gotRoom, tt.wantRoom)
			}
			if retained == tt.wantRoom {
				t.Fatalf("candidate retained = %v, want %v", retained, !tt.wantRoom)
			}
		})
	}
}

func TestOutboundManyActiveBackoffsKeepTotalScheduleCacheBounded(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }

	for chatID := int64(1); chatID <= maxChatSchedules+64; chatID++ {
		o.mu.Lock()
		schedule := o.retainChatLocked("bot-a", chatID)
		o.mu.Unlock()
		if schedule == nil {
			t.Fatalf("schedule %d was refused despite compressible idle backoff entries", chatID)
		}
		schedule.mu.Lock()
		schedule.setBackoffTill(current.Add(time.Hour + time.Duration(chatID)*time.Second))
		schedule.mu.Unlock()
		o.mu.Lock()
		o.releaseChatLocked(schedule, chatID)
		o.mu.Unlock()
		current = current.Add(editInterval)
	}
	o.mu.Lock()
	got := len(o.chats)
	fallback := o.botFallbackBackoff["bot-a"]
	o.mu.Unlock()

	if got > maxChatSchedules {
		t.Fatalf("active-backoff schedule cache size = %d, want <= %d", got, maxChatSchedules)
	}
	if !fallback.After(current) {
		t.Fatal("compressed active backoff was not merged into the bot fallback")
	}
}

func TestOutboundCompressedChatInheritsBotFallback(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }
	backoffTill := current.Add(time.Hour)

	for chatID := int64(1); chatID <= maxChatSchedules; chatID++ {
		o.mu.Lock()
		schedule := o.retainChatLocked("bot-a", chatID)
		o.mu.Unlock()
		schedule.mu.Lock()
		schedule.setBackoffTill(backoffTill)
		schedule.mu.Unlock()
		o.mu.Lock()
		o.releaseChatLocked(schedule, chatID)
		o.mu.Unlock()
		current = current.Add(editInterval)
	}
	o.mu.Lock()
	newSchedule := o.retainChatLocked("bot-a", maxChatSchedules+1)
	compressed := o.chats[chatScheduleKey{botKey: "bot-a", chatID: 1}] == nil
	o.mu.Unlock()
	if newSchedule == nil || !compressed {
		t.Fatal("oldest active schedule was not compressed to make room")
	}
	o.releaseChat(newSchedule, maxChatSchedules+1)
	o.mu.Lock()
	recreated := o.retainChatLocked("bot-a", 1)
	o.mu.Unlock()
	if recreated == nil {
		t.Fatal("compressed chat schedule was not recreated")
	}

	recreated.mu.Lock()
	available := o.terminalAvailableAt(recreated, "bot-a", current)
	recreated.mu.Unlock()
	if available.Before(backoffTill) {
		t.Fatalf("compressed chat fallback available at %s, want >= %s", available, backoffTill)
	}
	o.releaseChat(recreated, 1)
}

func TestOutboundExpiredBotFallbackIsRemoved(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.mu.Lock()
	o.botFallbackBackoff["bot-a"] = current.Add(time.Minute)
	o.mu.Unlock()

	current = current.Add(time.Minute)
	if got := o.botFallbackTill("bot-a", current); !got.IsZero() {
		t.Fatalf("expired bot fallback = %s, want zero", got)
	}
	o.mu.Lock()
	_, exists := o.botFallbackBackoff["bot-a"]
	o.mu.Unlock()
	if exists {
		t.Fatal("expired bot fallback remained resident")
	}
}

func TestOutboundBotFallbackDoesNotThrottleAnotherInstallation(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.mu.Lock()
	o.botFallbackBackoff["bot-a"] = current.Add(time.Hour)
	o.mu.Unlock()
	schedule := &chatSchedule{key: chatScheduleKey{botKey: "bot-b", chatID: 42}}

	schedule.mu.Lock()
	available := o.terminalAvailableAt(schedule, "bot-b", current)
	schedule.mu.Unlock()
	if available.After(current) {
		t.Fatalf("bot-b was delayed by bot-a fallback until %s", available)
	}
}

func TestOutboundCompletionWaitsForRetryAfterBackoff(t *testing.T) {
	var methods []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		methods = append(methods, method)
		w.Header().Set("Content-Type", "application/json")
		switch len(methods) {
		case 1:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"hello"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":2}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }
	var waited []time.Duration
	o.wait = func(_ context.Context, delay time.Duration) error {
		waited = append(waited, delay)
		current = current.Add(delay)
		return nil
	}
	taskID := telegramTestEvent().TaskID
	partial := events.Event{
		TaskID: taskID, Type: protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{TaskID: taskID, Type: "text", Content: "hello"},
	}
	o.handleTaskMessage(partial)
	current = current.Add(3 * time.Second)
	partial.Payload = protocol.TaskMessagePayload{TaskID: taskID, Type: "text", Content: " world"}
	o.handleTaskMessage(partial)

	done := telegramTestEvent()
	done.Payload = protocol.ChatDonePayload{
		TaskID: taskID, ChatSessionID: done.ChatSessionID, Content: "hello world!",
	}
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), o, done); err != nil {
		t.Fatalf("finish chat after 429: %v", err)
	}
	if strings.Join(methods, ",") != "sendMessage,editMessageText,editMessageText" {
		t.Fatalf("methods = %v", methods)
	}
	if len(waited) != 1 || waited[0] != 2*time.Second {
		t.Fatalf("backoff waits = %v, want [2s]", waited)
	}
}

func TestOutboundTerminalWorkerDeliversLongMultiChunkReply(t *testing.T) {
	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"ok"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }
	var simulatedElapsed time.Duration
	o.wait = func(ctx context.Context, delay time.Duration) error {
		deadline, ok := ctx.Deadline()
		if ok && simulatedElapsed+delay > time.Until(deadline) {
			return context.DeadlineExceeded
		}
		simulatedElapsed += delay
		current = current.Add(delay)
		return nil
	}

	done := telegramTestEvent()
	content := strings.Repeat("x", maxMessageUnits*8)
	done.Payload = protocol.ChatDonePayload{
		TaskID: done.TaskID, ChatSessionID: done.ChatSessionID, Content: content,
	}
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), o, done); err != nil {
		t.Fatalf("finish long reply: %v", err)
	}

	if requests != 8 {
		t.Fatalf("terminal chunk requests = %d, want 8", requests)
	}
}

func TestOutboundTerminalDeliveryHasNoFixedRetryAfterBudget(t *testing.T) {
	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1200}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"ok"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }
	var simulatedElapsed time.Duration
	o.wait = func(ctx context.Context, delay time.Duration) error {
		deadline, ok := ctx.Deadline()
		if ok && simulatedElapsed+delay > time.Until(deadline) {
			return context.DeadlineExceeded
		}
		simulatedElapsed += delay
		current = current.Add(delay)
		return nil
	}

	if err := sendTerminalReplySynchronouslyForTest(context.Background(), o, telegramTestEvent()); err != nil {
		t.Fatalf("finish reply after long retry_after: %v", err)
	}
	if requests != 2 {
		t.Fatalf("terminal retry requests = %d, want 2", requests)
	}
	if simulatedElapsed < 20*time.Minute {
		t.Fatalf("retry_after wait = %s, want at least 20m", simulatedElapsed)
	}
}

func TestOutboundChatDoneDoesNotBlockRealtimeFanout(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startOnce sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(requestStarted) })
		<-releaseRequest
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"ok"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		close(releaseRequest)
		cancel()
		if !o.WaitWithTimeout(time.Second) {
			t.Error("terminal workers did not stop")
		}
	}()

	bus := events.New()
	o.Register(bus)
	realtimeSeen := make(chan struct{})
	bus.SubscribeAll(func(events.Event) { close(realtimeSeen) })
	publishReturned := make(chan struct{})
	go func() {
		bus.Publish(telegramTestEvent())
		close(publishReturned)
	}()

	select {
	case <-publishReturned:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("EventChatDone publish blocked on Telegram delivery")
	}
	select {
	case <-realtimeSeen:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("SubscribeAll realtime fanout was blocked by Telegram delivery")
	}
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal worker did not start Telegram delivery")
	}
}

func TestOutboundLegacyLaneCollisionDoesNotBlockAnotherSession(t *testing.T) {
	first := telegramTestEventFor(3, 2, "blocked")
	second := telegramTestEventFor(7, 5, "ready")
	legacyLane := func(key string) int {
		hash := uint32(2166136261)
		for i := 0; i < len(key); i++ {
			hash ^= uint32(key[i])
			hash *= 16777619
		}
		return int(hash % terminalWorkerCount)
	}
	if legacyLane(first.ChatSessionID) != legacyLane(second.ChatSessionID) {
		t.Fatal("test sessions must collide in the removed four-lane scheduler")
	}

	blockedSeen := make(chan struct{})
	readySeen := make(chan struct{})
	var blockedOnce sync.Once
	var readyOnce sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64 `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if body.ChatID == 42 {
			blockedOnce.Do(func() { close(blockedSeen) })
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1200}}`))
			return
		}
		readyOnce.Do(func() { close(readySeen) })
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":43,"type":"private"},"date":0,"text":"ready"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	q.bindings = map[[16]byte]db.ChannelChatSessionBinding{
		{15: 3}: telegramTestBinding(42),
		{15: 7}: telegramTestBinding(43),
	}
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		cancel()
		if !o.WaitWithTimeout(time.Second) {
			t.Error("terminal scheduler did not stop")
		}
	}()

	o.enqueueTerminalReply(first)
	select {
	case <-blockedSeen:
	case <-time.After(time.Second):
		t.Fatal("blocked session did not reach Telegram")
	}
	o.enqueueTerminalReply(second)
	select {
	case <-readySeen:
	case <-time.After(time.Second):
		t.Fatal("unrelated colliding session was blocked by retry_after")
	}
}

func TestOutboundSameSessionSecondReplyCannotOvertakeFirst(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	var firstOnce sync.Once
	var secondOnce sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Text == "first" {
			firstOnce.Do(func() { close(firstStarted) })
			<-releaseFirst
		} else if body.Text == "second" {
			secondOnce.Do(func() { close(secondStarted) })
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"ok"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
		cancel()
		if !o.WaitWithTimeout(time.Second) {
			t.Error("terminal scheduler did not stop")
		}
	}()

	first := telegramTestEventFor(3, 2, "first")
	second := telegramTestEventFor(3, 5, "second")
	o.enqueueTerminalReply(first)
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first reply did not start")
	}
	o.enqueueTerminalReply(second)
	select {
	case <-secondStarted:
		t.Fatal("second reply overtook the in-flight first reply")
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
}

func TestOutboundBackoffSessionsDoNotExhaustWorkers(t *testing.T) {
	backoffSeen := make(chan int64, terminalWorkerCount+2)
	readySeen := make(chan struct{})
	var readyOnce sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64 `json:"chat_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body.ChatID != 200 {
			backoffSeen <- body.ChatID
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1200}}`))
			return
		}
		readyOnce.Do(func() { close(readySeen) })
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":200,"type":"private"},"date":0,"text":"ready"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	q.bindings = make(map[[16]byte]db.ChannelChatSessionBinding)
	for i := byte(1); i <= terminalWorkerCount+2; i++ {
		q.bindings[[16]byte{15: i}] = telegramTestBinding(100 + int64(i))
	}
	q.bindings[[16]byte{15: 20}] = telegramTestBinding(200)
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		cancel()
		if !o.WaitWithTimeout(time.Second) {
			t.Error("terminal scheduler did not stop")
		}
	}()

	for i := byte(1); i <= terminalWorkerCount+2; i++ {
		o.enqueueTerminalReply(telegramTestEventFor(i, i+30, "backoff"))
	}
	for range terminalWorkerCount + 2 {
		select {
		case <-backoffSeen:
		case <-time.After(time.Second):
			t.Fatal("retry-waiting sessions occupied the fixed workers")
		}
	}
	o.enqueueTerminalReply(telegramTestEventFor(20, 60, "ready"))
	select {
	case <-readySeen:
	case <-time.After(time.Second):
		t.Fatal("new ready session could not execute after all workers observed 429")
	}
}

func TestOutboundShutdownAbandonsQueuedRetryWaitingAndInflightJobs(t *testing.T) {
	retrySeen := make(chan struct{})
	inflightSeen := make(chan struct{})
	var retryOnce sync.Once
	var inflightOnce sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64 `json:"chat_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch body.ChatID {
		case 42:
			retryOnce.Do(func() { close(retrySeen) })
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1200}}`))
		case 43:
			inflightOnce.Do(func() { close(inflightSeen) })
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":44,"type":"private"},"date":0,"text":"ok"}}`))
		}
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	q.bindings = map[[16]byte]db.ChannelChatSessionBinding{
		{15: 3}: telegramTestBinding(42),
		{15: 7}: telegramTestBinding(43),
	}
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	o.enqueueTerminalReply(telegramTestEventFor(3, 2, "retry"))
	o.enqueueTerminalReply(telegramTestEventFor(7, 5, "inflight"))
	o.enqueueTerminalReply(telegramTestEventFor(7, 6, "queued"))
	select {
	case <-retrySeen:
	case <-time.After(time.Second):
		t.Fatal("retry-waiting job was not established")
	}
	select {
	case <-inflightSeen:
	case <-time.After(time.Second):
		t.Fatal("in-flight job was not established")
	}

	cancel()
	if !o.WaitWithTimeout(time.Second) {
		t.Fatal("terminal scheduler did not stop")
	}
	o.terminalMu.Lock()
	sessions := len(o.terminalSessions)
	retries := o.terminalRetries.Len()
	inflight := o.terminalInFlight
	stopped := o.terminalStopped
	o.terminalMu.Unlock()
	if !stopped || sessions != 0 || retries != 0 || inflight != 0 {
		t.Fatalf("shutdown state stopped=%v sessions=%d retries=%d inflight=%d", stopped, sessions, retries, inflight)
	}
}

func TestOutboundRetryResumesAtFailedChunkWithoutRepeatingSuccess(t *testing.T) {
	var sequence []byte
	var requests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sequence = append(sequence, body.Text[0])
		request := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if request == 2 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1200}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"ok"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	current := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return current }
	o.wait = func(_ context.Context, delay time.Duration) error {
		current = current.Add(delay)
		return nil
	}
	e := telegramTestEvent()
	e.Payload = protocol.ChatDonePayload{
		TaskID: e.TaskID, ChatSessionID: e.ChatSessionID,
		Content: strings.Repeat("A", maxMessageUnits) + strings.Repeat("B", maxMessageUnits) + "C",
	}
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), o, e); err != nil {
		t.Fatalf("finish chunked reply: %v", err)
	}
	if got := string(sequence); got != "ABBC" {
		t.Fatalf("chunk request sequence = %q, want ABBC", got)
	}
}

func TestOutboundTerminalSchedulerPreservesSessionFIFO(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	first := telegramTestEvent()
	second := telegramTestEvent()
	second.TaskID = "00000000-0000-0000-0000-000000000005"
	second.Payload = protocol.ChatDonePayload{
		TaskID: second.TaskID, ChatSessionID: second.ChatSessionID, Content: "second",
	}

	o.enqueueTerminalReply(first)
	o.enqueueTerminalReply(second)
	o.terminalMu.Lock()
	session := o.terminalSessions[first.ChatSessionID]
	o.terminalMu.Unlock()
	if session == nil || len(session.queue) != 2 {
		t.Fatalf("terminal session queue = %+v, want 2 jobs", session)
	}
	if session.queue[0].event.TaskID != first.TaskID || session.queue[1].event.TaskID != second.TaskID {
		t.Fatalf("terminal FIFO = [%s, %s], want [%s, %s]",
			session.queue[0].event.TaskID, session.queue[1].event.TaskID, first.TaskID, second.TaskID)
	}
}

func TestOutboundRejectsSixtyFifthQueuedTerminalReply(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	for i := byte(0); i < maxQueuedTerminalReplies; i++ {
		sessionByte := i/byte(maxQueuedTerminalRepliesPerSession) + 1
		o.enqueueTerminalReply(telegramTestEventFor(sessionByte, i+20, "queued"))
	}
	rejected := telegramTestEventFor(20, 100, "rejected")
	o.enqueueTerminalReply(rejected)

	o.terminalMu.Lock()
	count := o.queuedTerminalReplyCount
	_, rejectedSessionExists := o.terminalSessions[rejected.ChatSessionID]
	o.terminalMu.Unlock()
	if count != maxQueuedTerminalReplies || rejectedSessionExists {
		t.Fatalf("queued replies = %d, rejected session exists = %v", count, rejectedSessionExists)
	}
}

func TestOutboundRejectsNinthQueuedTerminalReplyInSession(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	for i := byte(0); i < maxQueuedTerminalRepliesPerSession; i++ {
		o.enqueueTerminalReply(telegramTestEventFor(3, i+20, "queued"))
	}
	o.enqueueTerminalReply(telegramTestEventFor(3, 100, "rejected"))

	o.terminalMu.Lock()
	session := o.terminalSessions[telegramTestEventFor(3, 20, "").ChatSessionID]
	count := o.queuedTerminalReplyCount
	o.terminalMu.Unlock()
	if session == nil || len(session.queue) != maxQueuedTerminalRepliesPerSession || count != maxQueuedTerminalRepliesPerSession {
		t.Fatalf("session queue = %v, total = %d", session, count)
	}
}

func TestOutboundRejectsTerminalReplyBeyondByteLimit(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	e := telegramTestEventFor(3, 2, strings.Repeat("x", maxQueuedTerminalReplyBytes+1))
	o.enqueueTerminalReply(e)

	o.terminalMu.Lock()
	count := o.queuedTerminalReplyCount
	bytes := o.queuedTerminalReplyBytes
	o.terminalMu.Unlock()
	if count != 0 || bytes != 0 {
		t.Fatalf("rejected payload consumed capacity: count=%d bytes=%d", count, bytes)
	}
}

func TestOutboundCompletedTerminalReplyReleasesCapacityImmediately(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	e := telegramTestEventFor(3, 2, "reply")
	o.enqueueTerminalReply(e)

	o.terminalMu.Lock()
	session := o.terminalSessions[e.ChatSessionID]
	reply := session.queue[0]
	session.ready = false
	session.running = true
	o.terminalInFlight = 1
	o.terminalMu.Unlock()
	o.updateQueueAfterTerminalRequest(terminalResult{
		terminalWork: terminalWork{sessionID: e.ChatSessionID, reply: reply},
		done:         true,
	})

	o.terminalMu.Lock()
	count := o.queuedTerminalReplyCount
	bytes := o.queuedTerminalReplyBytes
	o.terminalMu.Unlock()
	if count != 0 || bytes != 0 {
		t.Fatalf("completed reply retained capacity: count=%d bytes=%d", count, bytes)
	}
}

func TestOutboundShutdownClearsTerminalReplyCapacity(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	o.enqueueTerminalReply(telegramTestEventFor(3, 2, "first"))
	o.enqueueTerminalReply(telegramTestEventFor(7, 5, "second"))
	o.stopTerminalScheduler()

	o.terminalMu.Lock()
	count := o.queuedTerminalReplyCount
	bytes := o.queuedTerminalReplyBytes
	sessions := len(o.terminalSessions)
	o.terminalMu.Unlock()
	if count != 0 || bytes != 0 || sessions != 0 {
		t.Fatalf("shutdown state count=%d bytes=%d sessions=%d", count, bytes, sessions)
	}
}

func TestOutboundRejectedTerminalReplyReleasesStreamSchedule(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, logger)
	e := telegramTestEventFor(3, 2, "rejected")
	schedule := &chatSchedule{key: chatScheduleKey{botKey: "bot", chatID: 42}, refs: 1}
	o.streams[e.TaskID] = &streamState{chatID: 42, schedule: schedule}
	o.chats[schedule.key] = schedule
	o.queuedTerminalReplyCount = maxQueuedTerminalReplies

	o.enqueueTerminalReply(e)
	o.mu.Lock()
	_, streamExists := o.streams[e.TaskID]
	refs := schedule.refs
	o.mu.Unlock()
	if streamExists || refs != 0 {
		t.Fatalf("rejected reply retained stream=%v schedule refs=%d", streamExists, refs)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "level=ERROR") || !strings.Contains(logOutput, e.TaskID) || !strings.Contains(logOutput, e.ChatSessionID) {
		t.Fatalf("rejection log lacks error level or identity: %s", logOutput)
	}
}

func TestOutboundEmptyTerminalReplySkipsQueueAndClearsStream(t *testing.T) {
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, nil)
	e := telegramTestEventFor(3, 2, "")
	schedule := &chatSchedule{key: chatScheduleKey{botKey: "bot", chatID: 42}, refs: 1}
	o.streams[e.TaskID] = &streamState{chatID: 42, schedule: schedule}
	o.chats[schedule.key] = schedule

	o.enqueueTerminalReply(e)
	o.terminalMu.Lock()
	count := o.queuedTerminalReplyCount
	o.terminalMu.Unlock()
	o.mu.Lock()
	_, streamExists := o.streams[e.TaskID]
	refs := schedule.refs
	o.mu.Unlock()
	if count != 0 || streamExists || refs != 0 {
		t.Fatalf("empty reply count=%d stream=%v schedule refs=%d", count, streamExists, refs)
	}
}

func TestOutboundInvalidTerminalReplyIdentityFailsFast(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	o := NewOutbound(newTelegramOutboundQueries(), nil, "", nil, logger)
	e := telegramTestEvent()
	e.TaskID = "invalid-task"
	e.ChatSessionID = "invalid-session"
	o.enqueueTerminalReply(e)

	o.terminalMu.Lock()
	count := o.queuedTerminalReplyCount
	o.terminalMu.Unlock()
	logOutput := logs.String()
	if count != 0 || !strings.Contains(logOutput, "level=ERROR") ||
		!strings.Contains(logOutput, e.TaskID) || !strings.Contains(logOutput, e.ChatSessionID) {
		t.Fatalf("invalid identity count=%d log=%s", count, logOutput)
	}
}

func TestOutboundTerminalReplyUsesPayloadTaskIDFromProductionEvent(t *testing.T) {
	delivered := make(chan struct{})
	var deliveredOnce sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deliveredOnce.Do(func() { close(delivered) })
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"reply"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		cancel()
		if !o.WaitWithTimeout(time.Second) {
			t.Error("terminal scheduler did not stop")
		}
	}()

	e := telegramTestEvent()
	e.TaskID = ""
	o.enqueueTerminalReply(e)
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("terminal reply with payload-only task id was not delivered")
	}
}

func TestOutboundRetryPendingFailureCleansStreamWithoutNotice(t *testing.T) {
	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	taskID := telegramTestEvent().TaskID
	o.mu.Lock()
	schedule := o.retainChatLocked("bot-a", 42)
	o.streams[taskID] = &streamState{chatID: 42, messageID: 99, schedule: schedule}
	o.mu.Unlock()

	o.handleTaskFailed(events.Event{
		Type: protocol.EventTaskFailed, TaskID: taskID,
		Payload: map[string]any{"retry_pending": true},
	})

	o.mu.Lock()
	_, streamExists := o.streams[taskID]
	o.mu.Unlock()
	if streamExists {
		t.Fatal("retry-pending failure did not clear the old stream")
	}
	if requests != 0 {
		t.Fatalf("retry-pending failure sent %d terminal notices, want 0", requests)
	}
}

func TestOutboundTerminalFailureSendsNotice(t *testing.T) {
	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"failed"}}`))
	}))
	defer api.Close()

	q := newTelegramOutboundQueries()
	q.channelOrigin = true
	o := NewOutbound(q, nil, api.URL, api.Client(), nil)
	e := telegramTestEvent()
	e.Type = protocol.EventTaskFailed
	e.Payload = map[string]any{"retry_pending": false}
	o.handleTaskFailed(e)
	if requests != 1 {
		t.Fatalf("terminal failure requests = %d, want 1", requests)
	}
}
