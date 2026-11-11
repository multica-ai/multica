package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestOutboundRemoteCancellationTidiesProgress(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.launch()
	h.last(1)
	other := review8545Second(h.o)
	e := events.Event{TaskID: h.taskID, ChatSessionID: telegramTestEvent().ChatSessionID, Type: protocol.EventTaskCancelled}
	reply := &terminalReply{event: e, kind: terminalKindClose, settleReason: "cancelled", remnant: other.takeStream(e)}
	result := other.initializeTerminalReply(context.Background(), reply)
	if !result.done || result.err != nil {
		t.Fatalf("cancel did not settle: %+v", result)
	}
	if got := h.last(2); got.method != "deleteMessage" {
		t.Fatalf("cancelled turn left progress in Telegram: %+v", h.sent())
	}
}

func seedReplyFragments(t *testing.T, h *progressHarness) {
	t.Helper()
	fx := testutil.New(testPool, "", "")
	unique := time.Now().UnixNano()
	fx.UserID = fx.User(t, "telegram-progress", fmt.Sprintf("progress-%d@example.test", unique))
	fx.WorkspaceID = fx.Workspace(t, "telegram-progress", fmt.Sprintf("progress-%d", unique))
	agent := fx.Agent(t, "telegram-progress", "")
	fx.Task(t, agent, testutil.Cols{"id": h.taskID, "status": "completed", "completed_at": testutil.Raw("now()")})
	for i, fragment := range []struct{ kind, content string }{
		{"text", "我"}, {"text", "会检查这些文件。"},
		{"tool_use", "PRIVATE_TOOL_INPUT"}, {"tool_result", "PRIVATE_TOOL_OUTPUT"},
		{"thinking", "PRIVATE_REASONING"}, {"error", "PRIVATE_ERROR"},
		{"text", "检查完成。"}, {"tool_use", "PRIVATE_TOOL_INPUT"},
	} {
		fx.Insert(t, "task_message", testutil.Cols{
			"task_id": h.taskID, "seq": i + 1, "type": fragment.kind, "content": fragment.content,
		})
	}
}

func TestOutboundRemoteClosePreservesPersistedProse(t *testing.T) {
	for _, reason := range []string{"cancelled", "empty_reply"} {
		t.Run(reason, func(t *testing.T) {
			h := newProgressHarness(t, channel.ChatTypeP2P)
			seedReplyFragments(t, h)
			h.text("我会检查这些文件。")
			h.tool("Read", map[string]any{"file_path": "private.txt"})
			h.text("检查完成。")
			h.tool("Bash", map[string]any{"command": "true"})
			other := review8545Second(h.o)
			e := telegramTestEvent()
			reply := &terminalReply{event: e, kind: terminalKindClose, settleReason: reason}
			result := other.initializeTerminalReply(context.Background(), reply)
			if !result.done || result.err != nil {
				t.Fatalf("close did not settle: %+v", result)
			}
			got := h.last(5)
			if got.method != "editMessageText" || got.text != "我会检查这些文件。\n\n检查完成。" {
				t.Fatalf("cleanup = %+v", got)
			}
		})
	}
}

func TestOutboundRemoteEmptyReplyDeletesStepsOnlyMessage(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.launch()
	other := review8545Second(h.o)
	reply := &terminalReply{event: telegramTestEvent(), kind: terminalKindClose, settleReason: "empty_reply"}
	result := other.initializeTerminalReply(context.Background(), reply)
	if !result.done || result.err != nil {
		t.Fatalf("empty completion did not settle: %+v", result)
	}
	if got := h.last(2); got.method != "deleteMessage" {
		t.Fatalf("cleanup = %+v", got)
	}
}

func TestOutboundCloseUsesProseReportedToOtherReplicas(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	seedReplyFragments(t, h)
	h.text("我")
	h.tool("Read", map[string]any{"file_path": "private.txt"})
	e := telegramTestEvent()
	reply := &terminalReply{event: e, kind: terminalKindClose, settleReason: "cancelled", remnant: h.o.takeStream(e)}
	result := h.o.initializeTerminalReply(context.Background(), reply)
	if !result.done || result.err != nil {
		t.Fatalf("cancel did not settle: %+v", result)
	}
	if got := h.last(3); got.text != "我会检查这些文件。\n\n检查完成。" {
		t.Fatalf("cleanup lost text reported to another replica: %+v", got)
	}
}

func TestOutboundLateCancellationDoesNotReplaceFinalAnswer(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.launch()
	other := review8545Second(h.o)
	e := telegramTestEvent()
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), other, e); err != nil {
		t.Fatal(err)
	}
	reply := &terminalReply{event: e, kind: terminalKindClose, settleReason: "cancelled", remnant: h.o.takeStream(e)}
	result := h.o.initializeTerminalReply(context.Background(), reply)
	if !result.done || result.err != nil {
		t.Fatalf("late cancel did not settle: %+v", result)
	}
	if got := h.sent(); len(got) != 2 || got[1].text != chatDoneContent(e.Payload) {
		t.Fatalf("late cancellation changed the answer: %+v", got)
	}
}

func TestOutboundLateCancellationDoesNotReplaceNewerAttempt(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	h.launch()
	retryID := "00000000-0000-0000-0000-000000000005"
	seedRetryChain(t, h.taskID, retryID)
	other := review8545Second(h.o)
	other.handleTaskMessage(telegramPartialEvent(retryID, "Newer attempt."))
	e := telegramTestEvent()
	reply := &terminalReply{event: e, kind: terminalKindClose, settleReason: "cancelled", remnant: h.o.takeStream(e)}
	result := h.o.initializeTerminalReply(context.Background(), reply)
	if !result.done || result.err != nil {
		t.Fatalf("late cancel did not settle: %+v", result)
	}
	if got := h.sent(); len(got) != 2 || got[1].text != "Newer attempt." {
		t.Fatalf("late cancellation changed the retry: %+v", got)
	}
}

type retryReplyFragments struct {
	outboundQueries
	failed bool
}

func (q *retryReplyFragments) ListTaskReplyFragments(ctx context.Context, taskID pgtype.UUID) ([]db.ListTaskReplyFragmentsRow, error) {
	if !q.failed {
		q.failed = true
		return nil, errors.New("transient transcript read failure")
	}
	return q.outboundQueries.ListTaskReplyFragments(ctx, taskID)
}

func TestOutboundRemoteCleanupCanRetryAfterTurnIsClosed(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)
	seedReplyFragments(t, h)
	h.launch()
	other := review8545Second(h.o)
	other.q = &retryReplyFragments{outboundQueries: other.q}
	reply := &terminalReply{event: telegramTestEvent(), kind: terminalKindClose, settleReason: "cancelled"}
	first := other.initializeTerminalReply(context.Background(), reply)
	if first.done || first.retryAt.IsZero() {
		t.Fatalf("transient read was not retried: %+v", first)
	}
	second := other.initializeTerminalReply(context.Background(), reply)
	if !second.done || second.err != nil {
		t.Fatalf("cleanup retry failed: %+v", second)
	}
	if got := h.last(2); got.text != "我会检查这些文件。\n\n检查完成。" {
		t.Fatalf("cleanup = %+v", got)
	}
}
