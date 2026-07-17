package aiimpact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestFunctionHTTPSeamAllowsOwnerAdminCreateAndKeepsMemberReadOnly(t *testing.T) {
	store := &recordingObservationStore{}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	workspaceID := uuid.New()
	actorID := uuid.New()
	functionOwnerID := uuid.New()
	body := `{
		"name":"Customer Service",
		"description":"Resolve customer needs",
		"owner_type":"member",
		"owner_id":"` + functionOwnerID.String() + `"
	}`

	request := func(role string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/cerebro/ai-impact/functions", strings.NewReader(body))
		ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
			UserID: pgtype.UUID{Bytes: [16]byte(actorID), Valid: true},
			Role:   role,
		})
		return req.WithContext(ctx)
	}

	for _, role := range []string{"owner", "admin"} {
		t.Run(role, func(t *testing.T) {
			createdRecorder := httptest.NewRecorder()
			router.ServeHTTP(createdRecorder, request(role))
			if createdRecorder.Code != http.StatusCreated {
				t.Fatalf("%s create function status = %d, want 201: %s", role, createdRecorder.Code, createdRecorder.Body.String())
			}
			var created struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				OwnerType   string `json:"owner_type"`
				OwnerID     string `json:"owner_id"`
				Active      bool   `json:"active"`
			}
			if err := json.NewDecoder(createdRecorder.Body).Decode(&created); err != nil {
				t.Fatalf("decode create function response: %v", err)
			}
			if created.ID == "" || created.Name != "Customer Service" || created.Description != "Resolve customer needs" ||
				created.OwnerType != "member" || created.OwnerID != functionOwnerID.String() || !created.Active {
				t.Fatalf("create function response = %+v", created)
			}
		})
	}

	readOnlyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readOnlyRecorder, request("member"))
	if readOnlyRecorder.Code != http.StatusForbidden {
		t.Fatalf("member create function status = %d, want 403: %s", readOnlyRecorder.Code, readOnlyRecorder.Body.String())
	}
}
