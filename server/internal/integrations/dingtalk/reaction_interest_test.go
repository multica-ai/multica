package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type countedTerminalQueries struct {
	deliveryOnlyOutboundQueries
	deliveryReads, taskReads, inputReads int
	deliveryErr                          error
}

func (q *countedTerminalQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	q.deliveryReads++
	return q.delivery, q.deliveryErr
}
func (q *countedTerminalQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	q.taskReads++
	return q.task, nil
}
func (q *countedTerminalQueries) ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error) {
	q.inputReads++
	return q.input, q.inputErr
}

func TestOutboundUnrelatedSessionsDoNotReadReactionInputs(t *testing.T) {
	for _, kind := range []string{"slack", "lark", "wecom", "telegram", "web"} {
		for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
			t.Run(kind+"/"+eventType, func(t *testing.T) {
				q := &countedTerminalQueries{deliveryOnlyOutboundQueries: deliveryOnlyOutboundQueries{
					delivery: db.ChannelTaskDelivery{ChannelType: kind},
					task:     db.AgentTaskQueue{ChatInputTaskID: sessionUUID(3)},
				}}
				if kind == "web" {
					q.deliveryErr = pgx.ErrNoRows
				}
				n := NewAckNotifier(nil, nil, nil, nil)
				// Another DingTalk session must not make the gate process-wide.
				n.client.rememberReplySource(sessionUUID(5), sessionUUID(6), sessionUUID(7), groupReactionMessage("other"))
				event := events.Event{Type: eventType, TaskID: util.UUIDToString(sessionUUID(1)), ChatSessionID: util.UUIDToString(sessionUUID(2)), Payload: map[string]any{"content": "answer", "error": "failed"}}
				if eventType == protocol.EventTaskCancelled {
					event.Payload = nil
				}
				if err := NewOutbound(q, nil, n.client, n, nil).processEvent(context.Background(), event); err != nil {
					t.Fatal(err)
				}
				wantDelivery := 1
				if eventType == protocol.EventTaskCancelled {
					wantDelivery = 0
				}
				if q.deliveryReads != wantDelivery || q.taskReads != 0 || q.inputReads != 0 {
					t.Fatalf("unrelated channel paid reaction queries: delivery=%d task=%d input=%d", q.deliveryReads, q.taskReads, q.inputReads)
				}
			})
		}
	}
}

type interestChatSession struct {
	captureChatSession
	duringCommit func()
}

func (c *interestChatSession) AppendUserMessage(ctx context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	c.duringCommit()
	return c.captureChatSession.AppendUserMessage(ctx, in)
}
func (c *interestChatSession) StartSession(ctx context.Context, in engine.StartSessionInput) (engine.StartSessionResult, error) {
	if in.BeforeCommit != nil {
		if err := in.BeforeCommit(ctx, nil, db.ChatSession{ID: c.startResult.SessionID}); err != nil {
			return engine.StartSessionResult{}, err
		}
	}
	c.duringCommit()
	return c.startResult, c.ensureErr
}

func TestTerminalBeforeSourceCaptureFencesLateReaction(t *testing.T) {
	for _, entry := range []string{"append", "start"} {
		for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
			t.Run(entry+"/"+eventType, func(t *testing.T) {
				sid, tid, inputID := sessionUUID(10), sessionUUID(11), sessionUUID(12)
				inst := engine.ResolvedInstallation{ID: sessionUUID(13)}
				msg := groupReactionMessage("source")
				row := db.ChatMessage{ID: inputID, ChatSessionID: sid, TaskID: tid, Role: "user", ChannelIngested: true}
				n := NewAckNotifier(nil, nil, nil, &ackInputQueries{rows: map[pgtype.UUID]db.ChatMessage{inputID: row}})
				q := &countedTerminalQueries{deliveryOnlyOutboundQueries: deliveryOnlyOutboundQueries{
					task: db.AgentTaskQueue{ChatInputTaskID: tid, ChatSessionID: sid}, input: []db.ChatMessage{row},
				}}
				o := NewOutbound(q, nil, n.client, n, nil)
				capture := &interestChatSession{captureChatSession: captureChatSession{
					appendResult: engine.AppendResult{MessageID: inputID},
					startResult:  engine.StartSessionResult{SessionID: sid, Append: engine.AppendResult{MessageID: inputID}},
				}}
				capture.duringCommit = func() {
					if _, ok := n.client.replySourceFor(inst.ID, inputID); ok {
						t.Fatal("source captured before simulated commit")
					}
					if !n.client.hasReplySession(sid) {
						t.Fatal("input visible without local interest")
					}
					if err := o.processEvent(context.Background(), events.Event{Type: eventType, TaskID: util.UUIDToString(tid), ChatSessionID: util.UUIDToString(sid)}); err != nil {
						t.Fatal(err)
					}
				}
				binder := &sessionBinder{session: capture, replies: n.client}
				if entry == "append" {
					if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{SessionID: sid, InstallationID: inst.ID, Message: msg}); err != nil {
						t.Fatal(err)
					}
				} else {
					callbackRan := false
					_, err := binder.StartSession(context.Background(), engine.StartSessionParams{Installation: inst, Message: msg, PersistMessage: true, BeforeCommit: func(context.Context, pgx.Tx, db.ChatSession) error { callbackRan = true; return nil }})
					if err != nil || !callbackRan {
						t.Fatalf("start callback: ran=%v err=%v", callbackRan, err)
					}
				}
				n.sendReaction = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error {
					t.Fatal("late callback recreated terminal reaction")
					return nil
				}
				n.OnIngested(context.Background(), inst, msg, sid)
				if q.taskReads != 1 || q.inputReads != 1 || len(n.active) != 0 {
					t.Fatalf("terminal fence not exercised: task=%d input=%d active=%v", q.taskReads, q.inputReads, n.active)
				}
			})
		}
	}
}

func TestReplySessionReferencesReleaseOnFailureAndEviction(t *testing.T) {
	client := NewClient(nil, "")
	sid, inst := sessionUUID(20), sessionUUID(21)
	first := client.beginReplyInput(sid)
	second := client.beginReplyInput(sid)
	first()
	if !client.hasReplySession(sid) {
		t.Fatal("one failed append hid overlapping input")
	}
	second()
	if client.hasReplySession(sid) {
		t.Fatal("failed appends leaked interest")
	}
	client.rememberReplySource(inst, sessionUUID(22), sid, groupReactionMessage("accepted"))
	release := client.beginReplyInput(sid)
	for i := 0; i < maxReplySources; i++ {
		client.rememberReplySource(inst, dbid.NewV7(), sessionUUID(23), groupReactionMessage("other"))
	}
	if !client.hasReplySession(sid) {
		t.Fatal("eviction hid in-flight input")
	}
	release()
	if client.hasReplySession(sid) || len(client.sources.sessions) != 1 {
		t.Fatal("evicted sources leaked session index")
	}
}

func TestReplyInputFailureReleasesInterest(t *testing.T) {
	for _, entry := range []string{"append", "start", "start callback"} {
		t.Run(entry, func(t *testing.T) {
			client := NewClient(nil, "")
			sid := sessionUUID(30)
			wantErr := errors.New("transaction failed")
			capture := &interestChatSession{captureChatSession: captureChatSession{appendErr: wantErr, ensureErr: wantErr, startResult: engine.StartSessionResult{SessionID: sid}}, duringCommit: func() {
				if !client.hasReplySession(sid) {
					t.Fatal("missing pending interest")
				}
			}}
			binder := &sessionBinder{session: capture, replies: client}
			var err error
			if entry == "append" {
				_, err = binder.AppendMessage(context.Background(), engine.AppendParams{SessionID: sid})
			} else {
				p := engine.StartSessionParams{PersistMessage: true}
				if entry == "start callback" {
					p.BeforeCommit = func(context.Context, pgx.Tx, db.ChatSession) error { return wantErr }
				}
				_, err = binder.StartSession(context.Background(), p)
			}
			if !errors.Is(err, wantErr) || client.hasReplySession(sid) {
				t.Fatalf("failed transaction leaked interest: %v", err)
			}
		})
	}
}

func TestOutboundReusesSuccessfulInputReadsForSettlement(t *testing.T) {
	for _, tc := range []struct {
		name          string
		empty, failed bool
		wantReads     int
	}{
		{name: "owned input", wantReads: 1}, {name: "empty sealed batch", empty: true, wantReads: 1}, {name: "failed read retried for cleanup", failed: true, wantReads: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sid, tid, id := sessionUUID(40), sessionUUID(41), sessionUUID(42)
			q := &countedTerminalQueries{deliveryOnlyOutboundQueries: deliveryOnlyOutboundQueries{
				delivery: db.ChannelTaskDelivery{ChannelType: string(TypeDingTalk)},
				task:     db.AgentTaskQueue{ChatSessionID: sid, ChatInputTaskID: tid}, channelIngested: true,
				input: []db.ChatMessage{{ID: id, ChannelIngested: true}},
			}}
			if tc.empty {
				q.input = nil
			}
			if tc.failed {
				q.inputErr = errors.New("input unavailable")
			}
			n := NewAckNotifier(nil, nil, nil, nil)
			n.client.rememberReplySource(sessionUUID(43), id, sid, groupReactionMessage("source"))
			o := NewOutbound(q, nil, n.client, n, nil)
			err := o.processEvent(context.Background(), events.Event{Type: protocol.EventChatDone, TaskID: util.UUIDToString(tid), ChatSessionID: util.UUIDToString(sid), Payload: protocol.ChatDonePayload{Content: "answer"}})
			if err != nil || q.taskReads != 1 || q.inputReads != tc.wantReads {
				t.Fatalf("reads: task=%d input=%d err=%v", q.taskReads, q.inputReads, err)
			}
			if n.settledInputs[id] == (tc.empty || tc.failed) {
				t.Fatal("successful sealed batch was not fenced, or failed/empty input invented a fence")
			}
		})
	}
}

func TestActiveReceiptRemainsDiscoverableAfterSourceEviction(t *testing.T) {
	n := NewAckNotifier(nil, nil, nil, nil)
	sid, tid, id := sessionUUID(50), sessionUUID(51), sessionUUID(52)
	inst := engine.ResolvedInstallation{ID: sessionUUID(53)}
	msg := groupReactionMessage("source")
	adds, recalls := 0, 0
	n.sendReaction = func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, _ string, recall bool) error {
		if recall {
			recalls++
		} else {
			adds++
		}
		return nil
	}
	n.client.rememberReplySource(inst.ID, id, sid, msg)
	n.OnIngested(context.Background(), inst, msg, sid)
	for i := 0; i < maxReplySources; i++ {
		n.client.rememberReplySource(inst.ID, dbid.NewV7(), sessionUUID(54), groupReactionMessage("other"))
	}
	if n.client.hasReplySession(sid) {
		t.Fatal("test did not evict source session")
	}
	q := &countedTerminalQueries{deliveryOnlyOutboundQueries: deliveryOnlyOutboundQueries{task: db.AgentTaskQueue{ChatSessionID: sid, ChatInputTaskID: tid}, input: []db.ChatMessage{{ID: id, ChannelIngested: true}}}}
	err := NewOutbound(q, nil, n.client, n, nil).processEvent(context.Background(), events.Event{Type: protocol.EventTaskCancelled, TaskID: util.UUIDToString(tid), ChatSessionID: util.UUIDToString(sid)})
	if err != nil || adds != 1 || recalls != 1 || len(n.active) != 0 {
		t.Fatalf("evicted source hid active cleanup: adds=%d recalls=%d err=%v", adds, recalls, err)
	}
}

func TestIngestKeepsTerminalInterestAcrossSourceEviction(t *testing.T) {
	sid, tid, id := sessionUUID(60), sessionUUID(61), sessionUUID(62)
	inst := engine.ResolvedInstallation{ID: sessionUUID(63)}
	row := db.ChatMessage{ID: id, ChatSessionID: sid, TaskID: tid, Role: "user", ChannelIngested: true}
	iq := &ackInputQueries{rows: map[pgtype.UUID]db.ChatMessage{id: row}}
	n := NewAckNotifier(nil, nil, nil, iq)
	msg := groupReactionMessage("source")
	n.client.rememberReplySource(inst.ID, id, sid, msg)
	q := &countedTerminalQueries{deliveryOnlyOutboundQueries: deliveryOnlyOutboundQueries{task: db.AgentTaskQueue{ChatSessionID: sid, ChatInputTaskID: tid}, input: []db.ChatMessage{row}}}
	reads := 0
	iq.read = func(pgtype.UUID) {
		reads++
		if reads != 1 {
			return
		}
		for i := 0; i < maxReplySources; i++ {
			n.client.rememberReplySource(inst.ID, dbid.NewV7(), sessionUUID(64), groupReactionMessage("other"))
		}
		if _, ok := n.client.replySourceFor(inst.ID, id); ok {
			t.Fatal("test did not evict source")
		}
		err := NewOutbound(q, nil, n.client, n, nil).processEvent(context.Background(), events.Event{Type: protocol.EventTaskCancelled, TaskID: util.UUIDToString(tid), ChatSessionID: util.UUIDToString(sid)})
		if err != nil {
			t.Fatal(err)
		}
	}
	n.sendReaction = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string, bool) error {
		t.Fatal("eviction allowed terminal reaction to reappear")
		return nil
	}
	n.OnIngested(context.Background(), inst, msg, sid)
	if q.inputReads != 1 || len(n.active) != 0 || n.client.hasReplySession(sid) {
		t.Fatal("in-flight interest was lost or leaked")
	}
}

func TestReplyInterestDuplicateCaptureReleasesOnEviction(t *testing.T) {
	client := NewClient(nil, "")
	inst, input, sid := sessionUUID(70), sessionUUID(71), sessionUUID(72)
	msg := groupReactionMessage("source")
	client.rememberReplySource(inst, input, sid, msg)
	client.rememberReplySource(inst, input, sid, msg)
	for i := 0; i < maxReplySources; i++ {
		client.rememberReplySource(inst, dbid.NewV7(), sessionUUID(73), groupReactionMessage("other"))
	}
	if client.hasReplySession(sid) {
		t.Fatal("duplicate capture leaked session interest after source eviction")
	}
}

func TestReplyInterestAbsentClientAndSessionAreNoOps(t *testing.T) {
	var absent *Client
	absent.beginReplyInput(sessionUUID(74))()
	if absent.hasReplySession(sessionUUID(74)) {
		t.Fatal("absent client has reaction interest")
	}
	client := NewClient(nil, "")
	client.beginReplyInput(pgtype.UUID{})()
	client.rememberReplySource(sessionUUID(75), sessionUUID(76), pgtype.UUID{}, groupReactionMessage("source"))
	if client.hasReplySession(pgtype.UUID{}) || len(client.sources.sessions) != 0 {
		t.Fatal("invalid session created reaction interest")
	}
}

func TestStartWithoutInputPreservesCallbackWithoutInterest(t *testing.T) {
	client := NewClient(nil, "")
	sid := sessionUUID(77)
	capture := &interestChatSession{captureChatSession: captureChatSession{
		startResult: engine.StartSessionResult{SessionID: sid},
	}, duringCommit: func() {
		if client.hasReplySession(sid) {
			t.Fatal("empty start registered reaction interest")
		}
	}}
	callbackRan := false
	binder := &sessionBinder{session: capture, replies: client}
	_, err := binder.StartSession(context.Background(), engine.StartSessionParams{
		BeforeCommit: func(context.Context, pgx.Tx, db.ChatSession) error {
			callbackRan = true
			return nil
		},
	})
	if err != nil || !callbackRan || client.hasReplySession(sid) || len(client.sources.entries) != 0 {
		t.Fatalf("empty start changed callback or retained reaction state: callback=%v err=%v", callbackRan, err)
	}
}
