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

func TestFunctionHTTPSeamLetsMemberListWorkspaceFunctions(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	functionOwnerID := uuid.New()
	store := &recordingObservationStore{functions: []Function{{
		ID:          functionID,
		WorkspaceID: workspaceID,
		Name:        "Customer Service",
		Description: "Resolve customer needs",
		OwnerType:   "member",
		OwnerID:     functionOwnerID,
		Active:      true,
	}}}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/functions", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("member list functions status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Functions []struct {
			ID          uuid.UUID `json:"id"`
			Name        string    `json:"name"`
			Description string    `json:"description"`
			OwnerType   string    `json:"owner_type"`
			OwnerID     uuid.UUID `json:"owner_id"`
			Active      bool      `json:"active"`
		} `json:"functions"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode member list functions response: %v", err)
	}
	if len(response.Functions) != 1 || response.Functions[0].ID != functionID ||
		response.Functions[0].Name != "Customer Service" ||
		response.Functions[0].Description != "Resolve customer needs" ||
		response.Functions[0].OwnerType != "member" || response.Functions[0].OwnerID != functionOwnerID ||
		!response.Functions[0].Active {
		t.Fatalf("member list functions response = %+v", response.Functions)
	}
	if store.listFunctionsWorkspaceID != workspaceID {
		t.Fatalf("list functions workspace = %s, want %s", store.listFunctionsWorkspaceID, workspaceID)
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

func TestOperatingLoopHTTPSeamLetsMemberListWorkspaceOperatingLoops(t *testing.T) {
	workspaceID := uuid.New()
	operatingLoopID := uuid.New()
	functionID := uuid.New()
	store := &recordingObservationStore{operatingLoops: []OperatingLoop{{
		ID:          operatingLoopID,
		WorkspaceID: workspaceID,
		FunctionID:  functionID,
		Name:        "Resolve customer need",
		Description: "Resolve tracking and order status without manual work",
		Active:      true,
	}}}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/operating-loops", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("member list operating loops status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		OperatingLoops []struct {
			ID          uuid.UUID `json:"id"`
			FunctionID  uuid.UUID `json:"function_id"`
			Name        string    `json:"name"`
			Description string    `json:"description"`
			Active      bool      `json:"active"`
		} `json:"operating_loops"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode member list operating loops response: %v", err)
	}
	if len(response.OperatingLoops) != 1 || response.OperatingLoops[0].ID != operatingLoopID ||
		response.OperatingLoops[0].FunctionID != functionID ||
		response.OperatingLoops[0].Name != "Resolve customer need" ||
		response.OperatingLoops[0].Description != "Resolve tracking and order status without manual work" ||
		!response.OperatingLoops[0].Active {
		t.Fatalf("member list operating loops response = %+v", response.OperatingLoops)
	}
	if store.listLoopsWorkspaceID != workspaceID {
		t.Fatalf("list operating loops workspace = %s, want %s", store.listLoopsWorkspaceID, workspaceID)
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

func TestMetricHTTPSeamLetsMemberListWorkspaceMetrics(t *testing.T) {
	workspaceID := uuid.New()
	metricID := uuid.New()
	operatingLoopID := uuid.New()
	baselineStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	baselineEnd := time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC)
	store := &recordingObservationStore{metrics: []Metric{{
		ID:              metricID,
		WorkspaceID:     workspaceID,
		OperatingLoopID: operatingLoopID,
		Name:            "Needs solved",
		Family:          FamilyOutcome,
		Unit:            "needs",
		Direction:       DirectionIncrease,
		BaselineStart:   baselineStart,
		BaselineEnd:     baselineEnd,
		Source:          "support",
		Guardrail:       false,
		Active:          true,
	}}}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/metrics", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("member list metrics status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Metrics []struct {
			ID              uuid.UUID       `json:"id"`
			OperatingLoopID uuid.UUID       `json:"operating_loop_id"`
			Name            string          `json:"name"`
			Family          MetricFamily    `json:"family"`
			Unit            string          `json:"unit"`
			Direction       MetricDirection `json:"direction"`
			BaselineStart   time.Time       `json:"baseline_start"`
			BaselineEnd     time.Time       `json:"baseline_end"`
			Source          string          `json:"source"`
			Guardrail       bool            `json:"guardrail"`
			Active          bool            `json:"active"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode member list metrics response: %v", err)
	}
	if len(response.Metrics) != 1 || response.Metrics[0].ID != metricID ||
		response.Metrics[0].OperatingLoopID != operatingLoopID ||
		response.Metrics[0].Name != "Needs solved" || response.Metrics[0].Family != FamilyOutcome ||
		response.Metrics[0].Unit != "needs" || response.Metrics[0].Direction != DirectionIncrease ||
		!response.Metrics[0].BaselineStart.Equal(baselineStart) ||
		!response.Metrics[0].BaselineEnd.Equal(baselineEnd) || response.Metrics[0].Source != "support" ||
		response.Metrics[0].Guardrail || !response.Metrics[0].Active {
		t.Fatalf("member list metrics response = %+v", response.Metrics)
	}
	if store.listMetricsWorkspaceID != workspaceID {
		t.Fatalf("list metrics workspace = %s, want %s", store.listMetricsWorkspaceID, workspaceID)
	}
}

func TestProjectBindingHTTPSeamAllowsOwnerAdminCreateAndKeepsMemberReadOnly(t *testing.T) {
	store := &recordingObservationStore{}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	workspaceID := uuid.New()
	actorID := uuid.New()
	projectID := uuid.New()
	operatingLoopID := uuid.New()
	body := `{
		"project_id":"` + projectID.String() + `",
		"operating_loop_id":"` + operatingLoopID.String() + `"
	}`

	request := func(role string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/cerebro/ai-impact/project-bindings", strings.NewReader(body))
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
				t.Fatalf("%s create project binding status = %d, want 201: %s", role, createdRecorder.Code, createdRecorder.Body.String())
			}
			var created struct {
				ID              string `json:"id"`
				ProjectID       string `json:"project_id"`
				OperatingLoopID string `json:"operating_loop_id"`
				Active          bool   `json:"active"`
			}
			if err := json.NewDecoder(createdRecorder.Body).Decode(&created); err != nil {
				t.Fatalf("decode create project binding response: %v", err)
			}
			if created.ID == "" || created.ProjectID != projectID.String() ||
				created.OperatingLoopID != operatingLoopID.String() || !created.Active {
				t.Fatalf("create project binding response = %+v", created)
			}
		})
	}

	readOnlyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readOnlyRecorder, request("member"))
	if readOnlyRecorder.Code != http.StatusForbidden {
		t.Fatalf("member create project binding status = %d, want 403: %s", readOnlyRecorder.Code, readOnlyRecorder.Body.String())
	}
}
