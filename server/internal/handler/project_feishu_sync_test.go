package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
)

func TestListProjectsIncludesFeishuSync(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id
		FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at
		LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Feishu list summary')
		RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create Project: %v", err)
	}

	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config,
			installer_user_id, status
		)
		VALUES (
			$1, $2, 'feishu', '{"bot_name":"List Summary Bot"}',
			$3, 'active'
		)
		RETURNING id`,
		testWorkspaceID, agentID, testUserID,
	).Scan(&installationID); err != nil {
		t.Fatalf("create Feishu installation: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_project_binding (
			workspace_id, project_id, installation_id, channel_type,
			channel_chat_id, channel_chat_name, state,
			created_by_user_id, bound_by_user_id, bound_at
		)
		VALUES (
			$1, $2, $3, 'feishu',
			'oc_list_summary', 'List Summary Group', 'active',
			$4, $4, now()
		)`,
		testWorkspaceID, projectID, installationID, testUserID,
	); err != nil {
		t.Fatalf("create Project binding: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_project_binding WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM project WHERE id = $1`, projectID)
	})

	projectSync, err := lark.NewProjectSyncService(lark.ProjectSyncServiceConfig{
		Pool:    testPool,
		Queries: testHandler.Queries,
		Issues:  testHandler.IssueService,
		Tasks:   testHandler.TaskService,
	})
	if err != nil {
		t.Fatalf("create Project sync service: %v", err)
	}
	h := *testHandler
	h.LarkProjectSync = projectSync

	recorder := httptest.NewRecorder()
	h.ListProjects(
		recorder,
		newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ListProjects: %d %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode Project list: %v", err)
	}
	for _, project := range response.Projects {
		if project.ID != projectID {
			continue
		}
		if project.FeishuSync == nil {
			t.Fatal("Project list omitted feishu_sync for an active binding")
		}
		if project.FeishuSync.InstallationID != installationID {
			t.Fatalf(
				"Project list installation = %q, want %q",
				project.FeishuSync.InstallationID,
				installationID,
			)
		}
		if project.FeishuSync.AgentID != agentID ||
			project.FeishuSync.State != "active" {
			t.Fatalf(
				"Project list sync = agent %q state %q",
				project.FeishuSync.AgentID,
				project.FeishuSync.State,
			)
		}
		return
	}
	t.Fatalf("Project %s not found in ListProjects response", projectID)
}
