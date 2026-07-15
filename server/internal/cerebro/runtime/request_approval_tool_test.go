package runtime

import (
	"context"
	"encoding/json"
	"testing"

	googleuuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type requestApprovalFake struct {
	calls  int
	params approvals.IntakeParams
}

func (f *requestApprovalFake) Intake(_ context.Context, p approvals.IntakeParams) (cerebrodb.CerebroApprovalRequest, error) {
	f.calls++
	f.params = p
	id, _ := util.ParseUUID(googleuuid.NewString())
	return cerebrodb.CerebroApprovalRequest{ID: id, WorkspaceID: p.WorkspaceID, Status: approvals.StatusPending}, nil
}

func TestFirtalRequestApprovalToolCreatesPendingWithSurfaceContext(t *testing.T) {
	ids := func() pgtype.UUID { id, _ := util.ParseUUID(googleuuid.NewString()); return id }
	fake := &requestApprovalFake{}
	tctx := ToolContext{
		AgentID:           ids(),
		WorkspaceID:       ids(),
		TaskID:            ids(),
		IssueID:           ids(),
		ChatSessionID:     ids(),
		TriggerCommentID:  ids(),
		Surface:           "chat",
		ApprovalRequester: fake,
	}
	tool := &FirtalRequestApprovalTool{tctx: tctx}

	result, err := tool.Call(context.Background(), map[string]any{
		"capability": "publish_campaign",
		"resource":   "campaign:summer",
		"reason":     "Needs owner review",
		"surface":    "issue",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !json.Valid([]byte(result)) || fake.calls != 1 {
		t.Fatalf("result=%q calls=%d", result, fake.calls)
	}
	for key, want := range map[string]string{
		"task_id": util.UUIDToString(tctx.TaskID), "issue_id": util.UUIDToString(tctx.IssueID),
		"chat_session_id": util.UUIDToString(tctx.ChatSessionID), "trigger_comment_id": util.UUIDToString(tctx.TriggerCommentID),
		"surface": "chat",
	} {
		if got := fake.params.Context[key]; got != want {
			t.Errorf("context[%q] = %#v, want %q", key, got, want)
		}
	}
}

func TestFirtalRequestApprovalToolRejectsMalformedInputWithoutIntake(t *testing.T) {
	fake := &requestApprovalFake{}
	tool := &FirtalRequestApprovalTool{tctx: ToolContext{ApprovalRequester: fake}}
	if _, err := tool.Call(context.Background(), map[string]any{"capability": "", "reason": ""}); err == nil {
		t.Fatal("Call error = nil, want malformed input error")
	}
	if fake.calls != 0 {
		t.Fatalf("approval intake calls = %d, want 0", fake.calls)
	}
}
