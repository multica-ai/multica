package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The workspace property catalog is capped at 20 active definitions. Six
// built-in dimensions plus that catalog fit under this guard with headroom,
// while preventing a single request from scheduling hundreds of sequential
// aggregation scans inside one snapshot transaction.
const issueTableMaxFacets = 32

type issueTableFacetValueResponse struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

type issueTableFacetResponse struct {
	Kind       string                         `json:"kind"`
	PropertyID string                         `json:"property_id,omitempty"`
	Values     []issueTableFacetValueResponse `json:"values"`
}

type issueTableFacetsResponse struct {
	QueryFingerprint string                    `json:"query_fingerprint"`
	Total            int64                     `json:"total"`
	Facets           []issueTableFacetResponse `json:"facets"`
}

func issueTableQueryWithoutFacet(input issueTableQuerySpec, facet issueTableFacetSpec) issueTableQuerySpec {
	output := input
	output.Filters = input.Filters
	if input.Filters.Properties != nil {
		output.Filters.Properties = make(map[string][]string, len(input.Filters.Properties))
		for propertyID, values := range input.Filters.Properties {
			output.Filters.Properties[propertyID] = append([]string(nil), values...)
		}
	}

	switch facet.Kind {
	case "status":
		output.Filters.Statuses = nil
	case "priority":
		output.Filters.Priorities = nil
	case "assignee":
		output.Filters.Assignees = nil
		output.Filters.IncludeNoAssignee = false
	case "creator":
		output.Filters.Creators = nil
	case "project":
		output.Filters.ProjectIDs = nil
		output.Filters.IncludeNoProject = false
	case "label":
		output.Filters.LabelIDs = nil
	case "property":
		delete(output.Filters.Properties, facet.PropertyID)
	case "working_agents":
		// Disjunctive like every other facet: drop this facet's own dimension so
		// the answer is "who would you see if you turned the agents-working
		// filter ON", which is also the answer while it is already on. That
		// makes the header chip's count stable across the toggle.
		output.Filters.WorkingIssueIDs = nil
		output.Filters.WorkingOnly = false
	}
	return output
}

func issueTableFacetIdentity(facet issueTableFacetSpec) string {
	if facet.Kind == "property" {
		return "property:" + facet.PropertyID
	}
	return facet.Kind
}

func issueTableBaseFacetExpression(query issueTableQuerySpec, facet issueTableFacetSpec) (string, bool) {
	switch facet.Kind {
	case "status":
		return "i.status", len(query.Filters.Statuses) == 0
	case "priority":
		return "i.priority", len(query.Filters.Priorities) == 0
	case "assignee":
		return "CASE WHEN i.assignee_type IS NULL OR i.assignee_id IS NULL THEN '__none__' ELSE i.assignee_type || ':' || i.assignee_id::text END", len(query.Filters.Assignees) == 0 && !query.Filters.IncludeNoAssignee
	case "creator":
		return "i.creator_type || ':' || i.creator_id::text", len(query.Filters.Creators) == 0
	case "project":
		return "COALESCE(i.project_id::text, '__none__')", len(query.Filters.ProjectIDs) == 0 && !query.Filters.IncludeNoProject
	default:
		return "", false
	}
}

func (h *Handler) issueTableBaseFacetQuery(
	w http.ResponseWriter,
	r *http.Request,
	base issueTableSQL,
	requestQuery issueTableQuerySpec,
	facets []issueTableFacetSpec,
	includeTotal bool,
) (map[string]issueTableFacetResponse, int64, bool) {
	markerCases := make([]string, 0, len(facets))
	valueCases := make([]string, 0, len(facets))
	groupingSets := make([]string, 0, len(facets)+1)
	responses := make(map[string]issueTableFacetResponse, len(facets))
	for _, facet := range facets {
		expression, ok := issueTableBaseFacetExpression(requestQuery, facet)
		if !ok {
			writeIssueTableQueryFailure(w, r, "failed to batch table facets")
			return nil, 0, false
		}
		identity := issueTableFacetIdentity(facet)
		markerCases = append(markerCases, fmt.Sprintf("WHEN GROUPING(%s) = 0 THEN '%s'", expression, identity))
		valueCases = append(valueCases, fmt.Sprintf("WHEN GROUPING(%s) = 0 THEN (%s)::text", expression, expression))
		groupingSets = append(groupingSets, "("+expression+")")
		responses[identity] = issueTableFacetResponse{
			Kind:       facet.Kind,
			PropertyID: facet.PropertyID,
			Values:     []issueTableFacetValueResponse{},
		}
	}
	if includeTotal {
		groupingSets = append(groupingSets, "()")
	}

	query := fmt.Sprintf(`SELECT CASE %s ELSE '__total__' END,
       CASE %s ELSE '' END,
       COUNT(*)::bigint
FROM issue i
WHERE %s
GROUP BY GROUPING SETS (%s)`, strings.Join(markerCases, " "), strings.Join(valueCases, " "), base.where, strings.Join(groupingSets, ", "))
	rows, err := h.DB.Query(r.Context(), query, base.args...)
	if err != nil {
		slog.Warn("ListIssueTableFacets batch query failed", append(logger.RequestAttrs(r), "error", err)...)
		writeIssueTableQueryFailure(w, r, "failed to list table facets")
		return nil, 0, false
	}
	defer rows.Close()

	var total int64
	for rows.Next() {
		var identity string
		var value string
		var count int64
		if err := rows.Scan(&identity, &value, &count); err != nil {
			writeIssueTableQueryFailure(w, r, "failed to list table facets")
			return nil, 0, false
		}
		if identity == "__total__" {
			total = count
			continue
		}
		response, ok := responses[identity]
		if !ok {
			writeIssueTableQueryFailure(w, r, "failed to list table facets")
			return nil, 0, false
		}
		response.Values = append(response.Values, issueTableFacetValueResponse{Key: value, Count: count})
		responses[identity] = response
	}
	if err := rows.Err(); err != nil {
		writeIssueTableQueryFailure(w, r, "failed to list table facets")
		return nil, 0, false
	}
	for identity, response := range responses {
		sort.Slice(response.Values, func(i, j int) bool {
			return strings.Compare(response.Values[i].Key, response.Values[j].Key) < 0
		})
		responses[identity] = response
	}
	return responses, total, true
}

func (h *Handler) issueTableFacetQuery(w http.ResponseWriter, r *http.Request, requestQuery issueTableQuerySpec, facet issueTableFacetSpec) (issueTableFacetResponse, bool) {
	response := issueTableFacetResponse{Kind: facet.Kind, PropertyID: facet.PropertyID, Values: []issueTableFacetValueResponse{}}
	compiled, ok := h.compileIssueTableQuery(w, r, issueTableQueryWithoutFacet(requestQuery, facet))
	if !ok {
		return response, false
	}

	query := ""
	switch facet.Kind {
	case "status":
		query = fmt.Sprintf(`SELECT i.status, COUNT(*)::bigint FROM issue i WHERE %s GROUP BY i.status`, compiled.where)
	case "priority":
		query = fmt.Sprintf(`SELECT i.priority, COUNT(*)::bigint FROM issue i WHERE %s GROUP BY i.priority`, compiled.where)
	case "assignee":
		query = fmt.Sprintf(`SELECT CASE WHEN i.assignee_type IS NULL OR i.assignee_id IS NULL THEN '__none__' ELSE i.assignee_type || ':' || i.assignee_id::text END, COUNT(*)::bigint FROM issue i WHERE %s GROUP BY 1`, compiled.where)
	case "creator":
		query = fmt.Sprintf(`SELECT i.creator_type || ':' || i.creator_id::text, COUNT(*)::bigint FROM issue i WHERE %s GROUP BY 1`, compiled.where)
	case "project":
		query = fmt.Sprintf(`SELECT COALESCE(i.project_id::text, '__none__'), COUNT(*)::bigint FROM issue i WHERE %s GROUP BY 1`, compiled.where)
	case "label":
		query = fmt.Sprintf(`SELECT itl.label_id::text, COUNT(DISTINCT i.id)::bigint FROM issue i JOIN issue_to_label itl ON itl.issue_id = i.id WHERE %s GROUP BY itl.label_id`, compiled.where)
	case "working_agents":
		// One row per agent currently running issue work inside THIS surface,
		// with its running-task count. Same predicate as
		// ListWorkspaceWorkingAgents with type=issue (chat > autopilot > issue
		// precedence, user-authored non-archived agents only), but the issue set
		// comes from the surface's own compiled scope + filters instead of a
		// second, independent workspace-wide definition. That is what keeps the
		// header chip's count equal to the rows clicking it leaves (MUL-5525).
		query = fmt.Sprintf(`SELECT a.id::text, COUNT(*)::bigint
FROM issue i
JOIN agent_task_queue atq ON atq.issue_id = i.id
JOIN agent a ON a.id = atq.agent_id AND a.workspace_id = i.workspace_id
WHERE %s
  AND a.kind = 'user'
  AND a.archived_at IS NULL
  AND atq.status = 'running'
  AND atq.chat_session_id IS NULL
  AND atq.autopilot_run_id IS NULL
GROUP BY a.id`, compiled.where)
	case "property":
		propertyID, err := util.ParseUUID(facet.PropertyID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid facets.property_id")
			return response, false
		}
		property, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{
			ID:          propertyID,
			WorkspaceID: compiled.workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeIssueTableUnsupportedGroup(w, "property_not_found", "The faceted property no longer exists.")
				return response, false
			}
			slog.Warn("resolve table facet property failed", append(logger.RequestAttrs(r), "error", err)...)
			writeIssueTableQueryFailure(w, r, "failed to resolve table facet")
			return response, false
		}
		if property.ArchivedAt.Valid {
			writeIssueTableUnsupportedGroup(w, "property_archived", "The faceted property is archived.")
			return response, false
		}
		propertyKey := "'" + util.UUIDToString(property.ID) + "'"
		switch property.Type {
		case "select", "actor":
			// Unset issues count under the "__none__" bucket so the filter menu's
			// "No value" option carries a real count (issues without the key).
			query = fmt.Sprintf(`SELECT COALESCE(i.properties ->> %s, '__none__'), COUNT(*)::bigint FROM issue i WHERE %s AND (jsonb_typeof(i.properties -> %s) = 'string' OR NOT (i.properties ? %s)) GROUP BY 1`, propertyKey, compiled.where, propertyKey, propertyKey)
		case "text", "url", "date", "number":
			// Scalar values are free-form, so grouping by distinct value would
			// return one row per observed value with no bound. The filter menu
			// only reads the "__none__" bucket for these types, so compute just
			// the two bounded buckets instead of pulling the whole value space.
			// Key existence is exact for "no value": stored values can never be
			// null or empty, so a present key is always a set value.
			query = fmt.Sprintf(`SELECT CASE WHEN i.properties ? %s THEN '__set__' ELSE '__none__' END, COUNT(*)::bigint FROM issue i WHERE %s GROUP BY 1`, propertyKey, compiled.where)
		case "multi_select", "multi_actor":
			query = fmt.Sprintf(`SELECT COALESCE(property_value.value, '__none__'), COUNT(DISTINCT i.id)::bigint FROM issue i JOIN LATERAL (SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(i.properties -> %s) = 'array' THEN i.properties -> %s ELSE '[]'::jsonb END) AS value UNION ALL SELECT NULL WHERE NOT (i.properties ? %s)) property_value(value) ON TRUE WHERE %s GROUP BY 1`, propertyKey, propertyKey, propertyKey, compiled.where)
		case "checkbox":
			query = fmt.Sprintf(`SELECT COALESCE(i.properties ->> %s, '__none__'), COUNT(*)::bigint FROM issue i WHERE %s AND (jsonb_typeof(i.properties -> %s) = 'boolean' OR NOT (i.properties ? %s)) GROUP BY 1`, propertyKey, compiled.where, propertyKey, propertyKey)
		default:
			writeIssueTableUnsupportedGroup(w, "property_type_unsupported", "This property type cannot be used as a filter facet.")
			return response, false
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid facets.kind")
		return response, false
	}

	rows, err := h.DB.Query(r.Context(), query, compiled.args...)
	if err != nil {
		slog.Warn("ListIssueTableFacets query failed", append(logger.RequestAttrs(r), "facet", issueTableFacetIdentity(facet), "error", err)...)
		writeIssueTableQueryFailure(w, r, "failed to list table facets")
		return response, false
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			writeIssueTableQueryFailure(w, r, "failed to list table facets")
			return response, false
		}
		response.Values = append(response.Values, issueTableFacetValueResponse{Key: value, Count: count})
	}
	if err := rows.Err(); err != nil {
		writeIssueTableQueryFailure(w, r, "failed to list table facets")
		return response, false
	}
	if facet.Kind == "working_agents" {
		values, ok := h.filterAccessibleAgentFacetValues(w, r, compiled.workspaceID, response.Values)
		if !ok {
			return response, false
		}
		response.Values = values
	}
	sort.Slice(response.Values, func(i, j int) bool {
		return strings.Compare(response.Values[i].Key, response.Values[j].Key) < 0
	})
	return response, true
}

// The working-agents facet keys are agent ids, so it discloses agent identity
// and must pass the same visibility gate as every other workspace-wide agent
// aggregation: a private or non-allow-listed agent is never exposed by its id,
// its count, or even its presence.
func (h *Handler) filterAccessibleAgentFacetValues(
	w http.ResponseWriter,
	r *http.Request,
	workspaceUUID pgtype.UUID,
	values []issueTableFacetValueResponse,
) ([]issueTableFacetValueResponse, bool) {
	if len(values) == 0 {
		return values, true
	}
	workspaceID := util.UUIDToString(workspaceUUID)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return nil, false
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeIssueTableQueryFailure(w, r, "failed to resolve agent access")
		return nil, false
	}
	filtered := make([]issueTableFacetValueResponse, 0, len(values))
	for _, value := range values {
		if _, permitted := allowed[value.Key]; permitted {
			filtered = append(filtered, value)
		}
	}
	return filtered, true
}

func (h *Handler) ListIssueTableFacets(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}
	var request issueTableFacetsRequest
	if !decodeIssueTableJSON(w, r, &request) {
		return
	}
	if len(request.Facets) == 0 || len(request.Facets) > issueTableMaxFacets {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("facets must contain between 1 and %d entries", issueTableMaxFacets))
		return
	}
	r, cancel := withIssueTableQueryTimeout(r)
	defer cancel()
	snapshot, tx, err := h.beginIssueTableSnapshot(r.Context())
	if err != nil {
		slog.Warn("ListIssueTableFacets snapshot failed", append(logger.RequestAttrs(r), "error", err)...)
		writeIssueTableQueryFailure(w, r, "failed to start table query")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	h = snapshot

	base, ok := h.compileIssueTableQuery(w, r, request.Query)
	if !ok {
		return
	}

	seen := make(map[string]struct{}, len(request.Facets))
	normalizedFacets := make([]issueTableFacetSpec, len(request.Facets))
	for index, facet := range request.Facets {
		facet.Kind = strings.TrimSpace(facet.Kind)
		facet.PropertyID = strings.TrimSpace(facet.PropertyID)
		identity := issueTableFacetIdentity(facet)
		if _, exists := seen[identity]; exists {
			writeError(w, http.StatusBadRequest, "duplicate table facet")
			return
		}
		seen[identity] = struct{}{}
		normalizedFacets[index] = facet
	}

	includeTotal := request.IncludeTotal == nil || *request.IncludeTotal
	batchFacets := make([]issueTableFacetSpec, 0, len(normalizedFacets))
	individualIndexes := make([]int, 0, len(normalizedFacets))
	for index, facet := range normalizedFacets {
		if _, batchable := issueTableBaseFacetExpression(request.Query, facet); batchable {
			batchFacets = append(batchFacets, facet)
		} else {
			individualIndexes = append(individualIndexes, index)
		}
	}

	responses := make([]issueTableFacetResponse, len(normalizedFacets))
	var total int64
	totalResolved := false
	if len(batchFacets) > 0 {
		batched, batchTotal, ok := h.issueTableBaseFacetQuery(w, r, base, request.Query, batchFacets, includeTotal)
		if !ok {
			return
		}
		for index, facet := range normalizedFacets {
			if response, exists := batched[issueTableFacetIdentity(facet)]; exists {
				responses[index] = response
			}
		}
		if includeTotal {
			total = batchTotal
			totalResolved = true
		}
	}
	if includeTotal && !totalResolved {
		if err := h.DB.QueryRow(r.Context(), fmt.Sprintf("SELECT COUNT(*)::bigint FROM issue i WHERE %s", base.where), base.args...).Scan(&total); err != nil {
			slog.Warn("ListIssueTableFacets total failed", append(logger.RequestAttrs(r), "error", err)...)
			writeIssueTableQueryFailure(w, r, "failed to count table facets")
			return
		}
	}
	for _, index := range individualIndexes {
		resolved, ok := h.issueTableFacetQuery(w, r, request.Query, normalizedFacets[index])
		if !ok {
			return
		}
		responses[index] = resolved
	}

	response := issueTableFacetsResponse{
		QueryFingerprint: base.fingerprint,
		Total:            total,
		Facets:           responses,
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ListIssueTableFacets snapshot commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeIssueTableQueryFailure(w, r, "failed to finish table query")
		return
	}
	committed = true
	writeJSON(w, http.StatusOK, response)
}
