package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type IssueAssigneeGroupResponse struct {
	ID           string          `json:"id"`
	AssigneeType *string         `json:"assignee_type"`
	AssigneeID   *string         `json:"assignee_id"`
	Issues       []IssueResponse `json:"issues"`
	Total        int64           `json:"total"`
}

type GroupedIssuesResponse struct {
	Groups []IssueAssigneeGroupResponse `json:"groups"`
}

type groupedIssueRow struct {
	db.ListIssuesRow
	GroupTotal int64
}

func assigneeGroupID(assigneeType pgtype.Text, assigneeID pgtype.UUID) string {
	if assigneeType.Valid && assigneeID.Valid {
		return "assignee:" + assigneeType.String + ":" + uuidToString(assigneeID)
	}
	return "assignee:unassigned"
}

func (h *Handler) ListGroupedIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}

	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "assignee"
	}
	if groupBy != "assignee" {
		writeError(w, http.StatusBadRequest, "unsupported group_by")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v > 0 {
			offset = v
		}
	}

	where := []string{"i.workspace_id = $1"}
	q := newIssueQueryBuilder(wsUUID)

	statuses := splitCommaParam(r.URL.Query().Get("statuses"))
	if len(statuses) == 0 {
		statuses = splitCommaParam(r.URL.Query().Get("status"))
	}
	if len(statuses) > 0 {
		where = append(where, fmt.Sprintf("i.status = ANY(%s::text[])", q.addArg(statuses)))
	}

	priorities := splitCommaParam(r.URL.Query().Get("priorities"))
	if len(priorities) == 0 {
		priorities = splitCommaParam(r.URL.Query().Get("priority"))
	}
	if len(priorities) > 0 {
		where = append(where, fmt.Sprintf("i.priority = ANY(%s::text[])", q.addArg(priorities)))
	}

	assigneeTypes := splitCommaParam(r.URL.Query().Get("assignee_types"))
	if len(assigneeTypes) > 0 {
		for _, assigneeType := range assigneeTypes {
			if !isIssueActorType(assigneeType) {
				writeError(w, http.StatusBadRequest, "invalid assignee_types")
				return
			}
		}
		where = append(where, fmt.Sprintf("i.assignee_type = ANY(%s::text[])", q.addArg(assigneeTypes)))
	}

	if raw := r.URL.Query().Get("assignee_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "assignee_id")
		if !ok {
			return
		}
		where = append(where, fmt.Sprintf("i.assignee_id = %s::uuid", q.addArg(id)))
	}
	if raw := r.URL.Query().Get("assignee_ids"); raw != "" {
		ids, ok := parseUUIDParamList(w, raw, "assignee_ids")
		if !ok {
			return
		}
		if len(ids) > 0 {
			where = append(where, fmt.Sprintf("i.assignee_id = ANY(%s::uuid[])", q.addArg(ids)))
		}
	}
	if raw := r.URL.Query().Get("creator_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "creator_id")
		if !ok {
			return
		}
		where = append(where, fmt.Sprintf("i.creator_id = %s::uuid", q.addArg(id)))
	}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return
		}
		where = append(where, fmt.Sprintf("i.project_id = %s::uuid", q.addArg(id)))
	}
	if filter, ok := parseMetadataFilterParam(w, r.URL.Query().Get("metadata")); !ok {
		return
	} else if filter != nil {
		where = append(where, fmt.Sprintf("i.metadata @> %s::jsonb", q.addArg(string(filter))))
	}
	// Mirror the involves_user_id 4-branch UNION from sqlc's ListIssues /
	// ListOpenIssues / CountIssues. ListGroupedIssues is a hand-written dynamic
	// SQL builder that does not share parameters with sqlc, so the fragment is
	// re-implemented here in lock-step. Member-direct assignment is excluded by
	// design: that semantics belongs to tab 1 (`assignee_id`), and tab 3 must
	// stay disjoint from tab 1.
	if raw := r.URL.Query().Get("involves_user_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "involves_user_id")
		if !ok {
			return
		}
		ref := q.addArg(id)
		where = append(where, fmt.Sprintf(`(
    (i.assignee_type = 'agent' AND i.assignee_id IN (
       SELECT a.id FROM agent a
        WHERE a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'member'
          AND sm.member_id   = %[1]s::uuid
       UNION
       SELECT s.id
         FROM squad s
         JOIN agent a ON a.id = s.leader_id
        WHERE s.workspace_id = $1
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
       UNION
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
         JOIN agent a ON a.id = sm.member_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'agent'
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
)`, ref))
	}

	assigneeFilters, ok := parseActorFilterList(w, r.URL.Query().Get("assignee_filters"), "assignee_filters")
	if !ok {
		return
	}
	includeNoAssignee := r.URL.Query().Get("include_no_assignee") == "true"
	if len(assigneeFilters) > 0 || includeNoAssignee {
		ors := make([]string, 0, len(assigneeFilters)+1)
		for _, filter := range assigneeFilters {
			ors = append(ors, fmt.Sprintf(
				"(i.assignee_type = %s::text AND i.assignee_id = %s::uuid)",
				q.addArg(filter.actorType),
				q.addArg(filter.actorID),
			))
		}
		if includeNoAssignee {
			ors = append(ors, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	creatorFilters, ok := parseActorFilterList(w, r.URL.Query().Get("creator_filters"), "creator_filters")
	if !ok {
		return
	}
	if len(creatorFilters) > 0 {
		ors := make([]string, 0, len(creatorFilters))
		for _, filter := range creatorFilters {
			ors = append(ors, fmt.Sprintf(
				"(i.creator_type = %s::text AND i.creator_id = %s::uuid)",
				q.addArg(filter.actorType),
				q.addArg(filter.actorID),
			))
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	projectIDs, ok := parseUUIDParamList(w, r.URL.Query().Get("project_ids"), "project_ids")
	if !ok {
		return
	}
	includeNoProject := r.URL.Query().Get("include_no_project") == "true"
	if len(projectIDs) > 0 || includeNoProject {
		ors := make([]string, 0, 2)
		if len(projectIDs) > 0 {
			ors = append(ors, fmt.Sprintf("i.project_id = ANY(%s::uuid[])", q.addArg(projectIDs)))
		}
		if includeNoProject {
			ors = append(ors, "i.project_id IS NULL")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	labelIDs, ok := parseUUIDParamList(w, r.URL.Query().Get("label_ids"), "label_ids")
	if !ok {
		return
	}
	if len(labelIDs) > 0 {
		where = append(where, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM issue_to_label itl WHERE itl.issue_id = i.id AND itl.label_id = ANY(%s::uuid[]))",
			q.addArg(labelIDs),
		))
	}

	dateFilter, ok := parseIssueDateFilter(w, r.URL.Query())
	if !ok {
		return
	}
	where = appendIssueDateFilter(where, q.addArg, dateFilter)

	if groupAssigneeType := r.URL.Query().Get("group_assignee_type"); groupAssigneeType != "" {
		if groupAssigneeType == "none" {
			where = append(where, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
		} else {
			if !isIssueActorType(groupAssigneeType) {
				writeError(w, http.StatusBadRequest, "invalid group_assignee_type")
				return
			}
			rawID := r.URL.Query().Get("group_assignee_id")
			if rawID == "" {
				writeError(w, http.StatusBadRequest, "invalid group_assignee_id")
				return
			}
			assigneeID, ok := parseUUIDOrBadRequest(w, rawID, "group_assignee_id")
			if !ok {
				return
			}
			where = append(where, fmt.Sprintf(
				"(i.assignee_type = %s::text AND i.assignee_id = %s::uuid)",
				q.addArg(groupAssigneeType),
				q.addArg(assigneeID),
			))
		}
	}

	sortCol := "position"
	if s := r.URL.Query().Get("sort"); s != "" {
		switch s {
		case "position", "title", "created_at", "start_date", "due_date":
			sortCol = s
		case "priority":
			sortCol = "CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END"
		default:
			writeError(w, http.StatusBadRequest, "invalid sort value")
			return
		}
	}
	sortDir := "ASC"
	if sortCol != "position" {
		if d := r.URL.Query().Get("direction"); d != "" {
			switch strings.ToLower(d) {
			case "asc":
				sortDir = "ASC"
			case "desc":
				sortDir = "DESC"
			default:
				writeError(w, http.StatusBadRequest, "invalid direction value")
				return
			}
		}
	}

	intraGroupOrder := sortCol
	if !strings.HasPrefix(sortCol, "CASE") {
		intraGroupOrder = "i." + sortCol
	}
	intraGroupOrder += " " + sortDir
	if sortCol == "start_date" || sortCol == "due_date" {
		intraGroupOrder += " NULLS LAST"
	}
	intraGroupOrder += ", i.created_at DESC"

	offsetRef := q.addArg(int64(offset))
	limitRef := q.addArg(int64(limit))
	query := fmt.Sprintf(`
WITH ranked AS (
	SELECT
		i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.position, i.due_date, i.created_at, i.updated_at,
		i.number, i.project_id, i.metadata,
		COUNT(*) OVER (PARTITION BY i.assignee_type, i.assignee_id) AS group_total,
		ROW_NUMBER() OVER (
			PARTITION BY i.assignee_type, i.assignee_id
			ORDER BY %s
		) AS rn
	FROM issue i
	WHERE %s
)
SELECT
	id, workspace_id, title, description, status, priority,
	assignee_type, assignee_id, creator_type, creator_id,
	parent_issue_id, position, due_date, created_at, updated_at,
	number, project_id, metadata, group_total
FROM ranked
WHERE rn > %s AND rn <= %s + %s
ORDER BY
	CASE assignee_type
		WHEN 'member' THEN 0
		WHEN 'agent' THEN 1
		WHEN 'squad' THEN 2
		ELSE 3
	END,
	assignee_type NULLS LAST,
	assignee_id NULLS LAST,
	rn`, intraGroupOrder, strings.Join(where, " AND "), offsetRef, offsetRef, limitRef)

	rows, err := h.DB.Query(ctx, query, q.args...)
	if err != nil {
		slog.Warn("ListGroupedIssues query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list grouped issues")
		return
	}
	defer rows.Close()

	groupedRows := []groupedIssueRow{}
	for rows.Next() {
		var row groupedIssueRow
		if err := rows.Scan(
			&row.ID,
			&row.WorkspaceID,
			&row.Title,
			&row.Description,
			&row.Status,
			&row.Priority,
			&row.AssigneeType,
			&row.AssigneeID,
			&row.CreatorType,
			&row.CreatorID,
			&row.ParentIssueID,
			&row.Position,
			&row.DueDate,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.Number,
			&row.ProjectID,
			&row.Metadata,
			&row.GroupTotal,
		); err != nil {
			slog.Warn("ListGroupedIssues scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list grouped issues")
			return
		}
		groupedRows = append(groupedRows, row)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListGroupedIssues rows failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list grouped issues")
		return
	}

	ids := make([]pgtype.UUID, len(groupedRows))
	for i, row := range groupedRows {
		ids[i] = row.ID
	}
	labelsMap := h.labelsByIssue(ctx, wsUUID, ids)
	prefix := h.getIssuePrefix(ctx, wsUUID)

	groups := []IssueAssigneeGroupResponse{}
	groupIndex := map[string]int{}
	for _, row := range groupedRows {
		groupID := assigneeGroupID(row.AssigneeType, row.AssigneeID)
		idx, exists := groupIndex[groupID]
		if !exists {
			idx = len(groups)
			groupIndex[groupID] = idx
			groups = append(groups, IssueAssigneeGroupResponse{
				ID:           groupID,
				AssigneeType: textToPtr(row.AssigneeType),
				AssigneeID:   uuidToPtr(row.AssigneeID),
				Issues:       []IssueResponse{},
				Total:        row.GroupTotal,
			})
		}

		issue := issueListRowToResponse(row.ListIssuesRow, prefix)
		labels := labelsMap[issue.ID]
		if labels == nil {
			labels = []LabelResponse{}
		}
		issue.Labels = &labels
		groups[idx].Issues = append(groups[idx].Issues, issue)
	}

	writeJSON(w, http.StatusOK, GroupedIssuesResponse{Groups: groups})
}
