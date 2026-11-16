package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (o *Outbound) processEvent(ctx context.Context, event events.Event) error {
	reply, err := o.prepareEvent(ctx, event)
	if err != nil || reply == nil {
		return err
	}
	return o.deliverReply(ctx, *reply)
}

func TestOutboundFullQueueDoesNotBlockEventBus(t *testing.T) {
	var logs bytes.Buffer
	o := NewOutbound(nil, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	defer close(o.done)
	for _, queue := range o.queues {
		for i := 0; i < cap(queue); i++ {
			queue <- events.Event{}
		}
	}
	bus := events.New()
	o.Register(bus)
	fannedOut := make(chan struct{})
	bus.SubscribeAll(func(events.Event) { close(fannedOut) })
	go bus.Publish(events.Event{Type: protocol.EventChatDone, ChatSessionID: "session", TaskID: "task"})
	select {
	case <-fannedOut:
	case <-time.After(time.Second):
		t.Fatal("full WeChat queue blocked realtime fanout")
	}
	if !strings.Contains(logs.String(), "capacity exceeded") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatal("dropped reply was not logged as an error")
	}
}

func TestOutboundDeliveryQueueLimits(t *testing.T) {
	var logs bytes.Buffer
	o := NewOutbound(nil, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	session := &outboundSession{queue: make([]outboundReply, outboundSessionQueueDepth)}
	o.sessions["session"] = session
	o.enqueueReply(context.Background(), outboundReply{event: events.Event{ChatSessionID: "session"}})
	if len(session.queue) != outboundSessionQueueDepth {
		t.Fatal("full session queue grew")
	}
	for i := 1; i < outboundSessionLimit; i++ {
		o.sessions[util.UUIDToString(dbid.NewV7())] = &outboundSession{}
	}
	o.enqueueReply(context.Background(), outboundReply{event: events.Event{ChatSessionID: "new-session"}})
	if len(o.sessions) != outboundSessionLimit || o.sessions["new-session"] != nil {
		t.Fatal("sender concurrency limit was exceeded")
	}
	if strings.Count(logs.String(), "capacity exceeded") != 2 {
		t.Fatal("queue overflow did not report both dropped replies")
	}
}

func outboundTestEvent(t *testing.T, x *databaseFixture, inst db.ChannelInstallation, session, binding, content string) events.Event {
	t.Helper()
	taskID := x.f.Task(t, util.UUIDToString(x.agent), dbfx.Cols{"chat_session_id": session, "status": "completed", "runtime_id": x.f.Runtime(t, "test runtime")})
	x.f.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, taskID)
	x.f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": session, "role": "user", "content": "hello", "task_id": taskID, "channel_ingested": true})
	info := DecodePublicConfig(inst.Config)
	route, _ := json.Marshal(weixinBindingConfig{BotID: info.BotID, UserID: info.UserID})
	x.f.InsertNoID(t, "channel_task_delivery", dbfx.Cols{"task_id": taskID, "binding_id": binding, "installation_id": inst.ID, "channel_type": "weixin", "channel_chat_id": info.UserID, "chat_type": "p2p", "route_revision": 1, "config": route}, "task_id=$1", taskID)
	x.f.Cleanup(t, `DELETE FROM channel_reply_delivery WHERE turn_id=$1`, taskID)
	x.f.Cleanup(t, `DELETE FROM channel_outbound_message WHERE installation_id=$1`, inst.ID)
	return events.Event{Type: protocol.EventChatDone, WorkspaceID: x.f.WorkspaceID, ChatSessionID: session, TaskID: taskID, Payload: map[string]any{"content": content}}
}

func TestOutboundSlowSendDoesNotBlockRoutingOrOtherSessions(t *testing.T) {
	slow, fast := newDatabaseFixture(t), newDatabaseFixture(t)
	slowInst, fastInst := slow.connect(t), fast.connect(t)
	slowSession := slow.f.ChatSession(t, util.UUIDToString(slow.agent))
	queueIndex := func(s string) uint32 { h := fnv.New32a(); _, _ = h.Write([]byte(s)); return h.Sum32() % 4 }
	fastSession := util.UUIDToString(dbid.NewV7())
	for queueIndex(fastSession) != queueIndex(slowSession) {
		fastSession = util.UUIDToString(dbid.NewV7())
	}
	fast.f.ChatSession(t, util.UUIDToString(fast.agent), dbfx.Cols{"id": fastSession})
	makeBinding := func(x *databaseFixture, inst db.ChannelInstallation, session string) string {
		info := DecodePublicConfig(inst.Config)
		sealed, _ := x.svc.box.Seal([]byte("reply-context"))
		if _, err := x.q.SaveWeixinContext(context.Background(), db.SaveWeixinContextParams{InstallationID: inst.ID, BotID: info.BotID, UserID: info.UserID, ContextTokenEncrypted: sealed, ContextTimeMs: 123}); err != nil {
			t.Fatal(err)
		}
		return x.f.Insert(t, "channel_chat_session_binding", dbfx.Cols{"chat_session_id": session, "installation_id": inst.ID, "channel_type": "weixin", "channel_chat_id": info.UserID, "chat_type": "p2p"})
	}
	slowBinding, fastBinding := makeBinding(slow, slowInst, slowSession), makeBinding(fast, fastInst, fastSession)
	first := outboundTestEvent(t, slow, slowInst, slowSession, slowBinding, "slow-first")
	second := outboundTestEvent(t, slow, slowInst, slowSession, slowBinding, "slow-second")
	other := outboundTestEvent(t, fast, fastInst, fastSession, fastBinding, "fast")
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	sent := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Msg message `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		text := body.Msg.Items[0].Text.Text
		if text == "slow-first" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		sent <- text
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	o := NewOutbound(slow.q, slow.svc.box, nil)
	o.client = providerClient(server)
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	defer func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		if !o.WaitWithTimeout(3 * time.Second) {
			t.Error("outbound workers did not stop")
		}
	}()
	bus := events.New()
	o.Register(bus)
	bus.Publish(first)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first send never started")
	}
	bus.Publish(second)
	bus.Publish(events.Event{Type: protocol.EventChatDone, ChatSessionID: fastSession, TaskID: util.UUIDToString(dbid.NewV7())})
	bus.Publish(other)
	select {
	case text := <-sent:
		if text != "fast" {
			t.Fatalf("same-session reply overtook blocked send: %s", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow WeChat send blocked routing or another conversation")
	}
	releaseOnce.Do(func() { close(release) })
	for _, want := range []string{"slow-first", "slow-second"} {
		select {
		case text := <-sent:
			if text != want {
				t.Fatalf("out-of-order reply: got %s want %s", text, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("missing reply: %s", want)
		}
	}
}
