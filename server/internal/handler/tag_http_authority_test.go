package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTagAuthorityConfigurationClosesNativeBrowserIdentityIngress(t *testing.T) {
	h := &Handler{TagAuthorityEnabled: true}
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"send code":   h.SendCode,
		"verify code": h.VerifyCode,
		"google":      h.GoogleLogin,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			call(response, httptest.NewRequest(http.MethodPost, "/auth/native", nil))
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}
	if _, _, err := h.findOrCreateUser(context.Background(), "profile@example.test"); err != ErrVIBESAuthorityRequired {
		t.Fatalf("findOrCreateUser error = %v", err)
	}
}

func TestCanManageAgentUsesGateRoleFromRequestContext(t *testing.T) {
	userID := "11111111-1111-1111-1111-111111111111"
	workspaceID := "22222222-2222-2222-2222-222222222222"
	otherOwnerID := "33333333-3333-3333-3333-333333333333"
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ownerUUID, err := util.ParseUUID(otherOwnerID)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/agents/agent-1", nil)
	request.Header.Set("X-User-ID", userID)
	request = request.WithContext(middleware.SetMemberContext(request.Context(), workspaceID, db.Member{
		UserID: userUUID, WorkspaceID: workspaceUUID, Role: "member",
	}))
	response := httptest.NewRecorder()
	h := &Handler{}
	if h.canManageAgent(response, request, db.Agent{WorkspaceID: workspaceUUID, OwnerID: ownerUUID}) {
		t.Fatal("fresh Gate member role inherited stale native admin authority")
	}
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
}
