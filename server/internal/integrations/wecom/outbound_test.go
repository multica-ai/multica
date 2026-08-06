package wecom

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeOutboundQueries struct {
	enqueued []db.EnqueueChannelOutboundParams
	task     db.AgentTaskQueue
	ingested bool
	binding  db.ChannelChatSessionBinding
	inst     db.ChannelInstallation
	enqErr   error
}

func (f *fakeOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return f.task, nil
}
func (f *fakeOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return f.ingested, nil
}
func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	if !f.binding.ChatSessionID.Valid {
		return db.ChannelChatSessionBinding{}, pgx.ErrNoRows
	}
	return f.binding, nil
}
func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.inst, nil
}
func (f *fakeOutboundQueries) GetAgent(context.Context, pgtype.UUID) (db.Agent, error) {
	return db.Agent{Name: "Bot"}, nil
}
func (f *fakeOutboundQueries) GetChatMessageByTaskAssistant(context.Context, pgtype.UUID) (db.ChatMessage, error) {
	return db.ChatMessage{}, pgx.ErrNoRows
}
func (f *fakeOutboundQueries) EnqueueChannelOutbound(_ context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if f.enqErr != nil {
		return db.ChannelOutboundQueue{}, f.enqErr
	}
	f.enqueued = append(f.enqueued, arg)
	return db.ChannelOutboundQueue{}, nil
}

func TestOutboundChatDoneEnqueuesAndWakes(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000001")
	sessionID := parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000001")
	instID := parseTestUUID(t, "cccccccc-cccc-cccc-cccc-000000000001")
	wake := NewOutboundWakeRegistry()
	wake.Register(util.UUIDToString(instID))

	q := &fakeOutboundQueries{
		task:     db.AgentTaskQueue{ID: taskID, ChatSessionID: sessionID, ChatInputTaskID: pgtype.UUID{}},
		ingested: true,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID:  sessionID,
			InstallationID: instID,
			Config:         []byte(`{"target_chat_id":"user1","target_chat_type":1}`),
		},
		inst: db.ChannelInstallation{ID: instID, WorkspaceID: parseTestUUID(t, "dddddddd-dddd-dddd-dddd-000000000001"), Status: "active"},
	}
	o := NewOutbound(OutboundConfig{Queries: q, Wake: wake})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        util.UUIDToString(taskID),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload:       map[string]any{"content": "hello"},
	})
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueue, got %d", len(q.enqueued))
	}
	if q.enqueued[0].SourceKind != sourceKindChatDone {
		t.Fatalf("source_kind=%q", q.enqueued[0].SourceKind)
	}
	select {
	case <-wake.Register(util.UUIDToString(instID)):
	default:
		t.Fatal("expected wake")
	}
}

// TestOutboundChatDoneTypedPayloadEnqueues pins the event shape TaskService
// actually publishes (broadcastChatDone in internal/service/task.go): a typed
// protocol.ChatDonePayload, and no envelope TaskID. Handling only map payloads
// made this subscriber drop every reply, so delivery fell through to the
// reconciler's 30s-lagged scan window and users saw ~40s replies.
func TestOutboundChatDoneTypedPayloadEnqueues(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000004")
	sessionID := parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000004")
	instID := parseTestUUID(t, "cccccccc-cccc-cccc-cccc-000000000004")
	wake := NewOutboundWakeRegistry()
	wake.Register(util.UUIDToString(instID))

	q := &fakeOutboundQueries{
		task:     db.AgentTaskQueue{ID: taskID, ChatSessionID: sessionID},
		ingested: true,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID:  sessionID,
			InstallationID: instID,
			Config:         []byte(`{"target_chat_id":"user1","target_chat_type":1}`),
		},
		inst: db.ChannelInstallation{ID: instID, WorkspaceID: parseTestUUID(t, "dddddddd-dddd-dddd-dddd-000000000004"), Status: "active"},
	}
	o := NewOutbound(OutboundConfig{Queries: q, Wake: wake})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: util.UUIDToString(sessionID),
		Payload: protocol.ChatDonePayload{
			ChatSessionID: util.UUIDToString(sessionID),
			TaskID:        util.UUIDToString(taskID),
			Content:       "hello",
		},
	})
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueue for the typed chat:done payload, got %d", len(q.enqueued))
	}
	if q.enqueued[0].SourceKind != sourceKindChatDone {
		t.Fatalf("source_kind=%q", q.enqueued[0].SourceKind)
	}
	select {
	case <-wake.Register(util.UUIDToString(instID)):
	default:
		t.Fatal("expected wake")
	}
}

// TestOutboundTaskFailedRecoversSessionFromTask pins the task:failed shape
// TaskService publishes: a map payload carrying task_id only, with no envelope
// ChatSessionID and no chat_session_id payload field. The chat session has to
// come from the task row, otherwise every failure notice also falls through to
// the reconciler.
func TestOutboundTaskFailedRecoversSessionFromTask(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000005")
	sessionID := parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000005")
	instID := parseTestUUID(t, "cccccccc-cccc-cccc-cccc-000000000005")

	q := &fakeOutboundQueries{
		task: db.AgentTaskQueue{
			ID:            taskID,
			ChatSessionID: sessionID,
			FailureReason: pgtype.Text{String: "runtime exited", Valid: true},
		},
		ingested: true,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID:  sessionID,
			InstallationID: instID,
			Config:         []byte(`{"target_chat_id":"user1","target_chat_type":1}`),
		},
		inst: db.ChannelInstallation{ID: instID, WorkspaceID: parseTestUUID(t, "dddddddd-dddd-dddd-dddd-000000000005"), Status: "active"},
	}
	o := NewOutbound(OutboundConfig{Queries: q})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type: protocol.EventTaskFailed,
		Payload: map[string]any{
			"task_id":        util.UUIDToString(taskID),
			"status":         "failed",
			"failure_reason": "runtime exited",
		},
	})
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueue for task:failed, got %d", len(q.enqueued))
	}
	if q.enqueued[0].SourceKind != sourceKindTaskFailed {
		t.Fatalf("source_kind=%q", q.enqueued[0].SourceKind)
	}
}

// TestOutboundSkipsTaskWithoutChatSession keeps issue / autopilot tasks out of
// the channel outbound queue now that the session is resolved from the task row
// instead of the event envelope.
func TestOutboundSkipsTaskWithoutChatSession(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000006")
	q := &fakeOutboundQueries{
		task:     db.AgentTaskQueue{ID: taskID},
		ingested: true,
		inst:     db.ChannelInstallation{Status: "active"},
	}
	o := NewOutbound(OutboundConfig{Queries: q})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type:    protocol.EventChatDone,
		Payload: protocol.ChatDonePayload{TaskID: util.UUIDToString(taskID), Content: "hello"},
	})
	if len(q.enqueued) != 0 {
		t.Fatalf("expected no enqueue, got %d", len(q.enqueued))
	}
}

func TestOutboundSkipsNonIngestedTask(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000002")
	sessionID := parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000002")
	inputID := parseTestUUID(t, "eeeeeeee-eeee-eeee-eeee-000000000001")
	q := &fakeOutboundQueries{
		task: db.AgentTaskQueue{
			ID:              taskID,
			ChatSessionID:   sessionID,
			ChatInputTaskID: inputID,
		},
		ingested: false,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID: sessionID,
			Config:        []byte(`{"target_chat_id":"user1","target_chat_type":1}`),
		},
		inst: db.ChannelInstallation{Status: "active"},
	}
	o := NewOutbound(OutboundConfig{Queries: q})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        util.UUIDToString(taskID),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload:       map[string]any{"content": "hello"},
	})
	if len(q.enqueued) != 0 {
		t.Fatalf("expected no enqueue, got %d", len(q.enqueued))
	}
}

func TestOutboundDuplicateEventIdempotentAtDB(t *testing.T) {
	q := &fakeOutboundQueries{
		task:     db.AgentTaskQueue{ChatInputTaskID: pgtype.UUID{}},
		ingested: true,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID: parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000003"),
			Config:        []byte(`{"target_chat_id":"u","target_chat_type":1}`),
		},
		inst: db.ChannelInstallation{Status: "active", ID: parseTestUUID(t, "cccccccc-cccc-cccc-cccc-000000000003")},
	}
	o := NewOutbound(OutboundConfig{Queries: q})
	ctx := context.Background()
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000003")
	payload, _ := json.Marshal(map[string]string{"content": "x"})
	_ = o.enqueue(ctx, q.inst, q.binding, taskID, sourceKindChatDone, "u", 1, payload)
	_ = o.enqueue(ctx, q.inst, q.binding, taskID, sourceKindChatDone, "u", 1, payload)
	if len(q.enqueued) != 2 {
		t.Fatalf("handler attempted %d enqueues", len(q.enqueued))
	}
}

func parseTestUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestOutboundReportsFastPathEnqueue is the other half of the reconcile-path
// alerting signal: a healthy realtime enqueue has to be attributed to the fast
// path, otherwise the two paths cannot be compared.
func TestOutboundReportsFastPathEnqueue(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000007")
	sessionID := parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000007")
	instID := parseTestUUID(t, "cccccccc-cccc-cccc-cccc-000000000007")
	q := &fakeOutboundQueries{
		task:     db.AgentTaskQueue{ID: taskID, ChatSessionID: sessionID},
		ingested: true,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID:  sessionID,
			InstallationID: instID,
			Config:         []byte(`{"target_chat_id":"user1","target_chat_type":1}`),
		},
		inst: db.ChannelInstallation{ID: instID, Status: "active"},
	}
	m := newRecordingMetrics()
	o := NewOutbound(OutboundConfig{Queries: q, Metrics: m})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: util.UUIDToString(sessionID),
		Payload:       protocol.ChatDonePayload{TaskID: util.UUIDToString(taskID), Content: "hello"},
	})
	if got := m.enqueues(enqueuePathFast, sourceKindChatDone); got != 1 {
		t.Fatalf("fast-path enqueues=%d, want 1", got)
	}
	if got := m.enqueues(enqueuePathReconcile, sourceKindChatDone); got != 0 {
		t.Fatalf("reconcile-path enqueues=%d, want 0", got)
	}
}

// TestOutboundDoesNotCountDuplicateEnqueue: a business-key conflict means the
// reply was already queued, so counting it would inflate the fast-path side of
// the ratio and mask a reconciler rescue.
func TestOutboundDoesNotCountDuplicateEnqueue(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000008")
	sessionID := parseTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-000000000008")
	instID := parseTestUUID(t, "cccccccc-cccc-cccc-cccc-000000000008")
	q := &fakeOutboundQueries{
		task:     db.AgentTaskQueue{ID: taskID, ChatSessionID: sessionID},
		ingested: true,
		binding: db.ChannelChatSessionBinding{
			ChatSessionID:  sessionID,
			InstallationID: instID,
			Config:         []byte(`{"target_chat_id":"user1","target_chat_type":1}`),
		},
		inst:   db.ChannelInstallation{ID: instID, Status: "active"},
		enqErr: pgx.ErrNoRows,
	}
	m := newRecordingMetrics()
	o := NewOutbound(OutboundConfig{Queries: q, Metrics: m})
	bus := events.New()
	o.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: util.UUIDToString(sessionID),
		Payload:       protocol.ChatDonePayload{TaskID: util.UUIDToString(taskID), Content: "hello"},
	})
	if got := m.enqueues(enqueuePathFast, sourceKindChatDone); got != 0 {
		t.Fatalf("fast-path enqueues=%d, want 0 for a duplicate", got)
	}
}
