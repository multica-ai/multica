package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

var usageExplorerDimensions = []string{
	"project", "group", "member", "agent", "runtime", "model", "provider",
	"trigger", "status", "cost", "saving", "skill",
}

type usageExplorerFilter struct {
	Days    int                 `json:"days"`
	Grain   string              `json:"grain"`
	Limit   int                 `json:"limit"`
	Offset  int                 `json:"offset"`
	Include map[string][]string `json:"include"`
	Exclude map[string][]string `json:"exclude"`
}

func parseUsageExplorerFilter(r *http.Request) (usageExplorerFilter, error) {
	q := r.URL.Query()
	days, err := boundedInt(q, "days", 30, 1, 365)
	if err != nil {
		return usageExplorerFilter{}, err
	}
	limit, err := boundedInt(q, "limit", 50, 1, 500)
	if err != nil {
		return usageExplorerFilter{}, err
	}
	offset, err := boundedInt(q, "offset", 0, 0, 1000000)
	if err != nil {
		return usageExplorerFilter{}, err
	}
	grain := q.Get("grain")
	if grain == "" {
		grain = "daily"
	}
	if grain != "daily" && grain != "weekly" {
		return usageExplorerFilter{}, errors.New("grain must be daily or weekly")
	}

	filter := usageExplorerFilter{
		Days: days, Grain: grain, Limit: limit, Offset: offset,
		Include: map[string][]string{}, Exclude: map[string][]string{},
	}
	for _, dimension := range usageExplorerDimensions {
		if values := normalizedValues(q[dimension]); len(values) > 0 {
			filter.Include[dimension] = values
		}
		if values := normalizedValues(q["exclude."+dimension]); len(values) > 0 {
			filter.Exclude[dimension] = values
		}
	}
	return filter, nil
}

func boundedInt(q url.Values, key string, fallback, min, max int) (int, error) {
	raw := q.Get(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return value, nil
}

func normalizedValues(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func usageExplorerMatches(filter usageExplorerFilter, values map[string][]string) bool {
	for dimension, wanted := range filter.Include {
		if len(wanted) > 0 && !usageValuesOverlap(wanted, values[dimension]) {
			return false
		}
	}
	for dimension, denied := range filter.Exclude {
		if usageValuesOverlap(denied, values[dimension]) {
			return false
		}
	}
	return true
}

func usageValuesOverlap(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if left == right {
				return true
			}
		}
	}
	return false
}

func usageExplorerFacetValues(values []string) []string {
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			values[i] = "Unknown"
		}
	}
	return normalizedValues(values)
}

type usageExplorerRun struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	Status          string    `json:"status"`
	Project         string    `json:"project"`
	Agent           string    `json:"agent"`
	Runtime         string    `json:"runtime"`
	Model           string    `json:"model"`
	Provider        string    `json:"provider"`
	Trigger         string    `json:"trigger"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CostCents       int64     `json:"cost_cents"`
	CostKind        string    `json:"cost_kind"`
	DurationSeconds int64     `json:"duration_seconds"`
	TraceURL        *string   `json:"trace_url"`
}

type usageExplorerFacet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}
type usageExplorerSummary struct {
	Runs               int   `json:"runs"`
	Tokens             int64 `json:"tokens"`
	ActualCostCents    int64 `json:"actual_cost_cents"`
	CalculatedCostRuns int   `json:"calculated_cost_runs"`
	MissingCostRuns    int   `json:"missing_cost_runs"`
}
type usageExplorerResponse struct {
	Filter  usageExplorerFilter             `json:"filter"`
	Summary usageExplorerSummary            `json:"summary"`
	Facets  map[string][]usageExplorerFacet `json:"facets"`
	Runs    []usageExplorerRun              `json:"runs"`
	Total   int                             `json:"total"`
	Savings []usageExplorerSaving           `json:"savings"`
}
type usageExplorerSaving struct {
	Type         string `json:"type"`
	State        string `json:"state"`
	SavedCents   int64  `json:"saved_cents"`
	SavedUnits   int64  `json:"saved_units"`
	AffectedRuns int    `json:"affected_runs"`
}

const usageExplorerRunsQuery = `
		SELECT atq.id::text, COALESCE(tu.created_at, atq.created_at), atq.status,
		       COALESCE(p.title, 'Unknown'), COALESCE(a.name, 'Deleted'),
		       COALESCE(ar.name, ar.provider, 'Unknown'), COALESCE(tu.model, 'Unknown'), COALESCE(NULLIF(tu.provider,''), 'Unknown'),
		       CASE WHEN atq.autopilot_run_id IS NOT NULL THEN 'autopilot' WHEN atq.chat_session_id IS NOT NULL THEN 'chat' WHEN atq.trigger_comment_id IS NOT NULL THEN 'issue' ELSE 'manual' END,
		       COALESCE(tu.input_tokens,0), COALESCE(tu.output_tokens,0), COALESCE(tu.cost_cents,0),
		       GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(atq.completed_at, now()) - COALESCE(atq.started_at, atq.created_at)))::bigint)
		FROM agent_task_queue atq JOIN agent a ON a.id=atq.agent_id
		LEFT JOIN task_usage tu ON tu.task_id=atq.id LEFT JOIN issue i ON i.id=atq.issue_id
		LEFT JOIN project p ON p.id=i.project_id LEFT JOIN agent_runtime ar ON ar.id=atq.runtime_id
		WHERE a.workspace_id=$1 AND atq.created_at >= now()-make_interval(days=>$2)
		ORDER BY atq.created_at DESC, atq.id`

// GetDashboardUsageExplorer serves one canonical filtered result so totals,
// facets, savings and run rows cannot drift apart.
func (h *Handler) GetDashboardUsageExplorer(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	filter, err := parseUsageExplorerFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.DB.Query(r.Context(), usageExplorerRunsQuery, parseUUID(workspaceID), filter.Days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to explore usage")
		return
	}
	defer rows.Close()
	all := []usageExplorerRun{}
	for rows.Next() {
		var row usageExplorerRun
		if err := rows.Scan(&row.ID, &row.CreatedAt, &row.Status, &row.Project, &row.Agent, &row.Runtime, &row.Model, &row.Provider, &row.Trigger, &row.InputTokens, &row.OutputTokens, &row.CostCents, &row.DurationSeconds); err != nil {
			writeError(w, 500, "failed to read usage")
			return
		}
		if row.CostCents > 0 {
			row.CostKind = "actual"
		} else {
			row.CostKind = "missing"
		}
		values := map[string][]string{"project": {row.Project}, "agent": {row.Agent}, "runtime": {row.Runtime}, "model": {row.Model}, "provider": {row.Provider}, "trigger": {row.Trigger}, "status": {row.Status}, "cost": {row.CostKind}}
		if usageExplorerMatches(filter, values) {
			all = append(all, row)
		}
	}
	response := usageExplorerResponse{Filter: filter, Facets: map[string][]usageExplorerFacet{}, Runs: []usageExplorerRun{}, Savings: []usageExplorerSaving{}, Total: len(all)}
	facetCounts := map[string]map[string]int{}
	for _, d := range usageExplorerDimensions {
		facetCounts[d] = map[string]int{}
	}
	for _, row := range all {
		response.Summary.Runs++
		response.Summary.Tokens += row.InputTokens + row.OutputTokens
		if row.CostKind == "actual" {
			response.Summary.ActualCostCents += row.CostCents
		} else {
			response.Summary.MissingCostRuns++
		}
		vals := map[string]string{"project": row.Project, "agent": row.Agent, "runtime": row.Runtime, "model": row.Model, "provider": row.Provider, "trigger": row.Trigger, "status": row.Status, "cost": row.CostKind}
		for d, v := range vals {
			facetCounts[d][v]++
		}
	}
	for d, counts := range facetCounts {
		values := make([]string, 0, len(counts))
		for v := range counts {
			values = append(values, v)
		}
		for _, v := range usageExplorerFacetValues(values) {
			response.Facets[d] = append(response.Facets[d], usageExplorerFacet{Value: v, Count: counts[v]})
		}
	}
	start := filter.Offset
	if start > len(all) {
		start = len(all)
	}
	end := start + filter.Limit
	if end > len(all) {
		end = len(all)
	}
	response.Runs = all[start:end]
	savingsRows, _ := h.DB.Query(r.Context(), `SELECT saving_key, COALESCE(SUM(saved_cents),0)::bigint, COALESCE(SUM(GREATEST(baseline_value-effective_value,0)),0)::bigint, COUNT(DISTINCT task_id)::int FROM cerebro_cost_optimization_measurement WHERE workspace_id=$1 AND created_at>=now()-make_interval(days=>$2) GROUP BY saving_key ORDER BY saving_key`, parseUUID(workspaceID), filter.Days)
	if savingsRows != nil {
		defer savingsRows.Close()
		for savingsRows.Next() {
			var s usageExplorerSaving
			if savingsRows.Scan(&s.Type, &s.SavedCents, &s.SavedUnits, &s.AffectedRuns) == nil {
				s.State = "measured"
				if usageExplorerMatches(filter, map[string][]string{"saving": {s.Type}}) {
					response.Savings = append(response.Savings, s)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

type skillUsageReportItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type skillUsageReport struct {
	Skills []skillUsageReportItem `json:"skills"`
}

func parseSkillUsageReport(r *http.Request) (skillUsageReport, error) {
	var report skillUsageReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		return report, errors.New("invalid JSON body")
	}
	if len(report.Skills) == 0 || len(report.Skills) > 100 {
		return report, errors.New("skills must contain between 1 and 100 items")
	}
	for i := range report.Skills {
		report.Skills[i].Name = strings.TrimSpace(report.Skills[i].Name)
		if report.Skills[i].Name == "" || report.Skills[i].Count < 1 {
			return report, errors.New("each skill requires a name and positive count")
		}
		if report.Skills[i].ID != "" {
			if _, err := util.ParseUUID(report.Skills[i].ID); err != nil {
				return report, errors.New("skill id must be a UUID")
			}
		}
	}
	return report, nil
}

// ReportTaskSkillUsage stores actual runtime-reported skill invocations. It
// deliberately does not infer usage from skills merely assigned to an agent.
func (h *Handler) ReportTaskSkillUsage(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "taskId")
	if !ok {
		return
	}
	report, err := parseSkillUsageReport(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, item := range report.Skills {
		var skillID pgtype.UUID
		if item.ID != "" {
			skillID, _ = util.ParseUUID(item.ID)
		}
		_, err = h.DB.Exec(r.Context(), `
			INSERT INTO task_skill_usage (task_id, skill_id, skill_name, invocation_count)
			SELECT atq.id, s.id, COALESCE(s.name, $3), $4
			FROM agent_task_queue atq
			JOIN agent a ON a.id = atq.agent_id
			LEFT JOIN skill s ON s.id = NULLIF($2::text, '')::uuid AND s.workspace_id = a.workspace_id
			WHERE atq.id = $1
			ON CONFLICT (task_id, skill_name) DO UPDATE SET
				skill_id = COALESCE(EXCLUDED.skill_id, task_skill_usage.skill_id),
				invocation_count = task_skill_usage.invocation_count + EXCLUDED.invocation_count,
				last_used_at = now()`, taskID, uuidString(skillID), item.Name, item.Count)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record skill usage")
			return
		}
	}
	h.projectAnalyticsRun(r.Context(), uuidToString(taskID))
	w.WriteHeader(http.StatusNoContent)
}

type dashboardSkillUsageRow struct {
	SkillID         *string    `json:"skill_id"`
	SkillName       string     `json:"skill_name"`
	InvocationCount int64      `json:"invocation_count"`
	RunCount        int64      `json:"run_count"`
	LastUsedAt      *time.Time `json:"last_used_at"`
}

// GetDashboardSkillUsage returns only explicit invocation reports, never
// agent-skill assignments. This keeps "used" distinct from "available".
func (h *Handler) GetDashboardSkillUsage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	filter, err := parseUsageExplorerFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID != "" {
		if _, err := util.ParseUUID(projectID); err != nil {
			writeError(w, http.StatusBadRequest, "project_id must be a UUID")
			return
		}
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT tsu.skill_id::text, tsu.skill_name,
		       SUM(tsu.invocation_count)::bigint, COUNT(DISTINCT tsu.task_id)::bigint,
		       MAX(tsu.last_used_at)
		FROM task_skill_usage tsu
		JOIN agent_task_queue atq ON atq.id = tsu.task_id
		LEFT JOIN issue i ON i.id = atq.issue_id
		JOIN agent a ON a.id = atq.agent_id
		WHERE a.workspace_id = $1
		  AND tsu.last_used_at >= now() - make_interval(days => $2)
		  AND ($3::text = '' OR i.project_id = $3::uuid)
		  AND (cardinality($4::text[]) = 0 OR tsu.skill_name = ANY($4::text[]))
		  AND (cardinality($5::text[]) = 0 OR NOT (tsu.skill_name = ANY($5::text[])))
		GROUP BY tsu.skill_id, tsu.skill_name
		ORDER BY SUM(tsu.invocation_count) DESC, tsu.skill_name
		LIMIT $6 OFFSET $7`, parseUUID(workspaceID), filter.Days, projectID,
		filter.Include["skill"], filter.Exclude["skill"], filter.Limit, filter.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill usage")
		return
	}
	defer rows.Close()
	result := []dashboardSkillUsageRow{}
	for rows.Next() {
		var row dashboardSkillUsageRow
		if err := rows.Scan(&row.SkillID, &row.SkillName, &row.InvocationCount, &row.RunCount, &row.LastUsedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read skill usage")
			return
		}
		result = append(result, row)
	}
	writeJSON(w, http.StatusOK, result)
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return util.UUIDToString(value)
}
