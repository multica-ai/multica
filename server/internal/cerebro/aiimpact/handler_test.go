package aiimpact

import (
	"context"
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

func TestObservationHTTPSeamAllowsOwnerWriteAndMemberReadOnly(t *testing.T) {
	store := &recordingObservationStore{}
	handler := NewHandler(NewService(store))
	workspaceID := uuid.New()
	metricID := uuid.New()
	actorID := uuid.New()
	body := `{
		"period_start":"2026-07-01T00:00:00Z",
		"period_end":"2026-07-02T00:00:00Z",
		"value":12,
		"evidence_status":"Measured",
		"confidence":0.9,
		"source":"support",
		"method":"audited count"
	}`

	request := func(method, role string, requestBody *strings.Reader) *http.Request {
		var req *http.Request
		if requestBody == nil {
			req = httptest.NewRequest(method, "/observations/"+metricID.String(), nil)
		} else {
			req = httptest.NewRequest(method, "/observations/"+metricID.String(), requestBody)
		}
		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("metricId", metricID.String())
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext)
		ctx = middleware.SetMemberContext(ctx, workspaceID.String(), db.Member{
			UserID: pgtype.UUID{Bytes: [16]byte(actorID), Valid: true},
			Role:   role,
		})
		return req.WithContext(ctx)
	}

	createdRecorder := httptest.NewRecorder()
	handler.AppendObservation(createdRecorder, request(http.MethodPost, "owner", strings.NewReader(body)))
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("owner append status = %d, want 201: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	var created struct {
		ID       string  `json:"id"`
		MetricID string  `json:"metric_id"`
		Value    float64 `json:"value"`
	}
	if err := json.NewDecoder(createdRecorder.Body).Decode(&created); err != nil {
		t.Fatalf("decode owner append response: %v", err)
	}
	if created.ID == "" || created.MetricID != metricID.String() || created.Value != 12 {
		t.Fatalf("owner append response = %+v", created)
	}

	listRecorder := httptest.NewRecorder()
	handler.ListObservations(listRecorder, request(http.MethodGet, "member", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("member list status = %d, want 200: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listed struct {
		Observations []struct {
			ID string `json:"id"`
		} `json:"observations"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listed); err != nil {
		t.Fatalf("decode member list response: %v", err)
	}
	if len(listed.Observations) != 1 || listed.Observations[0].ID != created.ID {
		t.Fatalf("member list response = %+v, want the owner observation", listed)
	}

	readOnlyRecorder := httptest.NewRecorder()
	handler.AppendObservation(readOnlyRecorder, request(http.MethodPost, "member", strings.NewReader(body)))
	if readOnlyRecorder.Code != http.StatusForbidden {
		t.Fatalf("member append status = %d, want 403: %s", readOnlyRecorder.Code, readOnlyRecorder.Body.String())
	}
}

func TestLatestObservationReadModelDoesNotDoubleCountMetricPeriod(t *testing.T) {
	workspaceID := uuid.New()
	metricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{observations: []Observation{
		{
			ID:             uuid.New(),
			MetricID:       metricID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			Value:          12,
			EvidenceStatus: EvidenceEstimated,
			Confidence:     0.6,
			Source:         "support",
			Method:         "sampled assessment",
			CreatedAt:      periodEnd,
		},
		{
			ID:             uuid.New(),
			MetricID:       metricID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			Value:          15,
			EvidenceStatus: EvidenceMeasured,
			Confidence:     0.9,
			Source:         "support",
			Method:         "audited count",
			CreatedAt:      periodEnd.Add(time.Hour),
		},
	}}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cerebro/ai-impact/metrics/"+metricID.String()+"/latest-observations",
		nil,
	)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("latest observation read model status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Observations []struct {
			Value          float64        `json:"value"`
			EvidenceStatus EvidenceStatus `json:"evidence_status"`
			Source         string         `json:"source"`
		} `json:"observations"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode latest observation read model: %v", err)
	}
	if len(response.Observations) != 1 || response.Observations[0].Value != 15 ||
		response.Observations[0].EvidenceStatus != EvidenceMeasured || response.Observations[0].Source != "support" {
		t.Fatalf("latest observation read model = %+v, want only the newest measured value", response.Observations)
	}
}

func TestWorkspaceLatestObservationReadModelReturnsLatestAcrossMetrics(t *testing.T) {
	workspaceID := uuid.New()
	firstMetricID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	secondMetricID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{workspaceObservations: []Observation{
		{
			ID:             uuid.New(),
			MetricID:       firstMetricID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			Value:          12,
			EvidenceStatus: EvidenceEstimated,
			Confidence:     0.6,
			Source:         "support",
			Method:         "sampled assessment",
			CreatedAt:      periodEnd,
		},
		{
			ID:             uuid.New(),
			MetricID:       firstMetricID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			Value:          15,
			EvidenceStatus: EvidenceMeasured,
			Confidence:     0.9,
			Source:         "support",
			Method:         "audited count",
			CreatedAt:      periodEnd.Add(time.Hour),
		},
		{
			ID:             uuid.New(),
			MetricID:       secondMetricID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			Value:          4,
			EvidenceStatus: EvidenceMeasured,
			Confidence:     0.8,
			Source:         "finance",
			Method:         "ledger reconciliation",
			CreatedAt:      periodEnd,
		},
	}}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/latest-observations", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("workspace latest observation read model status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Observations []struct {
			MetricID       uuid.UUID      `json:"metric_id"`
			Value          float64        `json:"value"`
			EvidenceStatus EvidenceStatus `json:"evidence_status"`
			Source         string         `json:"source"`
		} `json:"observations"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode workspace latest observation read model: %v", err)
	}
	if len(response.Observations) != 2 ||
		response.Observations[0].MetricID != firstMetricID || response.Observations[0].Value != 15 ||
		response.Observations[0].EvidenceStatus != EvidenceMeasured || response.Observations[0].Source != "support" ||
		response.Observations[1].MetricID != secondMetricID || response.Observations[1].Value != 4 ||
		response.Observations[1].EvidenceStatus != EvidenceMeasured || response.Observations[1].Source != "finance" {
		t.Fatalf("workspace latest observation read model = %+v, want latest values for both metrics", response.Observations)
	}
	if store.listWorkspaceObservationsWorkspaceID != workspaceID {
		t.Fatalf("workspace latest observations workspace = %s, want %s", store.listWorkspaceObservationsWorkspaceID, workspaceID)
	}
}

func TestWorkspaceEvidenceReadModelReturnsLatestObservationWithTaxonomy(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	metricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{{
			ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true,
		}},
		operatingLoops: []OperatingLoop{{
			ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID,
			Name: "Resolve customer needs", Active: true,
		}},
		metrics: []Metric{{
			ID: metricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
			Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs",
			Direction: DirectionIncrease, Source: "support", Active: true,
		}},
		workspaceObservations: []Observation{
			{
				ID: uuid.New(), MetricID: metricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 12, EvidenceStatus: EvidenceEstimated, Confidence: 0.6,
				Source: "support", Method: "sampled assessment", CreatedAt: periodEnd,
			},
			{
				ID: uuid.New(), MetricID: metricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd.Add(time.Hour),
			},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/evidence", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("workspace evidence read model status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			FunctionName      string         `json:"function_name"`
			OperatingLoopName string         `json:"operating_loop_name"`
			MetricName        string         `json:"metric_name"`
			MetricFamily      MetricFamily   `json:"metric_family"`
			PeriodStart       time.Time      `json:"period_start"`
			PeriodEnd         time.Time      `json:"period_end"`
			Value             float64        `json:"value"`
			EvidenceStatus    EvidenceStatus `json:"evidence_status"`
			Confidence        float64        `json:"confidence"`
			Source            string         `json:"source"`
			Method            string         `json:"method"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode workspace evidence read model: %v", err)
	}
	if len(response.Evidence) != 1 {
		t.Fatalf("workspace evidence count = %d, want one latest observation", len(response.Evidence))
	}
	evidence := response.Evidence[0]
	if evidence.FunctionName != "Customer Service" || evidence.OperatingLoopName != "Resolve customer needs" ||
		evidence.MetricName != "Resolved needs" || evidence.MetricFamily != FamilyOutcome ||
		!evidence.PeriodStart.Equal(periodStart) || !evidence.PeriodEnd.Equal(periodEnd) ||
		evidence.Value != 15 || evidence.EvidenceStatus != EvidenceMeasured || evidence.Confidence != 0.9 ||
		evidence.Source != "support" || evidence.Method != "audited count" {
		t.Fatalf("workspace evidence = %+v, want latest observation with full taxonomy", evidence)
	}
	if store.listFunctionsWorkspaceID != workspaceID || store.listLoopsWorkspaceID != workspaceID ||
		store.listMetricsWorkspaceID != workspaceID || store.listWorkspaceObservationsWorkspaceID != workspaceID {
		t.Fatalf("workspace evidence read model did not keep every read workspace-scoped")
	}
}

func TestWorkspaceEvidenceReadModelFiltersByMetricFamilyWithoutDuplicatingEvidence(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	qualityMetricID := uuid.New()
	outcomeMetricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{{
			ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true,
		}},
		operatingLoops: []OperatingLoop{{
			ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID,
			Name: "Resolve customer needs", Active: true,
		}},
		metrics: []Metric{
			{
				ID: qualityMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Reopened needs", Family: FamilyQuality, Unit: "needs",
				Direction: DirectionDecrease, Source: "support", Active: true,
			},
			{
				ID: outcomeMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs",
				Direction: DirectionIncrease, Source: "support", Active: true,
			},
		},
		workspaceObservations: []Observation{
			{
				ID: uuid.New(), MetricID: qualityMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 4, EvidenceStatus: EvidenceEstimated, Confidence: 0.6,
				Source: "support", Method: "sampled review", CreatedAt: periodEnd,
			},
			{
				ID: uuid.New(), MetricID: qualityMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 2, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd.Add(time.Hour),
			},
			{
				ID: uuid.New(), MetricID: outcomeMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd,
			},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/evidence?metric_family=Quality", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("metric family evidence status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			MetricID       uuid.UUID      `json:"metric_id"`
			MetricFamily   MetricFamily   `json:"metric_family"`
			Value          float64        `json:"value"`
			EvidenceStatus EvidenceStatus `json:"evidence_status"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode metric family evidence: %v", err)
	}
	if len(response.Evidence) != 1 || response.Evidence[0].MetricID != qualityMetricID ||
		response.Evidence[0].MetricFamily != FamilyQuality || response.Evidence[0].Value != 2 ||
		response.Evidence[0].EvidenceStatus != EvidenceMeasured {
		t.Fatalf("metric family evidence = %+v, want only the latest Quality evidence", response.Evidence)
	}
}

func TestWorkspaceEvidenceReadModelFiltersByEvidenceStatus(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	measuredMetricID := uuid.New()
	estimatedMetricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{{
			ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true,
		}},
		operatingLoops: []OperatingLoop{{
			ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID,
			Name: "Resolve customer needs", Active: true,
		}},
		metrics: []Metric{
			{
				ID: measuredMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs",
				Direction: DirectionIncrease, Source: "support", Active: true,
			},
			{
				ID: estimatedMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Capacity released", Family: FamilyEconomics, Unit: "hours",
				Direction: DirectionIncrease, Source: "support", Active: true,
			},
		},
		workspaceObservations: []Observation{
			{
				ID: uuid.New(), MetricID: measuredMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd,
			},
			{
				ID: uuid.New(), MetricID: estimatedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 8, EvidenceStatus: EvidenceEstimated, Confidence: 0.6,
				Source: "support", Method: "sampled estimate", CreatedAt: periodEnd,
			},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/evidence?evidence_status=Measured", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("evidence status filter response = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			MetricID       uuid.UUID      `json:"metric_id"`
			EvidenceStatus EvidenceStatus `json:"evidence_status"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode evidence status response: %v", err)
	}
	if len(response.Evidence) != 1 || response.Evidence[0].MetricID != measuredMetricID ||
		response.Evidence[0].EvidenceStatus != EvidenceMeasured {
		t.Fatalf("evidence status response = %+v, want only latest Measured evidence", response.Evidence)
	}
}

func TestWorkspaceEvidenceReadModelCombinesMetricFamilyAndEvidenceStatusFilters(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	qualityMeasuredMetricID := uuid.New()
	qualityEstimatedMetricID := uuid.New()
	outcomeMeasuredMetricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{{
			ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true,
		}},
		operatingLoops: []OperatingLoop{{
			ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID,
			Name: "Resolve customer needs", Active: true,
		}},
		metrics: []Metric{
			{
				ID: qualityMeasuredMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Reopened needs", Family: FamilyQuality, Unit: "needs",
				Direction: DirectionDecrease, Source: "support", Active: true,
			},
			{
				ID: qualityEstimatedMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Frustration-free", Family: FamilyQuality, Unit: "percent",
				Direction: DirectionIncrease, Source: "support", Active: true,
			},
			{
				ID: outcomeMeasuredMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID,
				Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs",
				Direction: DirectionIncrease, Source: "support", Active: true,
			},
		},
		workspaceObservations: []Observation{
			{
				ID: uuid.New(), MetricID: qualityMeasuredMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 2, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd,
			},
			{
				ID: uuid.New(), MetricID: qualityEstimatedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 92, EvidenceStatus: EvidenceEstimated, Confidence: 0.6,
				Source: "support", Method: "sampled assessment", CreatedAt: periodEnd,
			},
			{
				ID: uuid.New(), MetricID: outcomeMeasuredMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd,
			},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/evidence?metric_family=Quality&evidence_status=Measured", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("combined evidence filters response = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			MetricID       uuid.UUID      `json:"metric_id"`
			MetricFamily   MetricFamily   `json:"metric_family"`
			EvidenceStatus EvidenceStatus `json:"evidence_status"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode combined evidence filters response: %v", err)
	}
	if len(response.Evidence) != 1 || response.Evidence[0].MetricID != qualityMeasuredMetricID ||
		response.Evidence[0].MetricFamily != FamilyQuality || response.Evidence[0].EvidenceStatus != EvidenceMeasured {
		t.Fatalf("combined evidence filters response = %+v, want only latest measured Quality evidence", response.Evidence)
	}
}

func TestWorkspaceEvidenceReadModelRejectsUnknownFilters(t *testing.T) {
	workspaceID := uuid.New()
	handler := NewHandler(NewService(&recordingObservationStore{}))
	router := chi.NewRouter()
	handler.Mount(router)

	for _, path := range []string{
		"/api/cerebro/ai-impact/evidence?metric_family=Revenue",
		"/api/cerebro/ai-impact/evidence?evidence_status=Verified",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
				UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
				Role:   "member",
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req.WithContext(ctx))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("unknown evidence filter response = %d, want 400: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestWorkspaceEvidenceReadModelFiltersBeforeSelectingLatestObservation(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	metricID := uuid.New()
	windowStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(48 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{
			{ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true},
		},
		operatingLoops: []OperatingLoop{
			{ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID, Name: "Resolve customer needs", Active: true},
		},
		metrics: []Metric{
			{ID: metricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID, Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs", Direction: DirectionIncrease, Source: "support", Active: true},
		},
		workspaceObservations: []Observation{
			{
				ID: uuid.New(), MetricID: metricID, PeriodStart: windowStart, PeriodEnd: windowEnd,
				Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: windowEnd,
			},
			{
				ID: uuid.New(), MetricID: metricID, PeriodStart: windowEnd.Add(24 * time.Hour), PeriodEnd: windowEnd.Add(48 * time.Hour),
				Value: 20, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: windowEnd.Add(48 * time.Hour),
			},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	path := "/api/cerebro/ai-impact/evidence?period_start=" + windowStart.Format(time.RFC3339) +
		"&period_end=" + windowEnd.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("period-filtered evidence response = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			MetricID    uuid.UUID `json:"metric_id"`
			PeriodStart time.Time `json:"period_start"`
			PeriodEnd   time.Time `json:"period_end"`
			Value       float64   `json:"value"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode period-filtered evidence response: %v", err)
	}
	if len(response.Evidence) != 1 || response.Evidence[0].MetricID != metricID ||
		!response.Evidence[0].PeriodStart.Equal(windowStart) || !response.Evidence[0].PeriodEnd.Equal(windowEnd) ||
		response.Evidence[0].Value != 15 {
		t.Fatalf("period-filtered evidence response = %+v, want latest observation inside requested period", response.Evidence)
	}
}

func TestWorkspaceEvidenceReadModelFiltersSourceBeforeSelectingLatestObservation(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	metricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{
			{ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true},
		},
		operatingLoops: []OperatingLoop{
			{ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID, Name: "Resolve customer needs", Active: true},
		},
		metrics: []Metric{
			{ID: metricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID, Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs", Direction: DirectionIncrease, Source: "support", Active: true},
		},
		workspaceObservations: []Observation{
			{
				ID: uuid.New(), MetricID: metricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "support", Method: "audited count", CreatedAt: periodEnd,
			},
			{
				ID: uuid.New(), MetricID: metricID, PeriodStart: periodStart, PeriodEnd: periodEnd,
				Value: 20, EvidenceStatus: EvidenceMeasured, Confidence: 0.9,
				Source: "ledger", Method: "reconciliation", CreatedAt: periodEnd.Add(time.Hour),
			},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/evidence?source=support", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("source-filtered evidence response = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			MetricID uuid.UUID `json:"metric_id"`
			Value    float64   `json:"value"`
			Source   string    `json:"source"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode source-filtered evidence response: %v", err)
	}
	if len(response.Evidence) != 1 || response.Evidence[0].MetricID != metricID ||
		response.Evidence[0].Value != 15 || response.Evidence[0].Source != "support" {
		t.Fatalf("source-filtered evidence response = %+v, want latest support observation", response.Evidence)
	}
}

func TestFunctionEvidenceReadModelReturnsOnlyLatestEvidenceForRequestedFunction(t *testing.T) {
	workspaceID := uuid.New()
	requestedFunctionID := uuid.New()
	otherFunctionID := uuid.New()
	requestedLoopID := uuid.New()
	otherLoopID := uuid.New()
	requestedMetricID := uuid.New()
	otherMetricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{
			{ID: requestedFunctionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true},
			{ID: otherFunctionID, WorkspaceID: workspaceID, Name: "Finance", Active: true},
		},
		operatingLoops: []OperatingLoop{
			{ID: requestedLoopID, WorkspaceID: workspaceID, FunctionID: requestedFunctionID, Name: "Resolve customer needs", Active: true},
			{ID: otherLoopID, WorkspaceID: workspaceID, FunctionID: otherFunctionID, Name: "Close the books", Active: true},
		},
		metrics: []Metric{
			{ID: requestedMetricID, WorkspaceID: workspaceID, OperatingLoopID: requestedLoopID, Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs", Direction: DirectionIncrease, Source: "support", Active: true},
			{ID: otherMetricID, WorkspaceID: workspaceID, OperatingLoopID: otherLoopID, Name: "Close duration", Family: FamilyOutput, Unit: "hours", Direction: DirectionDecrease, Source: "ledger", Active: true},
		},
		workspaceObservations: []Observation{
			{ID: uuid.New(), MetricID: requestedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 12, EvidenceStatus: EvidenceEstimated, Confidence: 0.6, Source: "support", Method: "sampled assessment", CreatedAt: periodEnd},
			{ID: uuid.New(), MetricID: requestedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9, Source: "support", Method: "audited count", CreatedAt: periodEnd.Add(time.Hour)},
			{ID: uuid.New(), MetricID: otherMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 8, EvidenceStatus: EvidenceMeasured, Confidence: 0.8, Source: "ledger", Method: "reconciliation", CreatedAt: periodEnd},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/functions/"+requestedFunctionID.String()+"/evidence", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("function evidence read model status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			FunctionID      uuid.UUID      `json:"function_id"`
			OperatingLoopID uuid.UUID      `json:"operating_loop_id"`
			MetricID        uuid.UUID      `json:"metric_id"`
			Value           float64        `json:"value"`
			EvidenceStatus  EvidenceStatus `json:"evidence_status"`
			Source          string         `json:"source"`
			Method          string         `json:"method"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode function evidence read model: %v", err)
	}
	if len(response.Evidence) != 1 {
		t.Fatalf("function evidence count = %d, want one latest observation", len(response.Evidence))
	}
	evidence := response.Evidence[0]
	if evidence.FunctionID != requestedFunctionID || evidence.OperatingLoopID != requestedLoopID ||
		evidence.MetricID != requestedMetricID || evidence.Value != 15 ||
		evidence.EvidenceStatus != EvidenceMeasured || evidence.Source != "support" || evidence.Method != "audited count" {
		t.Fatalf("function evidence = %+v, want only the requested function's latest measured observation", evidence)
	}
}

func TestOperatingLoopEvidenceReadModelReturnsOnlyLatestEvidenceForRequestedLoop(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	requestedLoopID := uuid.New()
	otherLoopID := uuid.New()
	requestedMetricID := uuid.New()
	otherMetricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{
			{ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true},
		},
		operatingLoops: []OperatingLoop{
			{ID: requestedLoopID, WorkspaceID: workspaceID, FunctionID: functionID, Name: "Resolve customer needs", Active: true},
			{ID: otherLoopID, WorkspaceID: workspaceID, FunctionID: functionID, Name: "Review escalations", Active: true},
		},
		metrics: []Metric{
			{ID: requestedMetricID, WorkspaceID: workspaceID, OperatingLoopID: requestedLoopID, Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs", Direction: DirectionIncrease, Source: "support", Active: true},
			{ID: otherMetricID, WorkspaceID: workspaceID, OperatingLoopID: otherLoopID, Name: "Reviewed escalations", Family: FamilyOutput, Unit: "cases", Direction: DirectionIncrease, Source: "support", Active: true},
		},
		workspaceObservations: []Observation{
			{ID: uuid.New(), MetricID: requestedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 12, EvidenceStatus: EvidenceEstimated, Confidence: 0.6, Source: "support", Method: "sampled assessment", CreatedAt: periodEnd},
			{ID: uuid.New(), MetricID: requestedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9, Source: "support", Method: "audited count", CreatedAt: periodEnd.Add(time.Hour)},
			{ID: uuid.New(), MetricID: otherMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 8, EvidenceStatus: EvidenceMeasured, Confidence: 0.8, Source: "support", Method: "review log", CreatedAt: periodEnd},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/operating-loops/"+requestedLoopID.String()+"/evidence", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("operating loop evidence read model status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			FunctionID      uuid.UUID      `json:"function_id"`
			OperatingLoopID uuid.UUID      `json:"operating_loop_id"`
			MetricID        uuid.UUID      `json:"metric_id"`
			Value           float64        `json:"value"`
			EvidenceStatus  EvidenceStatus `json:"evidence_status"`
			Source          string         `json:"source"`
			Method          string         `json:"method"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode operating loop evidence read model: %v", err)
	}
	if len(response.Evidence) != 1 {
		t.Fatalf("operating loop evidence count = %d, want one latest observation", len(response.Evidence))
	}
	evidence := response.Evidence[0]
	if evidence.FunctionID != functionID || evidence.OperatingLoopID != requestedLoopID ||
		evidence.MetricID != requestedMetricID || evidence.Value != 15 ||
		evidence.EvidenceStatus != EvidenceMeasured || evidence.Source != "support" || evidence.Method != "audited count" {
		t.Fatalf("operating loop evidence = %+v, want only the requested loop's latest measured observation", evidence)
	}
}

func TestMetricEvidenceReadModelReturnsOnlyLatestEvidenceForRequestedMetric(t *testing.T) {
	workspaceID := uuid.New()
	functionID := uuid.New()
	operatingLoopID := uuid.New()
	requestedMetricID := uuid.New()
	otherMetricID := uuid.New()
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	store := &recordingObservationStore{
		functions: []Function{
			{ID: functionID, WorkspaceID: workspaceID, Name: "Customer Service", Active: true},
		},
		operatingLoops: []OperatingLoop{
			{ID: operatingLoopID, WorkspaceID: workspaceID, FunctionID: functionID, Name: "Resolve customer needs", Active: true},
		},
		metrics: []Metric{
			{ID: requestedMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID, Name: "Resolved needs", Family: FamilyOutcome, Unit: "needs", Direction: DirectionIncrease, Source: "support", Active: true},
			{ID: otherMetricID, WorkspaceID: workspaceID, OperatingLoopID: operatingLoopID, Name: "Reopened needs", Family: FamilyQuality, Unit: "needs", Direction: DirectionDecrease, Source: "support", Active: true},
		},
		workspaceObservations: []Observation{
			{ID: uuid.New(), MetricID: requestedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 12, EvidenceStatus: EvidenceEstimated, Confidence: 0.6, Source: "support", Method: "sampled assessment", CreatedAt: periodEnd},
			{ID: uuid.New(), MetricID: requestedMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 15, EvidenceStatus: EvidenceMeasured, Confidence: 0.9, Source: "support", Method: "audited count", CreatedAt: periodEnd.Add(time.Hour)},
			{ID: uuid.New(), MetricID: otherMetricID, PeriodStart: periodStart, PeriodEnd: periodEnd, Value: 2, EvidenceStatus: EvidenceMeasured, Confidence: 0.8, Source: "support", Method: "reopen log", CreatedAt: periodEnd},
		},
	}
	handler := NewHandler(NewService(store))
	router := chi.NewRouter()
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/ai-impact/metrics/"+requestedMetricID.String()+"/evidence", nil)
	ctx := middleware.SetMemberContext(req.Context(), workspaceID.String(), db.Member{
		UserID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		Role:   "member",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("metric evidence read model status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Evidence []struct {
			FunctionID      uuid.UUID      `json:"function_id"`
			OperatingLoopID uuid.UUID      `json:"operating_loop_id"`
			MetricID        uuid.UUID      `json:"metric_id"`
			PeriodStart     time.Time      `json:"period_start"`
			PeriodEnd       time.Time      `json:"period_end"`
			Value           float64        `json:"value"`
			EvidenceStatus  EvidenceStatus `json:"evidence_status"`
			Confidence      float64        `json:"confidence"`
			Source          string         `json:"source"`
			Method          string         `json:"method"`
		} `json:"evidence"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode metric evidence read model: %v", err)
	}
	if len(response.Evidence) != 1 {
		t.Fatalf("metric evidence count = %d, want one latest observation", len(response.Evidence))
	}
	evidence := response.Evidence[0]
	if evidence.FunctionID != functionID || evidence.OperatingLoopID != operatingLoopID ||
		evidence.MetricID != requestedMetricID || !evidence.PeriodStart.Equal(periodStart) ||
		!evidence.PeriodEnd.Equal(periodEnd) || evidence.Value != 15 ||
		evidence.EvidenceStatus != EvidenceMeasured || evidence.Confidence != 0.9 ||
		evidence.Source != "support" || evidence.Method != "audited count" {
		t.Fatalf("metric evidence = %+v, want only the requested metric's latest measured observation", evidence)
	}
}
