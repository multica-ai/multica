package middleware

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type taskContextDB struct {
	db.DBTX
	token db.TaskToken
}

func (d taskContextDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return taskContextRow{d.token}
}

type taskContextRow struct{ token db.TaskToken }

func (r taskContextRow) Scan(dest ...any) error {
	values := []any{r.token.ID, r.token.TokenHash, r.token.TaskID, r.token.AgentID, r.token.WorkspaceID, r.token.UserID, r.token.ExpiresAt, r.token.CreatedAt}
	for i, v := range values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

func TestAuthTaskTokenRejectsConflictingExecutionContext(t *testing.T) {
	id := func(s string) pgtype.UUID {
		var u pgtype.UUID
		if err := u.Scan(s); err != nil {
			t.Fatal(err)
		}
		return u
	}
	token := db.TaskToken{TaskID: id("11111111-1111-4111-8111-111111111111"), AgentID: id("22222222-2222-4222-8222-222222222222"), WorkspaceID: id("33333333-3333-4333-8333-333333333333"), UserID: id("44444444-4444-4444-8444-444444444444")}
	for _, field := range []string{"", "X-Task-ID", "X-Agent-ID", "X-Workspace-ID"} {
		t.Run(field, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/issues", nil)
			req.Header.Set("Authorization", "Bearer mat_test")
			if field != "" {
				req.Header.Set(field, "55555555-5555-4555-8555-555555555555")
			}
			called := false
			h := Auth(db.New(taskContextDB{token: token}), nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Header.Get("X-Task-ID") != "11111111-1111-4111-8111-111111111111" {
					t.Error("missing authoritative task identity")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if field == "" {
				if w.Code != 204 || !called {
					t.Fatalf("headerless task auth rejected: %d", w.Code)
				}
			} else if w.Code != 403 || called {
				t.Fatalf("conflicting context reached handler: status=%d called=%v", w.Code, called)
			}
		})
	}
}
