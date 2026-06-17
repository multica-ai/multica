package recurringissue

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler hosts the REST surface for the recurring-issue feature, wired into
// the router by the cerebro-recurring-issue-routes CEREBRO-PATCH. Every
// endpoint is workspace-scoped via the X-Workspace-ID header and
// authenticated via X-User-ID.
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Pool     *pgxpool.Pool
	Upstream *db.Queries
}

func NewHandler(cerebro *cerebrodb.Queries, pool *pgxpool.Pool, upstream *db.Queries) *Handler {
	return &Handler{Cerebro: cerebro, Pool: pool, Upstream: upstream}
}

// recurrenceRequest is the body for PUT (upsert). Pointers distinguish
// "absent" from "explicit zero" on update.
type recurrenceRequest struct {
	Frequency      *string `json:"frequency,omitempty"`
	IntervalCount  *int32  `json:"interval_count,omitempty"`
	Weekdays       *[]int  `json:"weekdays,omitempty"`
	DaysAfter      *int32  `json:"days_after,omitempty"`
	TriggerStatus  *string `json:"trigger_status,omitempty"`
	Anchor         *string `json:"anchor,omitempty"`
	CreateNewIssue *bool   `json:"create_new_issue,omitempty"`
	NewStatus      *string `json:"new_status,omitempty"`
	RecurForever   *bool   `json:"recur_forever,omitempty"`
	EndDate        *string `json:"end_date,omitempty"`
	MaxOccurrences *int32  `json:"max_occurrences,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

// GetByIssue returns the recurrence rule for an issue, or 404 when none —
// the client treats 404 as "not recurring yet" and shows the empty panel.
func (h *Handler) GetByIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, ok := parseUUIDParam(w, r, "issueID")
	if !ok {
		return
	}
	rule, err := h.Cerebro.GetCerebroIssueRecurrenceBySourceIssue(r.Context(), issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no recurrence for issue")
			return
		}
		writeError(w, http.StatusInternalServerError, "load recurrence failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, rule.WorkspaceID) {
		return
	}
	writeJSON(w, http.StatusOK, toResponse(rule))
}

// Upsert creates or updates the recurrence rule for an issue.
func (h *Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issueID, ok := parseUUIDParam(w, r, "issueID")
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	var req recurrenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	issue, err := h.Upstream.GetIssue(r.Context(), issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load issue failed")
		return
	}
	if !uuidEqual(issue.WorkspaceID, wsUUID) {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return
	}

	existing, err := h.Cerebro.GetCerebroIssueRecurrenceBySourceIssue(r.Context(), issueID)
	hasExisting := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "load recurrence failed")
		return
	}

	merged := mergeRequest(req, existing, hasExisting)
	if verr := validate(merged); verr != "" {
		writeError(w, http.StatusBadRequest, verr)
		return
	}
	endDate, err := parseDate(merged.endDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end_date (want YYYY-MM-DD)")
		return
	}

	if hasExisting {
		out, err := h.Cerebro.UpdateCerebroIssueRecurrence(r.Context(), cerebrodb.UpdateCerebroIssueRecurrenceParams{
			ID:             existing.ID,
			Frequency:      merged.frequency,
			IntervalCount:  merged.intervalCount,
			Weekdays:       intsToInt16s(merged.weekdays),
			DaysAfter:      merged.daysAfter,
			TriggerStatus:  merged.triggerStatus,
			Anchor:         merged.anchor,
			CreateNewIssue: merged.createNewIssue,
			NewStatus:      merged.newStatus,
			RecurForever:   merged.recurForever,
			EndDate:        endDate,
			MaxOccurrences: pgInt4(merged.maxOccurrences),
			Enabled:        merged.enabled,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update recurrence failed")
			return
		}
		writeJSON(w, http.StatusOK, toResponse(out))
		return
	}

	// Create. Arm based on the issue's current status so a rule added to an
	// already-closed issue does not immediately fire on the next tick unless
	// it is re-opened first.
	armed := issue.Status != merged.triggerStatus
	out, err := h.Cerebro.CreateCerebroIssueRecurrence(r.Context(), cerebrodb.CreateCerebroIssueRecurrenceParams{
		WorkspaceID:    wsUUID,
		ProjectID:      issue.ProjectID,
		SourceIssueID:  issueID,
		Frequency:      merged.frequency,
		IntervalCount:  merged.intervalCount,
		Weekdays:       intsToInt16s(merged.weekdays),
		DaysAfter:      merged.daysAfter,
		TriggerStatus:  merged.triggerStatus,
		Anchor:         merged.anchor,
		CreateNewIssue: merged.createNewIssue,
		NewStatus:      merged.newStatus,
		RecurForever:   merged.recurForever,
		EndDate:        endDate,
		MaxOccurrences: pgInt4(merged.maxOccurrences),
		Armed:          armed,
		Enabled:        merged.enabled,
		CreatedByType:  pgtype.Text{String: "member", Valid: true},
		CreatedByID:    parseUUIDOrZero(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create recurrence failed")
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(out))
}

// Delete removes the recurrence rule for an issue (stops the series).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, ok := parseUUIDParam(w, r, "issueID")
	if !ok {
		return
	}
	rule, err := h.Cerebro.GetCerebroIssueRecurrenceBySourceIssue(r.Context(), issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "load recurrence failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, rule.WorkspaceID) {
		return
	}
	if err := h.Cerebro.DeleteCerebroIssueRecurrence(r.Context(), rule.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete recurrence failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunNow triggers one synchronous sweep of a single rule (ops/QA) so a tester
// does not have to wait for the poll interval.
func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	rule, err := h.Cerebro.GetCerebroIssueRecurrence(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recurrence not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load recurrence failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, rule.WorkspaceID) {
		return
	}
	sweeper := NewSweeper(h.Pool, h.Cerebro, h.Upstream)
	spawned, err := sweeper.ProcessRuleByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "run failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"spawned": spawned})
}

// ---------------------------------------------------------------------------
// merge + validate
// ---------------------------------------------------------------------------

type mergedConfig struct {
	frequency      string
	intervalCount  int32
	weekdays       []int
	daysAfter      int32
	triggerStatus  string
	anchor         string
	createNewIssue bool
	newStatus      string
	recurForever   bool
	endDate        string
	maxOccurrences *int32
	enabled        bool
}

func mergeRequest(req recurrenceRequest, existing cerebrodb.CerebroIssueRecurrence, hasExisting bool) mergedConfig {
	// Defaults for a brand-new rule (match the migration defaults + mockup).
	m := mergedConfig{
		frequency:      FreqWeekly,
		intervalCount:  1,
		weekdays:       nil,
		daysAfter:      0,
		triggerStatus:  "done",
		anchor:         AnchorCompletion,
		createNewIssue: true,
		newStatus:      "todo",
		recurForever:   true,
		endDate:        "",
		maxOccurrences: nil,
		enabled:        true,
	}
	if hasExisting {
		m.frequency = existing.Frequency
		m.intervalCount = existing.IntervalCount
		m.weekdays = int16sToInts(existing.Weekdays)
		m.daysAfter = existing.DaysAfter
		m.triggerStatus = existing.TriggerStatus
		m.anchor = existing.Anchor
		m.createNewIssue = existing.CreateNewIssue
		m.newStatus = existing.NewStatus
		m.recurForever = existing.RecurForever
		if existing.EndDate.Valid {
			m.endDate = existing.EndDate.Time.UTC().Format(dateLayout)
		}
		if existing.MaxOccurrences.Valid {
			v := existing.MaxOccurrences.Int32
			m.maxOccurrences = &v
		}
		m.enabled = existing.Enabled
	}
	if req.Frequency != nil {
		m.frequency = *req.Frequency
	}
	if req.IntervalCount != nil {
		m.intervalCount = *req.IntervalCount
	}
	if req.Weekdays != nil {
		m.weekdays = *req.Weekdays
	}
	if req.DaysAfter != nil {
		m.daysAfter = *req.DaysAfter
	}
	if req.TriggerStatus != nil {
		m.triggerStatus = strings.TrimSpace(*req.TriggerStatus)
	}
	if req.Anchor != nil {
		m.anchor = *req.Anchor
	}
	if req.CreateNewIssue != nil {
		m.createNewIssue = *req.CreateNewIssue
	}
	if req.NewStatus != nil {
		m.newStatus = strings.TrimSpace(*req.NewStatus)
	}
	if req.RecurForever != nil {
		m.recurForever = *req.RecurForever
	}
	if req.EndDate != nil {
		m.endDate = strings.TrimSpace(*req.EndDate)
	}
	if req.MaxOccurrences != nil {
		v := *req.MaxOccurrences
		m.maxOccurrences = &v
	}
	if req.Enabled != nil {
		m.enabled = *req.Enabled
	}
	return m
}

func validate(m mergedConfig) string {
	if err := ValidateFrequency(m.frequency); err != nil {
		return err.Error()
	}
	if err := ValidateAnchor(m.anchor); err != nil {
		return err.Error()
	}
	if m.intervalCount <= 0 {
		return "interval_count must be > 0"
	}
	if m.daysAfter < 0 {
		return "days_after must be >= 0"
	}
	if strings.TrimSpace(m.triggerStatus) == "" {
		return "trigger_status is required"
	}
	if strings.TrimSpace(m.newStatus) == "" {
		return "new_status is required"
	}
	for _, w := range m.weekdays {
		if w < 1 || w > 7 {
			return "weekdays must be ISO 1..7"
		}
	}
	if m.maxOccurrences != nil && *m.maxOccurrences <= 0 {
		return "max_occurrences must be > 0"
	}
	return ""
}

// ---------------------------------------------------------------------------
// shared helpers (package-local copies of the sprint handler's helpers)
// ---------------------------------------------------------------------------

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing "+name)
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return uid, true
}

func workspaceFromContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing workspace")
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return pgtype.UUID{}, false
	}
	return uid, true
}

func ensureWorkspaceMatch(w http.ResponseWriter, r *http.Request, target pgtype.UUID) bool {
	current, ok := workspaceFromContext(w, r)
	if !ok {
		return false
	}
	if !uuidEqual(current, target) {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return false
	}
	return true
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func parseUUIDOrZero(s string) pgtype.UUID {
	uid, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return uid
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
