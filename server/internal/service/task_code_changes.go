package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/codechanges"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrCodeChangesExecutionStale = errors.New("code changes execution reference is stale")

func prepareTaskCodeChanges(snapshot *codechanges.Snapshot, ref codechanges.ExecutionRef, transactionAvailable bool) ([]byte, error) {
	if snapshot == nil {
		return nil, nil
	}
	if !transactionAvailable {
		return nil, errors.New("code changes require a terminal transaction")
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	return codechanges.Encode(snapshot)
}
func codeChangesExecutionMatches(task db.AgentTaskQueue, ref codechanges.ExecutionRef) bool {
	runtime, err := util.ParseUUID(ref.RuntimeID)
	if err != nil {
		return false
	}
	dispatched, err := time.Parse(time.RFC3339Nano, ref.DispatchedAt)
	return err == nil && task.RuntimeID == runtime && task.DispatchedAt.Valid && task.DispatchedAt.Time.Equal(dispatched)
}
func persistTaskCodeChanges(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, encoded []byte, snapshot *codechanges.Snapshot, ref codechanges.ExecutionRef) error {
	if snapshot == nil {
		return nil
	}
	if !codeChangesExecutionMatches(task, ref) {
		return ErrCodeChangesExecutionStale
	}
	if !task.IssueID.Valid || task.ChatSessionID.Valid {
		return errors.New("code changes require an issue execution")
	}
	runtime, err := util.ParseUUID(ref.RuntimeID)
	if err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, ref.DispatchedAt)
	if err != nil {
		return err
	}
	return q.SetTaskCodeChanges(ctx, db.SetTaskCodeChangesParams{TaskID: task.ID, Snapshot: encoded, SnapshotDigest: codechanges.DigestEncoded(encoded), FileCount: int32(len(snapshot.Files)), CapturedRuntimeID: runtime, CapturedDispatchedAt: pgtype.Timestamptz{Time: at, Valid: true}})
}
func taskCodeChangesReplayMatches(task db.AgentTaskQueue, result []byte) bool {
	var saved, incoming struct {
		Output string `json:"output"`
	}
	return json.Unmarshal(task.Result, &saved) == nil && json.Unmarshal(result, &incoming) == nil && saved.Output == incoming.Output
}
func (s *TaskService) PurgeOrphanedCodeChanges(ctx context.Context) (int64, error) {
	return s.Queries.DeleteOrphanedTaskCodeChanges(ctx)
}
