package aiimpact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestOperatingLoopHTTPSeamAllowsOwnerAdminCreateAndKeepsMemberReadOnly(t *testing.T) {
	store := &recordingObservationStore{}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	workspaceID := uuid.New()
	actorID := uuid.New()
	functionID := uuid.New()
	body := `{
		"function_id":"` + functionID.String() + `",
		"name":"Resolve customer need",
		"description":"Resolve tracking and order status without manual work"
	}`

	request := func(role string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/cerebro/ai-impact/operating-loops", strings.NewReader(body))
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
				t.Fatalf("%s create operating loop status = %d, want 201: %s", role, createdRecorder.Code, createdRecorder.Body.String())
			}
			var created struct {
				ID          string `json:"id"`
				FunctionID  string `json:"function_id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Active      bool   `json:"active"`
			}
			if err := json.NewDecoder(createdRecorder.Body).Decode(&created); err != nil {
				t.Fatalf("decode create operating loop response: %v", err)
			}
			if created.ID == "" || created.FunctionID != functionID.String() ||
				created.Name != "Resolve customer need" ||
				created.Description != "Resolve tracking and order status without manual work" || !created.Active {
				t.Fatalf("create operating loop response = %+v", created)
			}
		})
	}

	readOnlyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readOnlyRecorder, request("member"))
	if readOnlyRecorder.Code != http.StatusForbidden {
		t.Fatalf("member create operating loop status = %d, want 403: %s", readOnlyRecorder.Code, readOnlyRecorder.Body.String())
	}
}

func TestMetricHTTPSeamAllowsOwnerAdminCreateAndKeepsMemberReadOnly(t *testing.T) {
	store := &recordingObservationStore{}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	workspaceID := uuid.New()
	actorID := uuid.New()
	operatingLoopID := uuid.New()
	body := `{
		"operating_loop_id":"` + operatingLoopID.String() + `",
		"name":"Needs solved",
		"family":"Outcome",
		"unit":"needs",
		"direction":"increase",
		"baseline_start":"2026-07-01T00:00:00Z",
		"baseline_end":"2026-07-08T00:00:00Z",
		"source":"support",
		"guardrail":false
	}`

	request := func(role string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/cerebro/ai-impact/metrics", strings.NewReader(body))
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
				t.Fatalf("%s create metric status = %d, want 201: %s", role, createdRecorder.Code, createdRecorder.Body.String())
			}
			var created struct {
				ID              string          `json:"id"`
				OperatingLoopID string          `json:"operating_loop_id"`
				Name            string          `json:"name"`
				Family          MetricFamily    `json:"family"`
				Unit            string          `json:"unit"`
				Direction       MetricDirection `json:"direction"`
				BaselineStart   time.Time       `json:"baseline_start"`
				BaselineEnd     time.Time       `json:"baseline_end"`
				Source          string          `json:"source"`
				Guardrail       bool            `json:"guardrail"`
				Active          bool            `json:"active"`
			}
			if err := json.NewDecoder(createdRecorder.Body).Decode(&created); err != nil {
				t.Fatalf("decode create metric response: %v", err)
			}
			if created.ID == "" || created.OperatingLoopID != operatingLoopID.String() ||
				created.Name != "Needs solved" || created.Family != FamilyOutcome || created.Unit != "needs" ||
				created.Direction != DirectionIncrease ||
				!created.BaselineStart.Equal(time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)) ||
				!created.BaselineEnd.Equal(time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC)) ||
				created.Source != "support" || created.Guardrail || !created.Active {
				t.Fatalf("create metric response = %+v", created)
			}
		})
	}

	readOnlyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readOnlyRecorder, request("member"))
	if readOnlyRecorder.Code != http.StatusForbidden {
		t.Fatalf("member create metric status = %d, want 403: %s", readOnlyRecorder.Code, readOnlyRecorder.Body.String())
	}
}
