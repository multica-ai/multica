package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGetMeSessionFailures(t *testing.T) {
	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"missing user", pgx.ErrNoRows, http.StatusUnauthorized},
		{"wrapped missing user", fmt.Errorf("lookup: %w", pgx.ErrNoRows), http.StatusUnauthorized},
		{"database unavailable", errors.New("connection refused"), http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(Config{})
			h.Queries = db.New(&mockDB{getUserErr: tt.err})
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			req.Header.Set("X-User-ID", "00000000-0000-4000-8000-000000000001")
			testutil.Call(t, h.GetMe, req).Want(tt.wantStatus)
		})
	}
}
