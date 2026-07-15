package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// WorkflowCompletionGate is implemented entirely by cerebro/workflows.
// This upstream seam carries no policy logic.
type WorkflowCompletionGate interface {
	BeforeComplete(context.Context, pgtype.UUID, []byte) ([]byte, error)
}
