package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type claimChannelIdentityFixture struct {
	AgentID        string
	SessionID      string
	RuntimeID      string
	DaemonID       string
	TaskID         string
	InstallationID string
	UserID         string
	ChannelType    string
	ChannelUserID  string
}

func seedClaimChannelIdentityFixture(t *testing.T, ctx context.Context, channelType, userID, channelUserID string) claimChannelIdentityFixture {
	t.Helper()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, channelType+" identity chat")

	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, installer_user_id, status
		)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, channelType, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("seed channel installation: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_chat_session_binding (
			chat_session_id, installation_id, channel_type, channel_chat_id, chat_type
		)
		VALUES ($1, $2, $3, $4, 'group')
	`, sessionID, installationID, channelType, "C-IDENTITY-"+sessionID); err != nil {
		t.Fatalf("seed channel chat binding: %v", err)
	}
	if channelUserID != "" {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_user_binding (
				workspace_id, multica_user_id, installation_id, channel_type, channel_user_id
			)
			VALUES ($1, $2, $3, $4, $5)
		`, testWorkspaceID, userID, installationID, channelType, channelUserID); err != nil {
			t.Fatalf("seed channel user binding: %v", err)
		}
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'identity test message')
	`, sessionID); err != nil {
		t.Fatalf("seed chat message: %v", err)
	}

	session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	task, err := testHandler.TaskService.EnqueueChatTask(ctx, session, parseUUID(userID), false)
	if err != nil {
		t.Fatalf("enqueue channel chat task: %v", err)
	}
	taskID := uuidToString(task.ID)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM channel_user_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	return claimChannelIdentityFixture{
		AgentID:        agentID,
		SessionID:      sessionID,
		RuntimeID:      runtimeID,
		DaemonID:       daemonID,
		TaskID:         taskID,
		InstallationID: installationID,
		UserID:         userID,
		ChannelType:    channelType,
		ChannelUserID:  channelUserID,
	}
}

func claimChannelIdentity(t *testing.T, fixture claimChannelIdentityFixture) (string, *ChannelIdentityData) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+fixture.RuntimeID+"/tasks/claim",
		nil,
		testWorkspaceID,
		fixture.DaemonID,
	)
	req = withURLParam(req, "runtimeId", fixture.RuntimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Task *struct {
			ID              string               `json:"id"`
			ChatChannelType string               `json:"chat_channel_type"`
			ChannelIdentity *ChannelIdentityData `json:"channel_identity"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if response.Task == nil {
		t.Fatalf("expected a claimed task, got %s", w.Body.String())
	}
	if response.Task.ID != fixture.TaskID {
		t.Fatalf("claimed task = %q, want %q", response.Task.ID, fixture.TaskID)
	}
	return response.Task.ChatChannelType, response.Task.ChannelIdentity
}

func TestClaimTaskByRuntime_ChannelIdentity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	for _, tc := range []struct {
		name          string
		channelType   string
		channelUserID string
	}{
		{name: "Feishu open id", channelType: "feishu", channelUserID: "ou_claim_user"},
		{name: "Slack user id", channelType: "slack", channelUserID: "U_CLAIM_USER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := seedClaimChannelIdentityFixture(t, ctx, tc.channelType, testUserID, tc.channelUserID)
			channelType, identity := claimChannelIdentity(t, fixture)

			if channelType != tc.channelType {
				t.Fatalf("chat_channel_type = %q, want %q", channelType, tc.channelType)
			}
			if identity == nil {
				t.Fatal("channel_identity is missing")
			}
			if identity.ChannelType != tc.channelType ||
				identity.InstallationID != fixture.InstallationID ||
				identity.ChannelUserID != tc.channelUserID {
				t.Fatalf("channel_identity = %#v, want type=%q installation=%q user=%q",
					identity, tc.channelType, fixture.InstallationID, tc.channelUserID)
			}
		})
	}
}

func TestClaimTaskByRuntime_ChannelIdentityUsesCurrentSenderNotSessionCreator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var senderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Second Channel Sender', 'second-channel-sender@multica.test')
		RETURNING id
	`).Scan(&senderID); err != nil {
		t.Fatalf("seed second sender: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, senderID); err != nil {
		t.Fatalf("seed second sender membership: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, senderID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, senderID)
	})

	// setupDirectChatSession creates the session as testUserID (the installer),
	// while the queued task is attributed to senderID. The claim must resolve
	// the sender's binding, not the session creator's.
	fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", senderID, "ou_second_sender")
	_, identity := claimChannelIdentity(t, fixture)
	if identity == nil || identity.ChannelUserID != "ou_second_sender" {
		t.Fatalf("channel_identity = %#v, want second sender binding", identity)
	}
}

func TestClaimTaskByRuntime_ChannelIdentityOmittedWhenUnboundOrAmbiguous(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("unbound", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", testUserID, "")
		channelType, identity := claimChannelIdentity(t, fixture)
		if channelType != "feishu" {
			t.Fatalf("chat_channel_type = %q, want feishu", channelType)
		}
		if identity != nil {
			t.Fatalf("channel_identity = %#v, want omitted", identity)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", testUserID, "ou_first")
		if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_user_binding (
				workspace_id, multica_user_id, installation_id, channel_type, channel_user_id
			)
			VALUES ($1, $2, $3, 'feishu', 'ou_second')
		`, testWorkspaceID, testUserID, fixture.InstallationID); err != nil {
			t.Fatalf("seed ambiguous binding: %v", err)
		}
		_, identity := claimChannelIdentity(t, fixture)
		if identity != nil {
			t.Fatalf("channel_identity = %#v, want omitted", identity)
		}
	})
}

func TestClaimTaskByRuntime_ChannelIdentityRevalidatesInstallationAndMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("inactive installation", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", testUserID, "ou_inactive")
		if _, err := testPool.Exec(ctx, `
			UPDATE channel_installation SET status = 'revoked' WHERE id = $1
		`, fixture.InstallationID); err != nil {
			t.Fatalf("revoke installation: %v", err)
		}
		_, identity := claimChannelIdentity(t, fixture)
		if identity != nil {
			t.Fatalf("channel_identity = %#v, want omitted", identity)
		}
	})

	t.Run("agent mismatch", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", testUserID, "ou_wrong_agent")
		if _, err := testPool.Exec(ctx, `
			UPDATE channel_installation
			SET agent_id = '00000000-0000-0000-0000-000000000098'::uuid
			WHERE id = $1
		`, fixture.InstallationID); err != nil {
			t.Fatalf("change installation agent: %v", err)
		}
		_, identity := claimChannelIdentity(t, fixture)
		if identity != nil {
			t.Fatalf("channel_identity = %#v, want omitted", identity)
		}
	})

	t.Run("workspace mismatch", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", testUserID, "ou_wrong_workspace")
		if _, err := testPool.Exec(ctx, `
			UPDATE channel_installation
			SET workspace_id = '00000000-0000-0000-0000-000000000097'::uuid
			WHERE id = $1
		`, fixture.InstallationID); err != nil {
			t.Fatalf("change installation workspace: %v", err)
		}
		_, identity := claimChannelIdentity(t, fixture)
		if identity != nil {
			t.Fatalf("channel_identity = %#v, want omitted", identity)
		}
	})

	t.Run("removed member", func(t *testing.T) {
		ctx := context.Background()
		var userID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO "user" (name, email)
			VALUES ('Removed Channel Member', 'removed-channel-member@multica.test')
			RETURNING id
		`).Scan(&userID); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO member (workspace_id, user_id, role)
			VALUES ($1, $2, 'member')
		`, testWorkspaceID, userID); err != nil {
			t.Fatalf("seed member: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
			testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
		})

		fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", userID, "ou_removed")
		if _, err := testPool.Exec(ctx, `
			DELETE FROM member WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, userID); err != nil {
			t.Fatalf("remove member: %v", err)
		}
		_, identity := claimChannelIdentity(t, fixture)
		if identity != nil {
			t.Fatalf("channel_identity = %#v, want omitted", identity)
		}
	})
}

func TestResolveClaimChannelIdentityRequiresDirectMatchingOriginator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := seedClaimChannelIdentityFixture(t, ctx, "feishu", testUserID, "ou_direct")
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(fixture.TaskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	binding, err := testHandler.Queries.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: parseUUID(fixture.SessionID),
		ChannelType:   "feishu",
	})
	if err != nil {
		t.Fatalf("load channel chat binding: %v", err)
	}

	task.OriginatorSource.String = "delegation"
	if got := testHandler.resolveClaimChannelIdentity(ctx, task, parseUUID(testWorkspaceID), binding); got != nil {
		t.Fatalf("delegated task resolved channel identity: %#v", got)
	}

	task.OriginatorSource.String = "direct_human"
	task.OriginatorUserID = parseUUID("00000000-0000-0000-0000-000000000099")
	if got := testHandler.resolveClaimChannelIdentity(ctx, task, parseUUID(testWorkspaceID), binding); got != nil {
		t.Fatalf("mismatched originator resolved channel identity: %#v", got)
	}
}
