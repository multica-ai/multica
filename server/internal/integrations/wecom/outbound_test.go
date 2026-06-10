package wecom

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeWecomPatcherQueries struct {
	mu            sync.Mutex
	binding       db.WecomChatSessionBinding
	bindingErr    error
	installation  db.WecomInstallation
	installErr    error
	stream        db.WecomOutboundStream
	streamErr     error
	statusUpdates []db.UpdateWecomOutboundStreamStatusParams
}

func (f *fakeWecomPatcherQueries) GetWecomInstallation(ctx context.Context, id pgtype.UUID) (db.WecomInstallation, error) {
	return f.installation, f.installErr
}

func (f *fakeWecomPatcherQueries) GetWecomChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.WecomChatSessionBinding, error) {
	return f.binding, f.bindingErr
}

func (f *fakeWecomPatcherQueries) GetWecomOutboundStreamByTask(ctx context.Context, taskID pgtype.UUID) (db.WecomOutboundStream, error) {
	return f.stream, f.streamErr
}

func (f *fakeWecomPatcherQueries) GetWecomOutboundStreamByChatSession(ctx context.Context, chatSessionID pgtype.UUID) (db.WecomOutboundStream, error) {
	return f.stream, f.streamErr
}

func (f *fakeWecomPatcherQueries) UpdateWecomOutboundStreamStatus(ctx context.Context, arg db.UpdateWecomOutboundStreamStatusParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusUpdates = append(f.statusUpdates, arg)
	return nil
}

type fakeStreamSender struct {
	mu    sync.Mutex
	calls []streamSendCall
	err   error
}

type streamSendCall struct {
	instID   pgtype.UUID
	reqID    string
	streamID string
	content  string
	finish   bool
}

func (f *fakeStreamSender) SendStreamReply(_ context.Context, instID pgtype.UUID, reqID, streamID, content string, finish bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, streamSendCall{instID, reqID, streamID, content, finish})
	return f.err
}

func uuidFromString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}

func TestPatcherCompletesStreamOnChatDone(t *testing.T) {
	instID := uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111")
	sessID := uuidFromString(t, "cccccccc-cccc-cccc-cccc-cccccccccccc")
	taskID := uuidFromString(t, "ee333333-ee33-ee33-ee33-eeeeeeeeeeee")
	streamRowID := uuidFromString(t, "dddddddd-dddd-dddd-dddd-dddddddddddd")

	q := &fakeWecomPatcherQueries{
		binding: db.WecomChatSessionBinding{
			ChatSessionID:  sessID,
			InstallationID: instID,
		},
		installation: db.WecomInstallation{
			ID:     instID,
			Status: string(InstallationActive),
		},
		stream: db.WecomOutboundStream{
			ID:             streamRowID,
			InstallationID: instID,
			ChatSessionID:  sessID,
			TaskID:         taskID,
			ReqID:          "req-inbound-1",
			StreamID:       "stream-abc",
			Status:         StreamStatusStreaming,
		},
	}
	sender := &fakeStreamSender{}
	p := NewPatcher(q, sender, PatcherConfig{})

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        "ee333333-ee33-ee33-ee33-eeeeeeeeeeee",
		ChatSessionID: "cccccccc-cccc-cccc-cccc-cccccccccccc",
		Payload: protocol.ChatDonePayload{
			TaskID:        "ee333333-ee33-ee33-ee33-eeeeeeeeeeee",
			ChatSessionID: "cccccccc-cccc-cccc-cccc-cccccccccccc",
			Content:       "1+1 等于 2。",
		},
	})

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.calls) != 1 {
		t.Fatalf("expected one stream send; got %d", len(sender.calls))
	}
	call := sender.calls[0]
	if call.reqID != "req-inbound-1" || call.streamID != "stream-abc" {
		t.Fatalf("unexpected stream ids: req=%q stream=%q", call.reqID, call.streamID)
	}
	if call.content != "1+1 等于 2。" || !call.finish {
		t.Fatalf("unexpected payload: content=%q finish=%v", call.content, call.finish)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.statusUpdates) != 1 || q.statusUpdates[0].Status != StreamStatusFinal {
		t.Fatalf("expected final status update; got %+v", q.statusUpdates)
	}
}

func TestPatcherSkipsWebOnlyChatSession(t *testing.T) {
	q := &fakeWecomPatcherQueries{bindingErr: pgx.ErrNoRows}
	sender := &fakeStreamSender{}
	p := NewPatcher(q, sender, PatcherConfig{})

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        "ee333333-ee33-ee33-ee33-eeeeeeeeeeee",
		ChatSessionID: "cccccccc-cccc-cccc-cccc-cccccccccccc",
		Payload: protocol.ChatDonePayload{
			Content: "hello",
		},
	})

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.calls) != 0 {
		t.Fatalf("web-only session must not produce outbound; got %d sends", len(sender.calls))
	}
}

func TestPatcherSkipsEmptyChatDoneContent(t *testing.T) {
	q := &fakeWecomPatcherQueries{
		binding: db.WecomChatSessionBinding{InstallationID: uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111")},
		installation: db.WecomInstallation{
			ID:     uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111"),
			Status: string(InstallationActive),
		},
		stream: db.WecomOutboundStream{ReqID: "r", StreamID: "s"},
	}
	sender := &fakeStreamSender{}
	p := NewPatcher(q, sender, PatcherConfig{})

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: "cccccccc-cccc-cccc-cccc-cccccccccccc",
		Payload:       protocol.ChatDonePayload{Content: ""},
	})

	time.Sleep(10 * time.Millisecond)
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.calls) != 0 {
		t.Fatalf("empty content must not send; got %d", len(sender.calls))
	}
}
