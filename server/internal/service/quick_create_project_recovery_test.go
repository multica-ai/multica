package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestQuickCreateOutcomePreservesOriginalProject(t *testing.T) {
	pool := sharedTestPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	userID := bootstrap.User(t, "project recovery", "project-recovery-"+suffix+"@example.com")
	workspaceID := bootstrap.Workspace(t, "project recovery", "project-recovery-"+suffix)
	fx := testutil.New(pool, workspaceID, userID)
	fx.Member(t, workspaceID, userID, "owner")
	runtimeID := fx.Runtime(t, "synthetic project recovery runtime", testutil.Cols{"owner_id": userID})
	agentID := fx.Agent(t, "synthetic project recovery agent", runtimeID, testutil.Cols{"owner_id": userID})
	projectID := fx.Project(t, "original project")
	t.Cleanup(func() { fx.Exec(t, `DELETE FROM inbox_item WHERE workspace_id=$1`, workspaceID) })
	svc := &TaskService{Queries: db.New(pool), Bus: events.New()}
	for _, outcome := range []string{inboxTypeQuickCreateFailed, inboxTypeQuickCreateUnconfirmed} {
		for _, project := range []string{projectID, ""} {
			t.Run(outcome+"/"+project, func(t *testing.T) {
				taskID := uuid.NewString()
				task := db.AgentTaskQueue{ID: util.MustParseUUID(taskID), AgentID: util.MustParseUUID(agentID)}
				qc := QuickCreateContext{RequesterID: userID, WorkspaceID: workspaceID, Prompt: "original request", ProjectID: project}
				svc.writeQuickCreateOutcomeInbox(context.Background(), task, qc, outcome, "synthetic recovery outcome", "synthetic failure")
				var raw []byte
				fx.QueryRow(t, `SELECT details FROM inbox_item WHERE workspace_id=$1 AND details->>'task_id'=$2`, workspaceID, taskID).Scan(&raw)
				var details map[string]string
				if err := json.Unmarshal(raw, &details); err != nil {
					t.Fatal(err)
				}
				got, present := details["project_id"]
				if !present || got != project {
					t.Fatalf("project context=(%q,present=%t), want %q explicitly; details=%s", got, present, project, raw)
				}
				if details["original_prompt"] != qc.Prompt {
					t.Fatalf("prompt changed: %s", raw)
				}
			})
		}
	}
}
