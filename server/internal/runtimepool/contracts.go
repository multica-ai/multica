package runtimepool

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	WaitingTaskScanLimit = 64
	RuntimeScanLimit     = 128
	AssignmentBatchLimit = 8
)

type AssignRequest struct {
	WorkspaceID pgtype.UUID
	FocusTaskID pgtype.UUID
	Limit       int
}

type AssignResult struct {
	Assigned        []db.AgentTaskQueue
	PromotedWaiting []db.AgentTaskQueue
}

type LivenessReader interface {
	Available() bool
	IsAliveBatch(context.Context, []string) (map[string]bool, bool)
}
