package evals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func scheduleRequest(t *testing.T, h *Handler, workspaceID, userID uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	member := db.Member{
		WorkspaceID: pgtype.UUID{Bytes: workspaceID, Valid: true},
		UserID:      pgtype.UUID{Bytes: userID, Valid: true},
		Role:        "member",
	}
	ctx := middleware.SetMemberContext(context.Background(), workspaceID.String(), member)
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

func TestEvalScheduleRoutesAndOwnerAccess(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	evalID := seedActiveEval(t, f, "schedule-routes", 1)
	h := NewHandler(evalTestPool)
	path := "/" + evalID.String() + "/schedule"

	created := scheduleRequest(t, h, f.workspaceID, f.actorID, http.MethodPut, path,
		`{"schedule_expr":"0 9 * * 1","timezone":"Europe/Copenhagen","enabled":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("PUT schedule: status=%d body=%s", created.Code, created.Body.String())
	}
	loaded := scheduleRequest(t, h, f.workspaceID, f.actorID, http.MethodGet, path, "")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"schedule_expr":"0 9 * * 1"`) {
		t.Fatalf("GET schedule: status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	denied := scheduleRequest(t, h, f.workspaceID, uuid.New(), http.MethodPut, path,
		`{"schedule_expr":"0 10 * * *","timezone":"Europe/Copenhagen","enabled":true}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-owner PUT schedule: status=%d body=%s", denied.Code, denied.Body.String())
	}
	deleted := scheduleRequest(t, h, f.workspaceID, f.actorID, http.MethodDelete, path, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("DELETE schedule: status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	empty := scheduleRequest(t, h, f.workspaceID, f.actorID, http.MethodGet, path, "")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "null" {
		t.Fatalf("GET empty schedule: status=%d body=%q", empty.Code, empty.Body.String())
	}
}
