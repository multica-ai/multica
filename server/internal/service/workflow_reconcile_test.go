package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIsWorkflowReconcileTrigger(t *testing.T) {
	tests := []struct {
		name string
		task db.AgentTaskQueue
		want bool
	}{
		{name: "completed", task: db.AgentTaskQueue{Status: "completed"}, want: true},
		{name: "failed", task: db.AgentTaskQueue{Status: "failed"}, want: true},
		{name: "server refused", task: db.AgentTaskQueue{
			Status:        "cancelled",
			FailureReason: pgtype.Text{String: "runtime_incompatible", Valid: true},
		}, want: true},
		{name: "deliberate cancellation", task: db.AgentTaskQueue{Status: "cancelled"}, want: false},
		{name: "still running", task: db.AgentTaskQueue{Status: "running"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWorkflowReconcileTrigger(tt.task); got != tt.want {
				t.Fatalf("isWorkflowReconcileTrigger() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkflowReconcileNeedsFreshSession(t *testing.T) {
	tests := []struct {
		name   string
		source db.AgentTaskQueue
		want   bool
	}{
		{
			name: "idle watchdog failure",
			source: db.AgentTaskQueue{
				Status:        "failed",
				FailureReason: pgtype.Text{String: "idle_watchdog", Valid: true},
			},
			want: true,
		},
		{
			name: "ordinary failure may resume",
			source: db.AgentTaskQueue{
				Status:        "failed",
				FailureReason: pgtype.Text{String: "agent_error.unknown", Valid: true},
			},
		},
		{
			name: "completed task",
			source: db.AgentTaskQueue{
				Status:        "completed",
				FailureReason: pgtype.Text{String: "idle_watchdog", Valid: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workflowReconcileNeedsFreshSession(tt.source); got != tt.want {
				t.Fatalf("workflowReconcileNeedsFreshSession() = %v, want %v", got, tt.want)
			}
		})
	}
}
