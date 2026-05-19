package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createSquadPermissionUser(t *testing.T, name, email string) string {
	t.Helper()

	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		t.Fatalf("invalid test email %q", email)
	}
	email = fmt.Sprintf("%s-%d@%s", local, time.Now().UnixNano(), domain)

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add workspace member %s: %v", email, err)
	}
	return userID
}

func createSquadPermissionAgent(t *testing.T, name, visibility, ownerID string) string {
	t.Helper()

	var agentID string
	name = fmt.Sprintf("%s-%s", name, ownerID[:8])
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb,
		        $3, $4, 1, $5, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), visibility, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent %s: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND assignee_type = 'squad' AND title LIKE 'assign squad with % leader'`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE leader_id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createSquadPermissionSquad(t *testing.T, creatorID, leaderID string) string {
	t.Helper()

	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "squad permission "+creatorID[:8]+" "+leaderID[:8], leaderID, creatorID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID
}

func decodeSquadTestError(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response %q: %v", string(body), err)
	}
	return payload.Error
}

func TestSquadAgentSelectionRejectsOtherPrivateAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	creatorID := createSquadPermissionUser(t, "Squad Creator", "squad-creator@multica.test")
	otherOwnerID := createSquadPermissionUser(t, "Other Agent Owner", "squad-other-owner@multica.test")
	ownPrivateAgentID := createSquadPermissionAgent(t, "creator-private-squad-agent", "private", creatorID)
	otherPrivateAgentID := createSquadPermissionAgent(t, "other-private-squad-agent", "private", otherOwnerID)
	workspaceAgentID := createSquadPermissionAgent(t, "workspace-squad-agent", "workspace", otherOwnerID)

	t.Run("create allows own private leader", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequestAs(creatorID, http.MethodPost, "/api/squads?workspace_id="+testWorkspaceID, map[string]any{
			"name":      "own private leader squad",
			"leader_id": ownPrivateAgentID,
		})
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		testHandler.CreateSquad(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateSquad own private leader: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create allows workspace leader", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequestAs(creatorID, http.MethodPost, "/api/squads?workspace_id="+testWorkspaceID, map[string]any{
			"name":      "workspace leader squad",
			"leader_id": workspaceAgentID,
		})
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		testHandler.CreateSquad(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateSquad workspace leader: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create rejects other private leader", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequestAs(creatorID, http.MethodPost, "/api/squads?workspace_id="+testWorkspaceID, map[string]any{
			"name":      "other private leader squad",
			"leader_id": otherPrivateAgentID,
		})
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		testHandler.CreateSquad(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateSquad other private leader: expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if got := decodeSquadTestError(t, w.Body.Bytes()); got != "leader must be one of your agents or a workspace agent" {
			t.Fatalf("CreateSquad error = %q", got)
		}
	})

	squadID := createSquadPermissionSquad(t, creatorID, ownPrivateAgentID)

	t.Run("update leader rejects other private agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequestAs(creatorID, http.MethodPut, "/api/squads/"+squadID+"?workspace_id="+testWorkspaceID, map[string]any{
			"leader_id": otherPrivateAgentID,
		})
		req = withURLParam(req, "id", squadID)
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		testHandler.UpdateSquad(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("UpdateSquad other private leader: expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if got := decodeSquadTestError(t, w.Body.Bytes()); got != "leader must be one of your agents or a workspace agent" {
			t.Fatalf("UpdateSquad error = %q", got)
		}
	})

	t.Run("add member rejects other private agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequestAs(creatorID, http.MethodPost, "/api/squads/"+squadID+"/members?workspace_id="+testWorkspaceID, map[string]any{
			"member_type": "agent",
			"member_id":   otherPrivateAgentID,
		})
		req = withURLParam(req, "id", squadID)
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		testHandler.AddSquadMember(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("AddSquadMember other private agent: expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if got := decodeSquadTestError(t, w.Body.Bytes()); got != "agent must be one of your agents or a workspace agent" {
			t.Fatalf("AddSquadMember error = %q", got)
		}
	})

	t.Run("add member allows workspace agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequestAs(creatorID, http.MethodPost, "/api/squads/"+squadID+"/members?workspace_id="+testWorkspaceID, map[string]any{
			"member_type": "agent",
			"member_id":   workspaceAgentID,
		})
		req = withURLParam(req, "id", squadID)
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		testHandler.AddSquadMember(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("AddSquadMember workspace agent: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestCreateIssueAssignToSquadRejectsUnselectablePrivateLeader(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	creatorID := createSquadPermissionUser(t, "Squad Assign Creator", "squad-assign-creator@multica.test")
	otherOwnerID := createSquadPermissionUser(t, "Squad Assign Other Owner", "squad-assign-other-owner@multica.test")
	otherPrivateAgentID := createSquadPermissionAgent(t, "squad-assign-other-private-agent", "private", otherOwnerID)
	workspaceAgentID := createSquadPermissionAgent(t, "squad-assign-workspace-agent", "workspace", otherOwnerID)
	privateLeaderSquadID := createSquadPermissionSquad(t, otherOwnerID, otherPrivateAgentID)
	workspaceLeaderSquadID := createSquadPermissionSquad(t, creatorID, workspaceAgentID)

	t.Run("rejects squad whose leader is another user's private agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, newRequestAs(creatorID, http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         "assign squad with private leader",
			"status":        "todo",
			"priority":      "medium",
			"assignee_type": "squad",
			"assignee_id":   privateLeaderSquadID,
		}))
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue squad private leader: expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if got := decodeSquadTestError(t, w.Body.Bytes()); got != "squad leader must be one of your agents or a workspace agent" {
			t.Fatalf("CreateIssue squad private leader error = %q", got)
		}
	})

	t.Run("allows squad whose leader is a workspace agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, newRequestAs(creatorID, http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         "assign squad with workspace leader",
			"status":        "todo",
			"priority":      "medium",
			"assignee_type": "squad",
			"assignee_id":   workspaceLeaderSquadID,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue squad workspace leader: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestMemberCanSelectSquadAgent(t *testing.T) {
	userID := "11111111-1111-1111-1111-111111111111"
	otherID := "22222222-2222-2222-2222-222222222222"

	cases := []struct {
		name  string
		agent db.Agent
		want  bool
	}{
		{
			name: "workspace agent owned by another user",
			agent: db.Agent{
				Visibility: "workspace",
				OwnerID:    util.MustParseUUID(otherID),
			},
			want: true,
		},
		{
			name: "own private agent",
			agent: db.Agent{
				Visibility: "private",
				OwnerID:    util.MustParseUUID(userID),
			},
			want: true,
		},
		{
			name: "other private agent",
			agent: db.Agent{
				Visibility: "private",
				OwnerID:    util.MustParseUUID(otherID),
			},
			want: false,
		},
		{
			name: "legacy private agent with no owner",
			agent: db.Agent{
				Visibility: "private",
			},
			want: true,
		},
		{
			name: "archived workspace agent",
			agent: db.Agent{
				Visibility: "workspace",
				OwnerID:    util.MustParseUUID(otherID),
				ArchivedAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), Valid: true},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memberCanSelectSquadAgent(tc.agent, userID); got != tc.want {
				t.Fatalf("memberCanSelectSquadAgent = %v; want %v", got, tc.want)
			}
		})
	}
}
