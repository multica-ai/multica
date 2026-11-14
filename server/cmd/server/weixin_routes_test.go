package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestWeixinRoutesRequireWorkspaceAdminForQRAndDisconnect(t *testing.T) {
	f := testutil.New(testPool, testWorkspaceID, testUserID)
	user := f.User(t, "WeChat route member", "weixin-route-"+time.Now().Format("150405.000000000")+"@example.test")
	f.Member(t, testWorkspaceID, user, "member")
	token, err := generateTestJWT(user, "weixin-route@example.test", "WeChat route member")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/workspaces/" + testWorkspaceID + "/weixin"
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, base + "/installations", http.StatusOK},
		{http.MethodPost, base + "/logins?agent_id=11111111-1111-4111-8111-111111111111", http.StatusForbidden},
		{http.MethodPost, base + "/logins/11111111-1111-4111-8111-111111111111/poll", http.StatusForbidden},
		{http.MethodDelete, base + "/logins/11111111-1111-4111-8111-111111111111", http.StatusForbidden},
		{http.MethodDelete, base + "/installations/11111111-1111-4111-8111-111111111111", http.StatusForbidden},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := testutil.JSONRequest(tc.method, tc.path, map[string]string{})
			testutil.WithHeaders(req, "Authorization", "Bearer "+token, "X-Workspace-ID", testWorkspaceID)
			testutil.Call(t, testServer.Config.Handler.ServeHTTP, req).Want(tc.want)
		})
	}
}
