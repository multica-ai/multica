package evals

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeBlockingGate struct {
	allowed  bool
	called   bool
	gotAdmin bool
}

func (f *fakeBlockingGate) CanSetBlockingGate(_ context.Context, _, _ uuid.UUID, isAdmin bool) (bool, error) {
	f.called = true
	f.gotAdmin = isAdmin
	return f.allowed || isAdmin, nil
}

func bindingBody(workflowID, evalID uuid.UUID, blocking bool) string {
	return fmt.Sprintf(`{"workflow_id":%q,"eval_id":%q,"phase":"delivery","blocking":%t}`,
		workflowID.String(), evalID.String(), blocking)
}

func postBinding(t *testing.T, h *Handler, workspaceID uuid.UUID, member db.Member, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := middleware.SetMemberContext(context.Background(), workspaceID.String(), member)
	req := httptest.NewRequest("POST", "/bindings", strings.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.CreateBinding(rec, req)
	return rec
}

func bindingFixture(t *testing.T) (evalFixture, uuid.UUID, db.Member) {
	t.Helper()
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	evalID := seedActiveEval(t, f, "binding-access", 1)
	member := db.Member{
		WorkspaceID: pgUUID(f.workspaceID),
		UserID:      pgUUID(f.actorID),
		Role:        "member",
	}
	return f, evalID, member
}

func TestCreateBindingDeniesBlockingForNonPermittedMember(t *testing.T) {
	f, evalID, member := bindingFixture(t)
	gate := &fakeBlockingGate{}
	h := NewHandler(evalTestPool).WithBlockingGateAuthorizer(gate)

	rec := postBinding(t, h, f.workspaceID, member, bindingBody(f.workflowID, evalID, true))
	if rec.Code != 403 || !gate.called || gate.gotAdmin {
		t.Fatalf("denied binding: status=%d called=%v admin=%v", rec.Code, gate.called, gate.gotAdmin)
	}
}

func TestCreateBindingAllowsBlockingForAdminOrGrantedMember(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		allowed bool
	}{
		{name: "admin", role: "admin"},
		{name: "granted member", role: "member", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, evalID, member := bindingFixture(t)
			member.Role = tc.role
			gate := &fakeBlockingGate{allowed: tc.allowed}
			h := NewHandler(evalTestPool).WithBlockingGateAuthorizer(gate)

			rec := postBinding(t, h, f.workspaceID, member, bindingBody(f.workflowID, evalID, true))
			if rec.Code != 201 || !gate.called {
				t.Fatalf("allowed binding: status=%d body=%s called=%v", rec.Code, rec.Body.String(), gate.called)
			}
			if (tc.role == "admin") != gate.gotAdmin {
				t.Fatalf("admin flag=%v for role %q", gate.gotAdmin, tc.role)
			}
		})
	}
}

func TestCreateBindingAllowsAdvisoryWithoutBlockingPermission(t *testing.T) {
	f, evalID, member := bindingFixture(t)
	gate := &fakeBlockingGate{}
	h := NewHandler(evalTestPool).WithBlockingGateAuthorizer(gate)

	rec := postBinding(t, h, f.workspaceID, member, bindingBody(f.workflowID, evalID, false))
	if rec.Code != 201 || gate.called {
		t.Fatalf("advisory binding: status=%d body=%s gate_called=%v", rec.Code, rec.Body.String(), gate.called)
	}
}
