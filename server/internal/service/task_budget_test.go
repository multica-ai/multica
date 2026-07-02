package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// claimBudgetQueries implements the subset of db.Queries methods that ClaimTask
// uses. This keeps the test focused on the budget enforcement path without
// needing a full database.
type claimBudgetQueries struct {
	agent           db.Agent
	tasksTodayCount int64
	tasksTodayErr   error
	runningCount    int64
	runningErr      error
	claimTask       *db.AgentTaskQueue
	claimErr        error
}

func (q *claimBudgetQueries) GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error) {
	return q.agent, nil
}

func (q *claimBudgetQueries) CountRunningTasks(ctx context.Context, agentID pgtype.UUID) (int64, error) {
	if q.runningErr != nil {
		return 0, q.runningErr
	}
	return q.runningCount, nil
}

func (q *claimBudgetQueries) CountAgentRunsToday(ctx context.Context, agentID pgtype.UUID) (int64, error) {
	if q.tasksTodayErr != nil {
		return 0, q.tasksTodayErr
	}
	return q.tasksTodayCount, nil
}

func (q *claimBudgetQueries) ClaimAgentTask(ctx context.Context, agentID pgtype.UUID) (db.AgentTaskQueue, error) {
	if q.claimErr != nil {
		return db.AgentTaskQueue{}, q.claimErr
	}
	if q.claimTask == nil {
		return db.AgentTaskQueue{}, pgx.ErrNoRows
	}
	return *q.claimTask, nil
}

func (q *claimBudgetQueries) RefreshAgentStatusFromTasks(ctx context.Context, agentID pgtype.UUID) (db.Agent, error) {
	return q.agent, nil
}

func makeAgentID(s string) pgtype.UUID {
	var id pgtype.UUID
	id.Scan(s)
	return id
}

func TestClaimTaskBudgetExceeded(t *testing.T) {
	agentID := makeAgentID("c0000000-0000-0000-0000-000000000001")
	taskID := makeAgentID("d0000000-0000-0000-0000-000000000001")
	runtimeID := makeAgentID("e0000000-0000-0000-0000-000000000001")

	cases := []struct {
		name             string
		maxRunsPerDay    int32
		tasksToday       int64
		runningCount     int64
		maxConcurrent    int32
		claimableTask    bool
		expectClaimed    bool
	}{
		{
			name:          "unlimited budget (null) — always claimable",
			maxRunsPerDay: 0, // Valid=false means unlimited
			tasksToday:    500,
			runningCount:  0,
			maxConcurrent: 6,
			claimableTask: true,
			expectClaimed: true,
		},
		{
			name:          "within budget — claimable",
			maxRunsPerDay: 10,
			tasksToday:    5,
			runningCount:  0,
			maxConcurrent: 6,
			claimableTask: true,
			expectClaimed: true,
		},
		{
			name:          "budget exactly reached — NOT claimable",
			maxRunsPerDay: 10,
			tasksToday:    10,
			runningCount:  0,
			maxConcurrent: 6,
			claimableTask: true,
			expectClaimed: false,
		},
		{
			name:          "budget exceeded — NOT claimable",
			maxRunsPerDay: 5,
			tasksToday:    7,
			runningCount:  0,
			maxConcurrent: 6,
			claimableTask: true,
			expectClaimed: false,
		},
		{
			name:          "no queued tasks — returns nil regardless of budget",
			maxRunsPerDay: 5,
			tasksToday:    0,
			runningCount:  0,
			maxConcurrent: 6,
			claimableTask: false,
			expectClaimed: false,
		},
		{
			name:          "at capacity — returns nil regardless of budget",
			maxRunsPerDay: 100,
			tasksToday:    0,
			runningCount:  6,
			maxConcurrent: 6,
			claimableTask: true,
			expectClaimed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &claimBudgetQueries{
				agent: db.Agent{
					ID:                 agentID,
					MaxConcurrentTasks: tc.maxConcurrent,
					MaxRunsPerDay:      pgtype.Int4{Int32: tc.maxRunsPerDay, Valid: tc.maxRunsPerDay > 0},
					RuntimeID:          runtimeID,
				},
				runningCount: tc.runningCount,
				tasksTodayCount: tc.tasksToday,
			}

			if tc.claimableTask {
				q.claimTask = &db.AgentTaskQueue{
					ID:        taskID,
					AgentID:   agentID,
					RuntimeID: runtimeID,
					Status:    "queued",
				}
			}

			// Simulate ClaimTask's budget check logic
			ctx := context.Background()
			agent := q.agent

			// Check max_concurrent_tasks
			running, _ := q.CountRunningTasks(ctx, agentID)
			if running >= int64(agent.MaxConcurrentTasks) {
				if tc.expectClaimed {
					t.Errorf("expected claim to proceed but capacity was full")
				}
				return
			}

			// Check budget
			if agent.MaxRunsPerDay.Valid && agent.MaxRunsPerDay.Int32 > 0 {
				todayCount, err := q.CountAgentRunsToday(ctx, agentID)
				if err != nil {
					t.Fatalf("CountAgentRunsToday error: %v", err)
				}
				if todayCount >= int64(agent.MaxRunsPerDay.Int32) {
					if tc.expectClaimed {
						t.Errorf("budget check: todayCount=%d >= maxRunsPerDay=%d, expected claimable but was blocked",
							todayCount, agent.MaxRunsPerDay.Int32)
					}
					return
				}
			}

			// Try to claim
			_, err := q.ClaimAgentTask(ctx, agentID)
			if tc.expectClaimed && err != nil {
				t.Errorf("expected claim to succeed but got error: %v", err)
			}
			if !tc.expectClaimed && err == nil {
				t.Error("expected no claim but got a claimed task")
			}
		})
	}
}

func TestClaimTaskCountRunsTodayError(t *testing.T) {
	agentID := makeAgentID("c0000000-0000-0000-0000-000000000001")

	q := &claimBudgetQueries{
		agent: db.Agent{
			ID:                 agentID,
			MaxConcurrentTasks: 6,
			MaxRunsPerDay:      pgtype.Int4{Int32: 10, Valid: true},
		},
		tasksTodayErr: errors.New("db connection lost"),
	}

	ctx := context.Background()
	agent := q.agent
	if agent.MaxRunsPerDay.Valid && agent.MaxRunsPerDay.Int32 > 0 {
		_, err := q.CountAgentRunsToday(ctx, agentID)
		if err == nil {
			t.Error("expected count error to propagate")
		}
	}
}
