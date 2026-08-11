package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPoolTaskOnlyMentionsRequester(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Pool mention policy", []byte(`{}`))
	runtimeID := handlerTestRuntimeID(t)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent
		SET runtime_id = NULL,
		    runtime_mode = 'pool',
		    runtime_binding_mode = 'pool',
		    comment_mention_policy = 'creator_only_for_non_creator',
		    runtime_requirements = '{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.agent.execute/v1"]}'::jsonb
		WHERE id = $1
	`, agentID); err != nil {
		t.Fatalf("convert Agent to Pool: %v", err)
	}

	issueID := createCommentTriggerPreviewIssue(t, "Pool task mention policy", "agent", agentID)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, started_at,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, session_affinity_state
		) VALUES (
			$1, $2, $3, 'running', now(), 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.agent.execute/v1"]}'::jsonb,
			$4, $5, 'none'
		)
		RETURNING id
	`, agentID, issueID, runtimeID, testWorkspaceID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("create running Pool Task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	tests := []struct {
		name       string
		content    string
		wantStatus int
	}{
		{
			name:       "requester member is allowed",
			content:    "[@Requester](mention://member/" + testUserID + ") completed",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "another member is forbidden",
			content:    "[@Other](mention://member/11111111-1111-4111-8111-111111111111) review",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "agent is forbidden",
			content:    "[@Agent](mention://agent/22222222-2222-4222-8222-222222222222) help",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "squad is forbidden",
			content:    "[@Squad](mention://squad/33333333-3333-4333-8333-333333333333) help",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "all is forbidden",
			content:    "[@all](mention://all/all) review",
			wantStatus: http.StatusForbidden,
		},
	}

	var allowedCommentID string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
				"content": tt.content,
			})
			r = withURLParam(r, "id", issueID)
			r.Header.Set("X-Agent-ID", agentID)
			r.Header.Set("X-Task-ID", taskID)

			testHandler.CreateComment(w, r)
			if w.Code != tt.wantStatus {
				t.Fatalf("CreateComment status = %d, want %d: %s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus == http.StatusCreated {
				var response CommentResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode allowed comment: %v", err)
				}
				allowedCommentID = response.ID
			}
		})
	}

	t.Run("edit cannot add a forbidden mention", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := newRequest("PATCH", "/api/comments/"+allowedCommentID, map[string]any{
			"content": "[@Other](mention://member/11111111-1111-4111-8111-111111111111) review",
		})
		r = withURLParam(r, "commentId", allowedCommentID)
		r.Header.Set("X-Agent-ID", agentID)
		r.Header.Set("X-Task-ID", taskID)

		testHandler.UpdateComment(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("UpdateComment status = %d, want 403: %s", w.Code, w.Body.String())
		}

		var stored string
		if err := testPool.QueryRow(ctx, `SELECT content FROM comment WHERE id = $1`, allowedCommentID).Scan(&stored); err != nil {
			t.Fatalf("load stored comment: %v", err)
		}
		want := "[@Requester](mention://member/" + testUserID + ") completed"
		if stored != want {
			t.Fatalf("forbidden edit changed stored content to %q, want %q", stored, want)
		}
	})
}

func TestRestrictedAssignedIssueOnlyMentionsCreatorFromClient(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Pool client mention policy", []byte(`{}`))
	if _, err := testPool.Exec(ctx, `
		UPDATE agent
		SET runtime_id = $2,
		    runtime_mode = 'local',
		    runtime_binding_mode = 'fixed',
		    comment_mention_policy = 'creator_only_for_non_creator'
		WHERE id = $1
	`, agentID, handlerTestRuntimeID(t)); err != nil {
		t.Fatalf("convert Agent to Pool: %v", err)
	}
	issueID := createCommentTriggerPreviewIssue(t, "Pool client mention policy", "agent", agentID)
	collaboratorID := createWorkspaceMemberUser(t, "Pool collaborator", "pool-collaborator@example.com")

	tests := []struct {
		name       string
		userID     string
		content    string
		wantStatus int
	}{
		{
			name:       "creator may mention any actor",
			userID:     testUserID,
			content:    "[@Pool](mention://squad/33333333-3333-4333-8333-333333333333) start",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "creator may mention another issue",
			userID:     testUserID,
			content:    "see [POO-1](mention://issue/33333333-3333-4333-8333-333333333333)",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "collaborator may mention creator",
			userID:     collaboratorID,
			content:    "[@Creator](mention://member/" + testUserID + ") update",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "collaborator may not mention another actor",
			userID:     collaboratorID,
			content:    "[@Pool](mention://squad/33333333-3333-4333-8333-333333333333) start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "collaborator may not mention another issue",
			userID:     collaboratorID,
			content:    "see [POO-1](mention://issue/33333333-3333-4333-8333-333333333333)",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := newRequestAs(tt.userID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
				"content": tt.content,
			})
			r = withURLParam(r, "id", issueID)

			testHandler.CreateComment(w, r)
			if w.Code != tt.wantStatus {
				t.Fatalf("CreateComment status = %d, want %d: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
