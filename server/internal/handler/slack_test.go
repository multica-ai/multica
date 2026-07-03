package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A successful BYO install must broadcast slack_installation:created so all open
// clients (not just the installer's tab) invalidate the installations query —
// the regression Niko's review caught (RegisterSlackBYO previously only wrote
// the response). Bus.Publish is synchronous, so the subscriber fires inline.
func TestPublishSlackInstallationCreated(t *testing.T) {
	bus := events.New()
	h := &Handler{Bus: bus}

	const (
		wsID   = "11111111-1111-1111-1111-111111111111"
		instID = "22222222-2222-2222-2222-222222222222"
	)

	var got events.Event
	fired := 0
	bus.Subscribe(protocol.EventSlackInstallationCreated, func(e events.Event) {
		got = e
		fired++
	})

	h.publishSlackInstallationCreated(db.ChannelInstallation{
		ID:          parseUUID(instID),
		WorkspaceID: parseUUID(wsID),
	}, "user-1")

	if fired != 1 {
		t.Fatalf("expected slack_installation:created published once, got %d", fired)
	}
	if got.WorkspaceID != wsID || got.ActorType != "user" || got.ActorID != "user-1" {
		t.Errorf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["id"] != instID {
		t.Errorf("payload = %v, want installation id %s", got.Payload, instID)
	}
}

func TestListSlackInstallationsShowsExistingRowsWhenInstallServiceUnavailable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM channel_installation WHERE workspace_id = $1 AND channel_type = 'slack'`, testWorkspaceID); err != nil {
		t.Fatalf("clear slack installations: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1 AND channel_type = 'slack'`, testWorkspaceID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, installer_user_id
		)
		VALUES (
			$1, $2, 'slack',
			'{"app_id":"A-fallback","team_id":"T-fallback","bot_user_id":"U-bot"}'::jsonb,
			$3
		)
	`, testWorkspaceID, agentID, testUserID); err != nil {
		t.Fatalf("insert slack installation: %v", err)
	}

	orig := testHandler.SlackInstall
	testHandler.SlackInstall = nil
	t.Cleanup(func() { testHandler.SlackInstall = orig })

	req := withURLParam(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/slack/installations", nil), "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListSlackInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		Installations    []SlackInstallationResponse `json:"installations"`
		Configured       bool                        `json:"configured"`
		InstallSupported bool                        `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Configured {
		t.Fatal("configured should stay true when Slack installation rows already exist")
	}
	if got.InstallSupported {
		t.Fatal("install_supported should remain false without the Slack install service")
	}
	if len(got.Installations) != 1 {
		t.Fatalf("installations length = %d, want 1", len(got.Installations))
	}
	if got.Installations[0].TeamID != "T-fallback" || got.Installations[0].BotUserID != "U-bot" {
		t.Fatalf("unexpected installation response: %+v", got.Installations[0])
	}
}
