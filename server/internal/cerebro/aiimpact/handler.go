package aiimpact

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// Handler exposes the AI Impact Observation HTTP seam.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around the Observation service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type appendObservationRequest struct {
	PeriodStart    time.Time      `json:"period_start"`
	PeriodEnd      time.Time      `json:"period_end"`
	Value          float64        `json:"value"`
	EvidenceStatus EvidenceStatus `json:"evidence_status"`
	Confidence     float64        `json:"confidence"`
	Source         string         `json:"source"`
	Method         string         `json:"method"`
}

type observationResponse struct {
	ID             uuid.UUID      `json:"id"`
	MetricID       uuid.UUID      `json:"metric_id"`
	PeriodStart    time.Time      `json:"period_start"`
	PeriodEnd      time.Time      `json:"period_end"`
	Value          float64        `json:"value"`
	EvidenceStatus EvidenceStatus `json:"evidence_status"`
	Confidence     float64        `json:"confidence"`
	Source         string         `json:"source"`
	Method         string         `json:"method"`
	CreatedAt      time.Time      `json:"created_at"`
}

func toObservationResponse(observation Observation) observationResponse {
	return observationResponse{
		ID:             observation.ID,
		MetricID:       observation.MetricID,
		PeriodStart:    observation.PeriodStart,
		PeriodEnd:      observation.PeriodEnd,
		Value:          observation.Value,
		EvidenceStatus: observation.EvidenceStatus,
		Confidence:     observation.Confidence,
		Source:         observation.Source,
		Method:         observation.Method,
		CreatedAt:      observation.CreatedAt,
	}
}

// AppendObservation appends evidence for one metric.
func (h *Handler) AppendObservation(w http.ResponseWriter, r *http.Request) {
	workspaceID, actorID, role, ok := observationRequestContext(w, r)
	if !ok {
		return
	}
	metricID, ok := observationMetricID(w, r)
	if !ok {
		return
	}

	var request appendObservationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input := ObservationInput{
		MetricID:       metricID,
		PeriodStart:    request.PeriodStart,
		PeriodEnd:      request.PeriodEnd,
		Value:          request.Value,
		EvidenceStatus: request.EvidenceStatus,
		Confidence:     request.Confidence,
		Source:         request.Source,
		Method:         request.Method,
	}
	if err := ValidateObservation(input); err != nil {
		writeObservationError(w, http.StatusBadRequest, err.Error())
		return
	}

	observation, err := h.service.AppendObservation(
		r.Context(), workspaceID, actorID, "member", role, input,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrReadOnly):
			writeObservationError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, ErrNotFound):
			writeObservationError(w, http.StatusNotFound, err.Error())
		default:
			writeObservationError(w, http.StatusInternalServerError, "failed to append observation")
		}
		return
	}
	writeObservationJSON(w, http.StatusCreated, toObservationResponse(observation))
}

// ListObservations returns the append-only evidence history for one metric.
func (h *Handler) ListObservations(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}
	metricID, ok := observationMetricID(w, r)
	if !ok {
		return
	}

	observations, err := h.service.ListObservations(r.Context(), workspaceID, metricID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list observations")
		return
	}
	response := make([]observationResponse, 0, len(observations))
	for _, observation := range observations {
		response = append(response, toObservationResponse(observation))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"observations": response})
}

func observationRequestContext(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, string, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok || !member.UserID.Valid {
		writeObservationError(w, http.StatusUnauthorized, "user not authenticated")
		return uuid.Nil, uuid.Nil, "", false
	}
	workspaceID, err := uuid.Parse(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid workspace_id")
		return uuid.Nil, uuid.Nil, "", false
	}
	actorID, err := uuid.FromBytes(member.UserID.Bytes[:])
	if err != nil {
		writeObservationError(w, http.StatusUnauthorized, "invalid user identity")
		return uuid.Nil, uuid.Nil, "", false
	}
	return workspaceID, actorID, member.Role, true
}

func observationMetricID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	metricID, err := uuid.Parse(chi.URLParam(r, "metricId"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid metric_id")
		return uuid.Nil, false
	}
	return metricID, true
}

func writeObservationJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeObservationError(w http.ResponseWriter, status int, message string) {
	writeObservationJSON(w, status, map[string]string{"error": message})
}
