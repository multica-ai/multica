package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	issueTableDefaultPageSize = 50
	issueTableMaxPageSize     = 100
	issueTableQueryTimeout    = 8 * time.Second
)

func withIssueTableQueryTimeout(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), issueTableQueryTimeout)
	return r.WithContext(ctx), cancel
}

func (h *Handler) beginIssueTableSnapshot(ctx context.Context) (*Handler, pgx.Tx, error) {
	if h.TxStarter == nil {
		return nil, nil, errors.New("transaction starter is unavailable")
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, nil, err
	}
	snapshot := *h
	snapshot.DB = tx
	snapshot.Queries = db.New(tx)
	return &snapshot, tx, nil
}

func writeIssueTableQueryFailure(w http.ResponseWriter, r *http.Request, message string) {
	if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{
			"error":   "query_timeout",
			"message": "The table query took too long. Narrow the filters and retry.",
		})
		return
	}
	writeError(w, http.StatusInternalServerError, message)
}

type issueTableActorRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type issueTableScope struct {
	Kind          string              `json:"kind"`
	AssigneeTypes []string            `json:"assignee_types,omitempty"`
	ProjectID     string              `json:"project_id,omitempty"`
	Actor         *issueTableActorRef `json:"actor,omitempty"`
	Relation      string              `json:"relation,omitempty"`
}

type issueTableDateFilterRequest struct {
	Field string `json:"field"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type issueTableFiltersRequest struct {
	Statuses          []string                     `json:"statuses,omitempty"`
	Priorities        []string                     `json:"priorities,omitempty"`
	Assignees         []issueTableActorRef         `json:"assignees,omitempty"`
	IncludeNoAssignee bool                         `json:"include_no_assignee,omitempty"`
	Creators          []issueTableActorRef         `json:"creators,omitempty"`
	ProjectIDs        []string                     `json:"project_ids,omitempty"`
	IncludeNoProject  bool                         `json:"include_no_project,omitempty"`
	LabelIDs          []string                     `json:"label_ids,omitempty"`
	Properties        map[string][]string          `json:"properties,omitempty"`
	Date              *issueTableDateFilterRequest `json:"date,omitempty"`
	WorkingOnly       bool                         `json:"working_only,omitempty"`
	IncludeSubIssues  *bool                        `json:"include_sub_issues,omitempty"`
}

type issueTableSortRequest struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type issueTableQuerySpec struct {
	Scope   issueTableScope          `json:"scope"`
	Filters issueTableFiltersRequest `json:"filters"`
	Search  string                   `json:"search,omitempty"`
	Sort    issueTableSortRequest    `json:"sort"`
}

type issueTableGroupSpec struct {
	Kind            string   `json:"kind"`
	PropertyID      string   `json:"property_id,omitempty"`
	IncludeEmpty    bool     `json:"include_empty,omitempty"`
	Primary         string   `json:"primary,omitempty"`
	Secondary       string   `json:"secondary,omitempty"`
	SecondaryValues []string `json:"secondary_values,omitempty"`
}

type issueTablePageRequest struct {
	Limit  int     `json:"limit,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
}

type issueTableHierarchyRequest struct {
	Enabled bool `json:"enabled"`
}

type issueTableGroupsRequest struct {
	Query issueTableQuerySpec   `json:"query"`
	Group issueTableGroupSpec   `json:"group"`
	Page  issueTablePageRequest `json:"page"`
}

type issueTableRowsRequest struct {
	Query     issueTableQuerySpec        `json:"query"`
	Group     issueTableGroupSpec        `json:"group"`
	GroupKey  *string                    `json:"group_key"`
	Hierarchy issueTableHierarchyRequest `json:"hierarchy"`
	ParentID  *string                    `json:"parent_id"`
	Page      issueTablePageRequest      `json:"page"`
}

type issueTableFacetSpec struct {
	Kind       string `json:"kind"`
	PropertyID string `json:"property_id,omitempty"`
}

type issueTableFacetsRequest struct {
	Query        issueTableQuerySpec   `json:"query"`
	Facets       []issueTableFacetSpec `json:"facets"`
	IncludeTotal *bool                 `json:"include_total,omitempty"`
}

type issueTableSQL struct {
	where       string
	args        []any
	fingerprint string
	workspaceID pgtype.UUID
}

type issueTableCursor struct {
	Version          int     `json:"v"`
	QueryFingerprint string  `json:"query"`
	GroupKey         *string `json:"group_key,omitempty"`
	ParentID         *string `json:"parent_id,omitempty"`
	GroupOrder       *int    `json:"group_order,omitempty"`
	GroupSortKey     *string `json:"group_sort_key,omitempty"`
	GroupCursorKey   *string `json:"group_cursor_key,omitempty"`
	BranchIdentity   string  `json:"branch_identity,omitempty"`
	SortValue        *string `json:"sort_value,omitempty"`
	SortIsNull       bool    `json:"sort_is_null,omitempty"`
	RowCreatedAt     string  `json:"row_created_at,omitempty"`
	RowID            string  `json:"row_id,omitempty"`
}

func decodeIssueTableJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue table query: "+err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid issue table query: request must contain one JSON object")
		return false
	}
	return true
}

func normalizeIssueTablePage(w http.ResponseWriter, page issueTablePageRequest) (int, *issueTableCursor, bool) {
	limit := page.Limit
	if limit == 0 {
		limit = issueTableDefaultPageSize
	}
	if limit < 1 || limit > issueTableMaxPageSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("page.limit must be between 1 and %d", issueTableMaxPageSize))
		return 0, nil, false
	}
	if page.Cursor == nil || strings.TrimSpace(*page.Cursor) == "" {
		return limit, nil, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(*page.Cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return 0, nil, false
	}
	var cursor issueTableCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return 0, nil, false
	}
	return limit, &cursor, true
}

func encodeIssueTableCursor(cursor issueTableCursor) *string {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return nil
	}
	value := base64.RawURLEncoding.EncodeToString(encoded)
	return &value
}

func issueTableCursorMatches(w http.ResponseWriter, cursor *issueTableCursor, fingerprint string, groupKey, parentID *string) bool {
	if cursor == nil {
		return true
	}
	if cursor.QueryFingerprint != fingerprint || !equalOptionalString(cursor.GroupKey, groupKey) || !equalOptionalString(cursor.ParentID, parentID) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "cursor_query_mismatch",
			"message": "cursor does not belong to this table query",
		})
		return false
	}
	return true
}

func equalOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func canonicalIssueTableFingerprint(workspaceID string, spec issueTableQuerySpec) (string, error) {
	normalized := spec
	normalized.Search = strings.TrimSpace(normalized.Search)
	normalized.Scope.AssigneeTypes = sortedUniqueStrings(normalized.Scope.AssigneeTypes)
	normalized.Filters.Statuses = sortedUniqueStrings(normalized.Filters.Statuses)
	normalized.Filters.Priorities = sortedUniqueStrings(normalized.Filters.Priorities)
	normalized.Filters.ProjectIDs = sortedUniqueStrings(normalized.Filters.ProjectIDs)
	normalized.Filters.LabelIDs = sortedUniqueStrings(normalized.Filters.LabelIDs)
	normalized.Filters.Assignees = sortedUniqueActors(normalized.Filters.Assignees)
	normalized.Filters.Creators = sortedUniqueActors(normalized.Filters.Creators)
	for key, values := range normalized.Filters.Properties {
		normalized.Filters.Properties[key] = sortedUniqueStrings(values)
	}
	encoded, err := json.Marshal(struct {
		WorkspaceID string              `json:"workspace_id"`
		Query       issueTableQuerySpec `json:"query"`
	}{
		WorkspaceID: workspaceID,
		Query:       normalized,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil
	}
	return result
}

func sortedUniqueActors(values []issueTableActorRef) []issueTableActorRef {
	seen := make(map[string]issueTableActorRef, len(values))
	for _, value := range values {
		key := value.Type + ":" + value.ID
		seen[key] = value
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]issueTableActorRef, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func issueTableContainsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseIssueTableUUIDList(w http.ResponseWriter, values []string, field string) ([]pgtype.UUID, bool) {
	result := make([]pgtype.UUID, 0, len(values))
	for _, raw := range values {
		parsed, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+field)
			return nil, false
		}
		result = append(result, parsed)
	}
	return result, true
}

func parseIssueTableActor(w http.ResponseWriter, actor issueTableActorRef, field string) (issueActorFilter, bool) {
	if !isIssueActorType(actor.Type) {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return issueActorFilter{}, false
	}
	id, err := util.ParseUUID(actor.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return issueActorFilter{}, false
	}
	return issueActorFilter{actorType: actor.Type, actorID: id}, true
}

func appendIssueTableInvolvedPredicate(where []string, addArg func(any) string, userID pgtype.UUID) []string {
	ref := addArg(userID)
	return append(where, fmt.Sprintf(`(
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

func (h *Handler) compileIssueTableQuery(w http.ResponseWriter, r *http.Request, spec issueTableQuerySpec) (issueTableSQL, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return issueTableSQL{}, false
	}
	fingerprint, err := canonicalIssueTableFingerprint(util.UUIDToString(workspaceUUID), spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to canonicalize table query")
		return issueTableSQL{}, false
	}

	where := []string{"i.workspace_id = $1"}
	args := []any{workspaceUUID}
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	for _, status := range spec.Filters.Statuses {
		if !issueTableContainsString(validIssueStatuses, status) {
			writeError(w, http.StatusBadRequest, "invalid filters.statuses")
			return issueTableSQL{}, false
		}
	}
	if len(spec.Filters.Statuses) > 0 {
		where = append(where, fmt.Sprintf("i.status = ANY(%s::text[])", addArg(sortedUniqueStrings(spec.Filters.Statuses))))
	}
	for _, priority := range spec.Filters.Priorities {
		if !issueTableContainsString(validIssuePriorities, priority) {
			writeError(w, http.StatusBadRequest, "invalid filters.priorities")
			return issueTableSQL{}, false
		}
	}
	if len(spec.Filters.Priorities) > 0 {
		where = append(where, fmt.Sprintf("i.priority = ANY(%s::text[])", addArg(sortedUniqueStrings(spec.Filters.Priorities))))
	}

	switch spec.Scope.Kind {
	case "workspace":
		for _, actorType := range spec.Scope.AssigneeTypes {
			if !isIssueActorType(actorType) {
				writeError(w, http.StatusBadRequest, "invalid scope.assignee_types")
				return issueTableSQL{}, false
			}
		}
		if len(spec.Scope.AssigneeTypes) > 0 {
			where = append(where, fmt.Sprintf("i.assignee_type = ANY(%s::text[])", addArg(sortedUniqueStrings(spec.Scope.AssigneeTypes))))
		}
	case "project":
		projectID, err := util.ParseUUID(spec.Scope.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid scope.project_id")
			return issueTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.project_id = %s::uuid", addArg(projectID)))
	case "assignee":
		if spec.Scope.Actor == nil {
			writeError(w, http.StatusBadRequest, "scope.actor is required")
			return issueTableSQL{}, false
		}
		actor, ok := parseIssueTableActor(w, *spec.Scope.Actor, "scope.actor")
		if !ok {
			return issueTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.assignee_type = %s::text AND i.assignee_id = %s::uuid", addArg(actor.actorType), addArg(actor.actorID)))
	case "creator":
		if spec.Scope.Actor == nil {
			writeError(w, http.StatusBadRequest, "scope.actor is required")
			return issueTableSQL{}, false
		}
		actor, ok := parseIssueTableActor(w, *spec.Scope.Actor, "scope.actor")
		if !ok {
			return issueTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.creator_type = %s::text AND i.creator_id = %s::uuid", addArg(actor.actorType), addArg(actor.actorID)))
	case "my":
		userID, ok := requireUserID(w, r)
		if !ok {
			return issueTableSQL{}, false
		}
		userUUID, err := util.ParseUUID(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not authenticated")
			return issueTableSQL{}, false
		}
		relation := spec.Scope.Relation
		if relation == "" {
			relation = "any"
		}
		switch relation {
		case "assigned":
			where = append(where, fmt.Sprintf("i.assignee_type = 'member' AND i.assignee_id = %s::uuid", addArg(userUUID)))
		case "created":
			where = append(where, fmt.Sprintf("i.creator_type = 'member' AND i.creator_id = %s::uuid", addArg(userUUID)))
		case "involved":
			where = appendIssueTableInvolvedPredicate(where, addArg, userUUID)
		case "any":
			assignedRef := addArg(userUUID)
			createdRef := addArg(userUUID)
			involved := appendIssueTableInvolvedPredicate(nil, addArg, userUUID)[0]
			where = append(where, fmt.Sprintf("((i.assignee_type = 'member' AND i.assignee_id = %s::uuid) OR (i.creator_type = 'member' AND i.creator_id = %s::uuid) OR %s)", assignedRef, createdRef, involved))
		default:
			writeError(w, http.StatusBadRequest, "invalid scope.relation")
			return issueTableSQL{}, false
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid scope.kind")
		return issueTableSQL{}, false
	}

	if len(spec.Filters.Assignees) > 0 || spec.Filters.IncludeNoAssignee {
		ors := make([]string, 0, len(spec.Filters.Assignees)+1)
		for _, value := range spec.Filters.Assignees {
			actor, ok := parseIssueTableActor(w, value, "filters.assignees")
			if !ok {
				return issueTableSQL{}, false
			}
			ors = append(ors, fmt.Sprintf("(i.assignee_type = %s::text AND i.assignee_id = %s::uuid)", addArg(actor.actorType), addArg(actor.actorID)))
		}
		if spec.Filters.IncludeNoAssignee {
			ors = append(ors, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	if len(spec.Filters.Creators) > 0 {
		ors := make([]string, 0, len(spec.Filters.Creators))
		for _, value := range spec.Filters.Creators {
			actor, ok := parseIssueTableActor(w, value, "filters.creators")
			if !ok {
				return issueTableSQL{}, false
			}
			ors = append(ors, fmt.Sprintf("(i.creator_type = %s::text AND i.creator_id = %s::uuid)", addArg(actor.actorType), addArg(actor.actorID)))
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	projectIDs, ok := parseIssueTableUUIDList(w, spec.Filters.ProjectIDs, "filters.project_ids")
	if !ok {
		return issueTableSQL{}, false
	}
	if len(projectIDs) > 0 || spec.Filters.IncludeNoProject {
		ors := make([]string, 0, 2)
		if len(projectIDs) > 0 {
			ors = append(ors, fmt.Sprintf("i.project_id = ANY(%s::uuid[])", addArg(projectIDs)))
		}
		if spec.Filters.IncludeNoProject {
			ors = append(ors, "i.project_id IS NULL")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	labelIDs, ok := parseIssueTableUUIDList(w, spec.Filters.LabelIDs, "filters.label_ids")
	if !ok {
		return issueTableSQL{}, false
	}
	if len(labelIDs) > 0 {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM issue_to_label itl WHERE itl.issue_id = i.id AND itl.label_id = ANY(%s::uuid[]))", addArg(labelIDs)))
	}

	if len(spec.Filters.Properties) > 0 {
		raw, err := json.Marshal(spec.Filters.Properties)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid filters.properties")
			return issueTableSQL{}, false
		}
		compiled, ok := parsePropertiesFilterParam(w, string(raw))
		if !ok {
			return issueTableSQL{}, false
		}
		if len(compiled) > 0 {
			where = append(where, propertiesFilterPredicate(compiled, addArg))
		}
	}

	if spec.Filters.Date != nil {
		column := ""
		switch spec.Filters.Date.Field {
		case "created_at", "updated_at":
			column = spec.Filters.Date.Field
		default:
			writeError(w, http.StatusBadRequest, "invalid filters.date.field")
			return issueTableSQL{}, false
		}
		start, startErr := time.Parse(time.RFC3339Nano, spec.Filters.Date.Start)
		end, endErr := time.Parse(time.RFC3339Nano, spec.Filters.Date.End)
		if startErr != nil || endErr != nil || !start.Before(end) {
			writeError(w, http.StatusBadRequest, "invalid filters.date range")
			return issueTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.%s >= %s AND i.%s < %s", column, addArg(start), column, addArg(end)))
	}

	if spec.Filters.WorkingOnly {
		where = append(where, "EXISTS (SELECT 1 FROM agent_task_queue atq WHERE atq.issue_id = i.id AND atq.status = 'running')")
	}
	if spec.Filters.IncludeSubIssues != nil && !*spec.Filters.IncludeSubIssues {
		where = append(where, "i.parent_issue_id IS NULL")
	}
	where = appendIssueTableSearchFilter(where, addArg, spec.Search)

	return issueTableSQL{
		where:       strings.Join(where, " AND "),
		args:        args,
		fingerprint: fingerprint,
		workspaceID: workspaceUUID,
	}, true
}
