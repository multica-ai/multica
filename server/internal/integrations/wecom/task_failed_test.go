package wecom

// task_failed_test.go — a failed run reaches the chat that asked, on the same
// route a reply takes; a retry-pending attempt and a web-UI run's failure do
// not; a cancellation sends nothing.
//
// REVERSE VERIFICATION: with the two bus.Subscribe lines for task:failed and
// task:cancelled removed from Register, TestRegister_AFailedRunReachesTheChat
// fails (no frame); with deliverableContent reverted to chatDoneContent, the
// processEvent tests fail (no frame).

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func taskFailedEvent(taskID, errText string, retryPending bool) events.Event {
	payload := map[string]any{
		"task_id":       taskID,
		"status":        "failed",
		"retry_pending": retryPending,
	}
	if errText != "" {
		payload["error"] = errText
	}
	return events.Event{
		Type:          protocol.EventTaskFailed,
		ActorType:     "system",
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		TaskID:        taskID,
		Payload:       payload,
	}
}

func failedRunRig(t *testing.T, origin *bool) (*Outbound, *recordingConn) {
	t.Helper()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: origin,
	}
	q.fileTask(t, "33333333-3333-3333-3333-333333333333")
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	return o, conn
}

func TestProcessEvent_AFailedRunIsReportedToTheChatThatAsked(t *testing.T) {
	t.Parallel()
	o, conn := failedRunRig(t, askedOverWecom())
	err := o.processEvent(context.Background(), taskFailedEvent("33333333-3333-3333-3333-333333333333", "上下文超出模型限制", false))
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "CHAT_1" {
		t.Errorf("chatid = %v, want CHAT_1", body["chatid"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil || md["content"] != "⚠️ 上下文超出模型限制" {
		t.Errorf("content = %v, want the failure text behind the warning mark", body["markdown"])
	}
	if n := conn.frameCount(); n != 1 {
		t.Errorf("%d frames, want exactly one", n)
	}
}

func TestProcessEvent_ARetryPendingFailureSaysNothing(t *testing.T) {
	t.Parallel()
	o, conn := failedRunRig(t, askedOverWecom())
	if err := o.processEvent(context.Background(), taskFailedEvent("33333333-3333-3333-3333-333333333333", "transient", true)); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := conn.frameCount(); n != 0 {
		t.Fatalf("%d frames for an attempt the platform is already retrying, want 0", n)
	}
}

func TestProcessEvent_AFailureWithNoTextSaysNothing(t *testing.T) {
	t.Parallel()
	o, conn := failedRunRig(t, askedOverWecom())
	if err := o.processEvent(context.Background(), taskFailedEvent("33333333-3333-3333-3333-333333333333", "", false)); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := conn.frameCount(); n != 0 {
		t.Fatalf("%d frames for a failure with no text, want 0", n)
	}
}

// The origin gate is the same one the reply path has: a run started from the
// web UI on a session that also lives in a WeCom group must not report its
// failure into the room.
func TestProcessEvent_AWebRunsFailureStaysOutOfTheRoom(t *testing.T) {
	t.Parallel()
	o, conn := failedRunRig(t, askedInTheWebUI())
	if err := o.processEvent(context.Background(), taskFailedEvent("33333333-3333-3333-3333-333333333333", "boom", false)); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := conn.frameCount(); n != 0 {
		t.Fatalf("%d frames for a web-UI run's failure, want 0", n)
	}
}

// The wiring, through a real bus: task:failed is subscribed, task:cancelled
// is subscribed and sends nothing.
func TestRegister_AFailedRunReachesTheChatAndACancelledOneDoesNot(t *testing.T) {
	t.Parallel()
	o, conn := failedRunRig(t, askedOverWecom())
	bus := events.New()
	o.Register(bus)

	bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		TaskID:        "33333333-3333-3333-3333-333333333333",
		Payload:       map[string]any{"task_id": "33333333-3333-3333-3333-333333333333", "status": "cancelled"},
	})
	if n := conn.frameCount(); n != 0 {
		t.Fatalf("%d frames for a cancellation, want 0", n)
	}

	bus.Publish(taskFailedEvent("33333333-3333-3333-3333-333333333333", "boom", false))
	if n := conn.frameCount(); n != 1 {
		t.Fatalf("%d frames after a published task:failed, want 1 — is the bus subscription there?", n)
	}
	md, _ := conn.sendBody(t, 0)["markdown"].(map[string]any)
	if md == nil || md["content"] != "⚠️ boom" {
		t.Errorf("content = %v, want ⚠️ boom", md)
	}
}
