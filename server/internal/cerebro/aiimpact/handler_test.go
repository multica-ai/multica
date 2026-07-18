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
