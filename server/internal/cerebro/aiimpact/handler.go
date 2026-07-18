package aiimpact

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// Handler exposes the AI Impact HTTP seam.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around the AI Impact service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Mount registers the AI Impact routes on a workspace-scoped router.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/cerebro/ai-impact/functions", h.ListFunctions)
	r.Post("/api/cerebro/ai-impact/functions", h.CreateFunction)
	r.Get("/api/cerebro/ai-impact/operating-loops", h.ListOperatingLoops)
	r.Post("/api/cerebro/ai-impact/operating-loops", h.CreateOperatingLoop)
	r.Get("/api/cerebro/ai-impact/project-bindings", h.ListProjectBindings)
	r.Post("/api/cerebro/ai-impact/project-bindings", h.CreateProjectBinding)
	r.Get("/api/cerebro/ai-impact/metrics", h.ListMetrics)
	r.Post("/api/cerebro/ai-impact/metrics", h.CreateMetric)
	r.Get("/api/cerebro/ai-impact/evidence", h.ListWorkspaceEvidence)
	r.Get("/api/cerebro/ai-impact/functions/{functionId}/evidence", h.ListFunctionEvidence)
	r.Get("/api/cerebro/ai-impact/operating-loops/{operatingLoopId}/evidence", h.ListOperatingLoopEvidence)
	r.Get("/api/cerebro/ai-impact/metrics/{metricId}/evidence", h.ListMetricEvidence)
	r.Get("/api/cerebro/ai-impact/latest-observations", h.ListWorkspaceLatestObservations)
	r.Get("/api/cerebro/ai-impact/metrics/{metricId}/latest-observations", h.ListLatestObservations)
	r.Get("/api/cerebro/ai-impact/metrics/{metricId}/observations", h.ListObservations)
	r.Post("/api/cerebro/ai-impact/metrics/{metricId}/observations", h.AppendObservation)
}

type evidenceResponse struct {
	FunctionID        uuid.UUID       `json:"function_id"`
	FunctionName      string          `json:"function_name"`
	OperatingLoopID   uuid.UUID       `json:"operating_loop_id"`
	OperatingLoopName string          `json:"operating_loop_name"`
	MetricID          uuid.UUID       `json:"metric_id"`
	MetricName        string          `json:"metric_name"`
	MetricFamily      MetricFamily    `json:"metric_family"`
	MetricUnit        string          `json:"metric_unit"`
	MetricDirection   MetricDirection `json:"metric_direction"`
	PeriodStart       time.Time       `json:"period_start"`
	PeriodEnd         time.Time       `json:"period_end"`
	Value             float64         `json:"value"`
	EvidenceStatus    EvidenceStatus  `json:"evidence_status"`
	Confidence        float64         `json:"confidence"`
	Source            string          `json:"source"`
	Method            string          `json:"method"`
}

func toEvidenceResponse(evidence EvidenceReadModel) evidenceResponse {
	return evidenceResponse{
		FunctionID:        evidence.Function.ID,
		FunctionName:      evidence.Function.Name,
		OperatingLoopID:   evidence.OperatingLoop.ID,
		OperatingLoopName: evidence.OperatingLoop.Name,
		MetricID:          evidence.Metric.ID,
		MetricName:        evidence.Metric.Name,
		MetricFamily:      evidence.Metric.Family,
		MetricUnit:        evidence.Metric.Unit,
		MetricDirection:   evidence.Metric.Direction,
		PeriodStart:       evidence.Observation.PeriodStart,
		PeriodEnd:         evidence.Observation.PeriodEnd,
		Value:             evidence.Observation.Value,
		EvidenceStatus:    evidence.Observation.EvidenceStatus,
		Confidence:        evidence.Observation.Confidence,
		Source:            evidence.Observation.Source,
		Method:            evidence.Observation.Method,
	}
}

// ListWorkspaceEvidence returns latest observations with their business taxonomy.
func (h *Handler) ListWorkspaceEvidence(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	metricFamily := MetricFamily(r.URL.Query().Get("metric_family"))
	evidenceStatus := EvidenceStatus(r.URL.Query().Get("evidence_status"))
	source := r.URL.Query().Get("source")
	var functionID uuid.UUID
	if value := r.URL.Query().Get("function_id"); value != "" {
		var err error
		functionID, err = uuid.Parse(value)
		if err != nil {
			writeObservationError(w, http.StatusBadRequest, "invalid function_id")
			return
		}
	}
	var operatingLoopID uuid.UUID
	if value := r.URL.Query().Get("operating_loop_id"); value != "" {
		var err error
		operatingLoopID, err = uuid.Parse(value)
		if err != nil {
			writeObservationError(w, http.StatusBadRequest, "invalid operating_loop_id")
			return
		}
	}
	var metricID uuid.UUID
	if value := r.URL.Query().Get("metric_id"); value != "" {
		var err error
		metricID, err = uuid.Parse(value)
		if err != nil {
			writeObservationError(w, http.StatusBadRequest, "invalid metric_id")
			return
		}
	}
	minimumConfidence, err := parseMinimumConfidence(r.URL.Query().Get("minimum_confidence"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid minimum_confidence")
		return
	}
	guardrail, err := parseGuardrail(r.URL.Query().Get("guardrail"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid guardrail")
		return
	}
	periodStart, err := parseEvidencePeriod(r.URL.Query().Get("period_start"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid period_start")
		return
	}
	periodEnd, err := parseEvidencePeriod(r.URL.Query().Get("period_end"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid period_end")
		return
	}
	if !periodStart.IsZero() && !periodEnd.IsZero() && periodStart.After(periodEnd) {
		writeObservationError(w, http.StatusBadRequest, "period_start must not follow period_end")
		return
	}
	if metricFamily != "" && !validMetricFamily(metricFamily) {
		writeObservationError(w, http.StatusBadRequest, "invalid metric_family")
		return
	}
	if evidenceStatus != "" && !validEvidenceStatus(evidenceStatus) {
		writeObservationError(w, http.StatusBadRequest, "invalid evidence_status")
		return
	}
	evidence, err := h.service.ListFilteredEvidence(r.Context(), workspaceID, EvidenceFilter{
		FunctionID:        functionID,
		OperatingLoopID:   operatingLoopID,
		MetricID:          metricID,
		Guardrail:         guardrail,
		MetricFamily:      metricFamily,
		EvidenceStatus:    evidenceStatus,
		Source:            source,
		MinimumConfidence: minimumConfidence,
		PeriodStart:       periodStart,
		PeriodEnd:         periodEnd,
	})
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list workspace evidence")
		return
	}
	response := make([]evidenceResponse, 0, len(evidence))
	for _, item := range evidence {
		response = append(response, toEvidenceResponse(item))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"evidence": response})
}

func parseGuardrail(value string) (*bool, error) {
	switch value {
	case "":
		return nil, nil
	case "true":
		guardrail := true
		return &guardrail, nil
	case "false":
		guardrail := false
		return &guardrail, nil
	default:
		return nil, errors.New("guardrail must be true or false")
	}
}

func parseMinimumConfidence(value string) (float64, error) {
	if value == "" {
		return 0, nil
	}
	confidence, err := strconv.ParseFloat(value, 64)
	if err != nil || confidence < 0 || confidence > 1 {
		return 0, errors.New("minimum confidence must be between zero and one")
	}
	return confidence, nil
}

func parseEvidencePeriod(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

// ListFunctionEvidence returns latest observations for one Function.
func (h *Handler) ListFunctionEvidence(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}
	functionID, err := uuid.Parse(chi.URLParam(r, "functionId"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid function_id")
		return
	}

	evidence, err := h.service.ListFunctionEvidence(r.Context(), workspaceID, functionID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list function evidence")
		return
	}
	response := make([]evidenceResponse, 0, len(evidence))
	for _, item := range evidence {
		response = append(response, toEvidenceResponse(item))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"evidence": response})
}

// ListOperatingLoopEvidence returns latest observations for one Operating Loop.
func (h *Handler) ListOperatingLoopEvidence(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}
	operatingLoopID, err := uuid.Parse(chi.URLParam(r, "operatingLoopId"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid operating_loop_id")
		return
	}

	evidence, err := h.service.ListOperatingLoopEvidence(r.Context(), workspaceID, operatingLoopID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list operating loop evidence")
		return
	}
	response := make([]evidenceResponse, 0, len(evidence))
	for _, item := range evidence {
		response = append(response, toEvidenceResponse(item))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"evidence": response})
}

// ListMetricEvidence returns latest observations for one Metric.
func (h *Handler) ListMetricEvidence(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}
	metricID, err := uuid.Parse(chi.URLParam(r, "metricId"))
	if err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid metric_id")
		return
	}

	evidence, err := h.service.ListMetricEvidence(r.Context(), workspaceID, metricID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list metric evidence")
		return
	}
	response := make([]evidenceResponse, 0, len(evidence))
	for _, item := range evidence {
		response = append(response, toEvidenceResponse(item))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"evidence": response})
}

// ListWorkspaceLatestObservations returns the newest evidence for every metric period in the workspace.
func (h *Handler) ListWorkspaceLatestObservations(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	observations, err := h.service.ListWorkspaceLatestObservations(r.Context(), workspaceID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list workspace latest observations")
		return
	}
	response := make([]observationResponse, 0, len(observations))
	for _, observation := range observations {
		response = append(response, toObservationResponse(observation))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"observations": response})
}

// ListLatestObservations returns the newest evidence for each period of one metric.
func (h *Handler) ListLatestObservations(w http.ResponseWriter, r *http.Request) {
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
		writeObservationError(w, http.StatusInternalServerError, "failed to list latest observations")
		return
	}
	latest := LatestObservations(observations)
	response := make([]observationResponse, 0, len(latest))
	for _, observation := range latest {
		response = append(response, toObservationResponse(observation))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"observations": response})
}

// ListFunctions returns workspace-scoped organizational functions.
func (h *Handler) ListFunctions(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	functions, err := h.service.ListFunctions(r.Context(), workspaceID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list functions")
		return
	}
	response := make([]functionResponse, 0, len(functions))
	for _, function := range functions {
		response = append(response, toFunctionResponse(function))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"functions": response})
}

type createProjectBindingRequest struct {
	ProjectID       uuid.UUID `json:"project_id"`
	OperatingLoopID uuid.UUID `json:"operating_loop_id"`
}

type projectBindingResponse struct {
	ID              uuid.UUID `json:"id"`
	ProjectID       uuid.UUID `json:"project_id"`
	OperatingLoopID uuid.UUID `json:"operating_loop_id"`
	Active          bool      `json:"active"`
}

func toProjectBindingResponse(binding ProjectBinding) projectBindingResponse {
	return projectBindingResponse{
		ID:              binding.ID,
		ProjectID:       binding.ProjectID,
		OperatingLoopID: binding.OperatingLoopID,
		Active:          binding.Active,
	}
}

// ListProjectBindings returns workspace-scoped project bindings.
func (h *Handler) ListProjectBindings(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	bindings, err := h.service.ListProjectBindings(r.Context(), workspaceID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list project bindings")
		return
	}
	response := make([]projectBindingResponse, 0, len(bindings))
	for _, binding := range bindings {
		response = append(response, toProjectBindingResponse(binding))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"project_bindings": response})
}

// CreateProjectBinding binds one workspace project to an operating loop.
func (h *Handler) CreateProjectBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, role, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	var request createProjectBindingRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input := ProjectBindingInput{
		ProjectID:       request.ProjectID,
		OperatingLoopID: request.OperatingLoopID,
	}
	if err := ValidateProjectBinding(input); err != nil {
		writeObservationError(w, http.StatusBadRequest, err.Error())
		return
	}

	binding, err := h.service.CreateProjectBinding(r.Context(), workspaceID, role, input)
	if err != nil {
		switch {
		case errors.Is(err, ErrReadOnly):
			writeObservationError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, ErrNotFound):
			writeObservationError(w, http.StatusNotFound, err.Error())
		default:
			writeObservationError(w, http.StatusInternalServerError, "failed to create project binding")
		}
		return
	}
	writeObservationJSON(w, http.StatusCreated, toProjectBindingResponse(binding))
}

type createMetricRequest struct {
	OperatingLoopID uuid.UUID       `json:"operating_loop_id"`
	Name            string          `json:"name"`
	Family          MetricFamily    `json:"family"`
	Unit            string          `json:"unit"`
	Direction       MetricDirection `json:"direction"`
	BaselineStart   time.Time       `json:"baseline_start"`
	BaselineEnd     time.Time       `json:"baseline_end"`
	Source          string          `json:"source"`
	Guardrail       bool            `json:"guardrail"`
}

type metricResponse struct {
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
}

func toMetricResponse(metric Metric) metricResponse {
	return metricResponse{
		ID:              metric.ID,
		OperatingLoopID: metric.OperatingLoopID,
		Name:            metric.Name,
		Family:          metric.Family,
		Unit:            metric.Unit,
		Direction:       metric.Direction,
		BaselineStart:   metric.BaselineStart,
		BaselineEnd:     metric.BaselineEnd,
		Source:          metric.Source,
		Guardrail:       metric.Guardrail,
		Active:          metric.Active,
	}
}

// ListMetrics returns workspace-scoped metrics.
func (h *Handler) ListMetrics(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	metrics, err := h.service.ListMetrics(r.Context(), workspaceID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list metrics")
		return
	}
	response := make([]metricResponse, 0, len(metrics))
	for _, metric := range metrics {
		response = append(response, toMetricResponse(metric))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"metrics": response})
}

// CreateMetric creates one workspace-scoped metric.
func (h *Handler) CreateMetric(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, role, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	var request createMetricRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input := MetricInput{
		OperatingLoopID: request.OperatingLoopID,
		Name:            request.Name,
		Family:          request.Family,
		Unit:            request.Unit,
		Direction:       request.Direction,
		BaselineStart:   request.BaselineStart,
		BaselineEnd:     request.BaselineEnd,
		Source:          request.Source,
		Guardrail:       request.Guardrail,
	}
	if err := ValidateMetric(input); err != nil {
		writeObservationError(w, http.StatusBadRequest, err.Error())
		return
	}

	metric, err := h.service.CreateMetric(r.Context(), workspaceID, role, input)
	if err != nil {
		switch {
		case errors.Is(err, ErrReadOnly):
			writeObservationError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, ErrNotFound):
			writeObservationError(w, http.StatusNotFound, err.Error())
		default:
			writeObservationError(w, http.StatusInternalServerError, "failed to create metric")
		}
		return
	}
	writeObservationJSON(w, http.StatusCreated, toMetricResponse(metric))
}

type createFunctionRequest struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OwnerType   string    `json:"owner_type"`
	OwnerID     uuid.UUID `json:"owner_id"`
}

type functionResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OwnerType   string    `json:"owner_type"`
	OwnerID     uuid.UUID `json:"owner_id"`
	Active      bool      `json:"active"`
}

func toFunctionResponse(function Function) functionResponse {
	return functionResponse{
		ID:          function.ID,
		Name:        function.Name,
		Description: function.Description,
		OwnerType:   function.OwnerType,
		OwnerID:     function.OwnerID,
		Active:      function.Active,
	}
}

// CreateFunction creates one workspace-scoped organizational function.
func (h *Handler) CreateFunction(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, role, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	var request createFunctionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input := FunctionInput{
		Name:        request.Name,
		Description: request.Description,
		OwnerType:   request.OwnerType,
		OwnerID:     request.OwnerID,
	}
	if err := ValidateFunction(input); err != nil {
		writeObservationError(w, http.StatusBadRequest, err.Error())
		return
	}

	function, err := h.service.CreateFunction(r.Context(), workspaceID, role, input)
	if err != nil {
		if errors.Is(err, ErrReadOnly) {
			writeObservationError(w, http.StatusForbidden, err.Error())
			return
		}
		writeObservationError(w, http.StatusInternalServerError, "failed to create function")
		return
	}
	writeObservationJSON(w, http.StatusCreated, toFunctionResponse(function))
}

type createOperatingLoopRequest struct {
	FunctionID  uuid.UUID `json:"function_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

type operatingLoopResponse struct {
	ID          uuid.UUID `json:"id"`
	FunctionID  uuid.UUID `json:"function_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
}

func toOperatingLoopResponse(operatingLoop OperatingLoop) operatingLoopResponse {
	return operatingLoopResponse{
		ID:          operatingLoop.ID,
		FunctionID:  operatingLoop.FunctionID,
		Name:        operatingLoop.Name,
		Description: operatingLoop.Description,
		Active:      operatingLoop.Active,
	}
}

// ListOperatingLoops returns workspace-scoped operating loops.
func (h *Handler) ListOperatingLoops(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	operatingLoops, err := h.service.ListOperatingLoops(r.Context(), workspaceID)
	if err != nil {
		writeObservationError(w, http.StatusInternalServerError, "failed to list operating loops")
		return
	}
	response := make([]operatingLoopResponse, 0, len(operatingLoops))
	for _, operatingLoop := range operatingLoops {
		response = append(response, toOperatingLoopResponse(operatingLoop))
	}
	writeObservationJSON(w, http.StatusOK, map[string]any{"operating_loops": response})
}

// CreateOperatingLoop creates one workspace-scoped operating loop.
func (h *Handler) CreateOperatingLoop(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, role, ok := observationRequestContext(w, r)
	if !ok {
		return
	}

	var request createOperatingLoopRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeObservationError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input := OperatingLoopInput{
		FunctionID:  request.FunctionID,
		Name:        request.Name,
		Description: request.Description,
	}
	if err := ValidateOperatingLoop(input); err != nil {
		writeObservationError(w, http.StatusBadRequest, err.Error())
		return
	}

	operatingLoop, err := h.service.CreateOperatingLoop(r.Context(), workspaceID, role, input)
	if err != nil {
		switch {
		case errors.Is(err, ErrReadOnly):
			writeObservationError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, ErrNotFound):
			writeObservationError(w, http.StatusNotFound, err.Error())
		default:
			writeObservationError(w, http.StatusInternalServerError, "failed to create operating loop")
		}
		return
	}
	writeObservationJSON(w, http.StatusCreated, toOperatingLoopResponse(operatingLoop))
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
