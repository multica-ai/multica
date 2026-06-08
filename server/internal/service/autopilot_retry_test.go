package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestShouldRetryAutopilotTask(t *testing.T) {
	runID := pgtype.UUID{Valid: true}
	runID.Bytes[0] = 1

	tests := []struct {
		name string
		task db.AgentTaskQueue
		want bool
	}{
		{
			name: "retries no-progress semantic inactivity once",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "codex_semantic_inactivity", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: true,
		},
		{
			name: "retries hard timeout once",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "timeout", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: true,
		},
		{
			name: "does not retry after budget exhausted",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "codex_semantic_inactivity", Valid: true},
				Attempt:        2,
				MaxAttempts:    2,
			},
			want: false,
		},
		{
			name: "does not retry agent errors",
			task: db.AgentTaskQueue{
				Status:         "failed",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "agent_error", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: false,
		},
		{
			name: "does not retry cancelled tasks",
			task: db.AgentTaskQueue{
				Status:         "cancelled",
				AutopilotRunID: runID,
				FailureReason:  pgtype.Text{String: "timeout", Valid: true},
				Attempt:        1,
				MaxAttempts:    2,
			},
			want: false,
		},
		{
			name: "does not retry non-autopilot tasks",
			task: db.AgentTaskQueue{
				Status:        "failed",
				FailureReason: pgtype.Text{String: "timeout", Valid: true},
				Attempt:       1,
				MaxAttempts:   2,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetryAutopilotTask(tc.task); got != tc.want {
				t.Fatalf("shouldRetryAutopilotTask() = %v, want %v", got, tc.want)
			}
		})
	}
}
