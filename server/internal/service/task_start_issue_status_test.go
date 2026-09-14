package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskMayAdvanceIssueOnStart(t *testing.T) {
	issueID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	otherID := util.MustParseUUID("00000000-0000-0000-0000-000000000002")
	tests := []struct {
		name string
		task db.AgentTaskQueue
		want bool
	}{
		{
			name: "assignment evidence points at issue",
			task: db.AgentTaskQueue{
				IssueID:              issueID,
				TriggerEvidenceKind:  pgtype.Text{String: string(attribution.EvidenceIssueAssignment), Valid: true},
				TriggerEvidenceRefID: issueID,
			},
			want: true,
		},
		{
			name: "comment task is not authoritative",
			task: db.AgentTaskQueue{
				IssueID:              issueID,
				TriggerEvidenceKind:  pgtype.Text{String: string(attribution.EvidenceComment), Valid: true},
				TriggerEvidenceRefID: issueID,
			},
		},
		{
			name: "mismatched evidence is rejected",
			task: db.AgentTaskQueue{
				IssueID:              issueID,
				TriggerEvidenceKind:  pgtype.Text{String: string(attribution.EvidenceIssueAssignment), Valid: true},
				TriggerEvidenceRefID: otherID,
			},
		},
		{name: "legacy task without evidence", task: db.AgentTaskQueue{IssueID: issueID}},
		{name: "non issue task", task: db.AgentTaskQueue{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskMayAdvanceIssueOnStart(tt.task); got != tt.want {
				t.Fatalf("taskMayAdvanceIssueOnStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStartTaskAdvancesOnlyOwnedAssignmentIssue(t *testing.T) {
	pool := newCancelFinalizePool(t)
	ctx := context.Background()

	tests := []struct {
		name               string
		issueStatus        string
		evidenceKind       string
		changeOwner        bool
		wantStatus         string
		wantStatusEventCnt int
	}{
		{
			name:               "owned assignment starts todo issue",
			issueStatus:        "todo",
			evidenceKind:       string(attribution.EvidenceIssueAssignment),
			wantStatus:         "in_progress",
			wantStatusEventCnt: 1,
		},
		{
			name:         "comment run preserves todo",
			issueStatus:  "todo",
			evidenceKind: string(attribution.EvidenceComment),
			wantStatus:   "todo",
		},
		{
			name:         "stale assignment preserves new owner state",
			issueStatus:  "todo",
			evidenceKind: string(attribution.EvidenceIssueAssignment),
			changeOwner:  true,
			wantStatus:   "todo",
		},
		{
			name:         "owned assignment preserves blocked",
			issueStatus:  "blocked",
			evidenceKind: string(attribution.EvidenceIssueAssignment),
			wantStatus:   "blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seed := dbfx.New(pool, "", "")
			suffix := uuid.NewString()
			userID := seed.User(t, "Task Start User", fmt.Sprintf("task-start-%s@multica.test", suffix))
			workspaceID := seed.Workspace(t, "Task Start", "task-start-"+suffix)
			fx := dbfx.New(pool, workspaceID, userID)
			fx.Member(t, workspaceID, userID, "owner")
			runtimeID := fx.Runtime(t, "task-start-runtime")
			agentID := fx.Agent(t, "Task Start Agent", runtimeID)
			ownerID := agentID
			if tt.changeOwner {
				ownerID = fx.Agent(t, "Replacement Agent", runtimeID)
			}
			issueID := fx.Issue(t, "Task start status", dbfx.Cols{
				"status":        tt.issueStatus,
				"assignee_type": "agent",
				"assignee_id":   ownerID,
			})
			taskID := fx.Task(t, agentID, dbfx.Cols{
				"runtime_id":              runtimeID,
				"issue_id":                issueID,
				"status":                  "dispatched",
				"trigger_evidence_kind":   tt.evidenceKind,
				"trigger_evidence_ref_id": issueID,
			})

			bus := events.New()
			statusEvents := 0
			bus.Subscribe(protocol.EventIssueUpdated, func(events.Event) {
				statusEvents++
			})
			svc := NewTaskService(db.New(pool), pool, nil, bus)
			if _, err := svc.StartTask(ctx, util.MustParseUUID(taskID)); err != nil {
				t.Fatalf("StartTask: %v", err)
			}

			var status string
			fx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
			if status != tt.wantStatus {
				t.Fatalf("issue status = %q, want %q", status, tt.wantStatus)
			}
			if statusEvents != tt.wantStatusEventCnt {
				t.Fatalf("issue status events = %d, want %d", statusEvents, tt.wantStatusEventCnt)
			}
		})
	}
}

func TestStartTaskAdvancesCustomTodoCategory(t *testing.T) {
	pool := newCancelFinalizePool(t)
	ctx := context.Background()
	seed := dbfx.New(pool, "", "")
	suffix := uuid.NewString()
	userID := seed.User(t, "Custom Status User", fmt.Sprintf("task-start-custom-%s@multica.test", suffix))
	workspaceID := seed.Workspace(t, "Custom Status", "task-start-custom-"+suffix)
	fx := dbfx.New(pool, workspaceID, userID)
	fx.Member(t, workspaceID, userID, "owner")
	runtimeID := fx.Runtime(t, "custom-status-runtime")
	agentID := fx.Agent(t, "Custom Status Agent", runtimeID)
	fx.Insert(t, "issue_status", dbfx.Cols{
		"workspace_id": workspaceID,
		"key":          "ready_for_agent",
		"name":         "Ready for agent",
		"category":     "todo",
		"color":        "#808080",
		"position":     50,
	})
	issueID := fx.Issue(t, "Custom todo status", dbfx.Cols{
		"status":        "ready_for_agent",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	taskID := fx.Task(t, agentID, dbfx.Cols{
		"runtime_id":              runtimeID,
		"issue_id":                issueID,
		"status":                  "dispatched",
		"trigger_evidence_kind":   string(attribution.EvidenceIssueAssignment),
		"trigger_evidence_ref_id": issueID,
	})

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	if _, err := svc.StartTask(ctx, util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	var status string
	fx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
	if status != "in_progress" {
		t.Fatalf("issue status = %q, want in_progress", status)
	}
}
