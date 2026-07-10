package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// AlertRuntimeFailure creates one actionable inbox card per runtime and failure
// reason. Repeating the same task cannot flood the runtime owner's inbox.
func (s *Service) AlertRuntimeFailure(ctx context.Context, task db.AgentTaskQueue) {
	if s == nil || s.Cerebro == nil || !task.RuntimeID.Valid || !task.FailureReason.Valid {
		return
	}
	rt, err := s.Cerebro.GetRuntimeOwnerForInbox(ctx, task.RuntimeID)
	if err != nil || !rt.OwnerID.Valid {
		return
	}
	runtimeID := util.UUIDToString(task.RuntimeID)
	reason := task.FailureReason.String
	_, err = s.Cerebro.FindRuntimeFailureInboxCard(ctx, cerebrodb.FindRuntimeFailureInboxCardParams{
		WorkspaceID: rt.WorkspaceID,
		RecipientID: rt.OwnerID,
		Column3:     runtimeID,
		Column4:     reason,
	})
	if err == nil {
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("runtime failure alert lookup failed", "runtime_id", runtimeID, "error", err)
		return
	}
	title, body := runtimeFailureAlertCopy(rt.Name, reason)
	details, _ := json.Marshal(map[string]string{"runtime_id": runtimeID, "failure_reason": reason})
	created, err := s.Cerebro.CreateRuntimeFailureInboxCard(ctx, cerebrodb.CreateRuntimeFailureInboxCardParams{
		WorkspaceID: rt.WorkspaceID,
		RecipientID: rt.OwnerID,
		IssueID:     task.IssueID,
		Title:       title,
		Body:        pgtype.Text{String: body, Valid: true},
		Details:     details,
	})
	if err != nil {
		slog.Warn("runtime failure alert creation failed", "runtime_id", runtimeID, "error", err)
		return
	}
	s.publishInboxNew(created)
}

func runtimeFailureAlertCopy(runtimeName, reason string) (string, string) {
	if runtimeName == "" {
		runtimeName = "Runtime"
	}
	detail := "needs attention before this task can run again"
	switch reason {
	case taskfailure.ReasonAgentRuntimeMissingExecutable.String():
		detail = "is missing a required executable. Install it on the runtime, then retry the task"
	case taskfailure.ReasonAgentRuntimeVersionUnsupported.String():
		detail = "has an unsupported runtime version. Upgrade the runtime, then retry the task"
	}
	return fmt.Sprintf("%s requires setup", runtimeName), fmt.Sprintf("This runtime %s.", detail)
}
