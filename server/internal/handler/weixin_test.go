package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/weixin"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
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

func TestWeixinConflictResponseNamesScopedAgent(t *testing.T) {
	w := httptest.NewRecorder()
	writeWeixinError(w, &weixin.ConflictError{AgentID: "agent-id", AgentName: "Assistant"})
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || body["code"] != "weixin_already_connected" || body["agent_name"] != "Assistant" {
		t.Fatalf("conflict body: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	writeWeixinError(w, weixin.ErrConflict)
	if strings.Contains(w.Body.String(), "agent_id") || strings.Contains(w.Body.String(), "agent_name") {
		t.Fatal("unscoped conflict exposed agent details")
	}
}

func TestWeixinExpiredAuthorizationProjection(t *testing.T) {
	row := db.ChannelInstallation{Status: "revoked", Config: []byte(`{"authorization_expired":true}`)}
	if got := weixinInstallationResponse(row); got.Status != "expired" {
		t.Fatalf("status=%s", got.Status)
	}
	row.Config = []byte(`{}`)
	if got := weixinInstallationResponse(row); got.Status != "revoked" {
		t.Fatal("manual disconnect shown as expired")
	}
}

func TestBeginWeixinLoginRejectsUnrelatedMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, otherMember := privateAgentTestFixture(t)
	box, err := secretbox.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	install, err := weixin.NewInstallService(testHandler.Queries, testPool, box)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Queries: testHandler.Queries, WeixinInstall: install}
	req := newRequestAs(otherMember, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/weixin/logins?agent_id="+agentID, nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testutil.Call(t, h.BeginWeixinLogin, req).Want(http.StatusForbidden)
}
