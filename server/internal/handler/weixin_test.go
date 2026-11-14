package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWeixinPublicInstallationOmitsCredentials(t *testing.T) {
	row := db.ChannelInstallation{Config: []byte(`{"app_id":"public-bot","user_id":"public-user","bot_token_encrypted":"private-ciphertext","base_url":"https://ilinkai.weixin.qq.com"}`)}
	raw, err := json.Marshal(weixinInstallationResponse(row))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "public-bot") || !strings.Contains(string(raw), "public-user") {
		t.Fatal("public identity missing")
	}
	for _, secret := range []string{"private-ciphertext", "bot_token", "base_url", "ws_lease"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("public response exposes %s", secret)
		}
	}
}

func TestDeleteWorkspaceRemovesWeixinLoginAndReplyContext(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "WeChat cleanup", "weixin-cleanup")
	dbfx.Member(t, ws, testUserID, "owner")
	agent := dbfx.Insert(t, "agent", testutil.Cols{
		"workspace_id": ws, "name": "WeChat cleanup", "owner_id": testUserID, "runtime_mode": "cloud",
	})
	installation := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": ws, "agent_id": agent, "channel_type": "weixin", "installer_user_id": testUserID,
	})
	dbfx.Insert(t, "weixin_login_session", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": ws, "agent_id": agent, "initiator_user_id": testUserID,
		"state_encrypted": []byte("test-ciphertext"), "expires_at": testutil.Raw("now() + interval '5 minutes'"),
	})
	dbfx.InsertNoID(t, "weixin_account_state", testutil.Cols{
		"installation_id": installation, "bot_id": "cleanup-bot", "user_id": "cleanup-scanner",
		"context_token_encrypted": []byte("test-reply-ciphertext"),
	}, "installation_id=$1", installation)
	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+ws, nil), "id", ws)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	if dbfx.Count(t, "SELECT count(*) FROM weixin_login_session WHERE workspace_id=$1", ws) != 0 || dbfx.Count(t, "SELECT count(*) FROM weixin_account_state WHERE installation_id=$1", installation) != 0 {
		t.Fatal("workspace deletion retained WeChat login or reply credentials")
	}
}
