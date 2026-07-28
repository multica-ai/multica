package workspacecopy

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeMemberLookup struct {
	member db.Member
	err    error
	params db.GetMemberByUserAndWorkspaceParams
}

func (f *fakeMemberLookup) GetMemberByUserAndWorkspace(
	_ context.Context,
	params db.GetMemberByUserAndWorkspaceParams,
) (db.Member, error) {
	f.params = params
	return f.member, f.err
}

const (
	sourceWorkspaceID = "11111111-1111-1111-1111-111111111111"
	targetWorkspaceID = "22222222-2222-2222-2222-222222222222"
)

var copyCallerID = pgtype.UUID{
	Bytes: [16]byte{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x43, 0x33, 0x83, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33},
	Valid: true,
}

func runCopyAuthorizationRequest(t *testing.T, members *fakeMemberLookup) *httptest.ResponseRecorder {
	t.Helper()
	handler := &Handler{Members: members}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/workspaces/"+sourceWorkspaceID+"/cerebro/copy",
		bytes.NewBufferString(`{
			"target_workspace_id":"`+targetWorkspaceID+`",
			"entity_type":"unsupported",
			"source_id":"44444444-4444-4444-8444-444444444444"
		}`),
	)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", sourceWorkspaceID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	request = request.WithContext(middleware.SetMemberContext(
		request.Context(),
		sourceWorkspaceID,
		db.Member{UserID: copyCallerID, Role: "admin"},
	))

	response := httptest.NewRecorder()
	handler.Copy(response, request)
	return response
}

func TestCopyRejectsCallerWithoutTargetWorkspaceAdminRole(t *testing.T) {
	members := &fakeMemberLookup{member: db.Member{Role: "member"}}
	response := runCopyAuthorizationRequest(t, members)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if members.params.UserID != copyCallerID {
		t.Fatalf("target membership user = %v, want %v", members.params.UserID, copyCallerID)
	}
	expectedTarget, err := util.ParseUUID(targetWorkspaceID)
	if err != nil {
		t.Fatalf("parse target workspace fixture: %v", err)
	}
	if got := members.params.WorkspaceID; got != expectedTarget {
		t.Fatalf("target membership workspace = %v, want %s", got, targetWorkspaceID)
	}
}

func TestCopyAllowsTargetWorkspaceOwnerAndAdminRoles(t *testing.T) {
	for _, role := range []string{"owner", "admin"} {
		t.Run(role, func(t *testing.T) {
			response := runCopyAuthorizationRequest(t, &fakeMemberLookup{
				member: db.Member{Role: role},
			})
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestCopyFailsClosedWhenTargetWorkspaceMembershipCannotBeVerified(t *testing.T) {
	tests := []struct {
		name       string
		lookupErr  error
		wantStatus int
	}{
		{name: "not a target member", lookupErr: pgx.ErrNoRows, wantStatus: http.StatusForbidden},
		{name: "membership lookup fails", lookupErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := runCopyAuthorizationRequest(t, &fakeMemberLookup{err: test.lookupErr})
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}
