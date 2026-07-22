package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueTableGroupValueResponse struct {
	Kind       string              `json:"kind"`
	Status     string              `json:"status,omitempty"`
	Actor      *issueTableActorRef `json:"actor"`
	PropertyID string              `json:"property_id,omitempty"`
	Value      any                 `json:"value,omitempty"`
	ValueState string              `json:"value_state,omitempty"`
}

type issueTableGroupDescriptorResponse struct {
	Key   string                       `json:"key"`
	Value issueTableGroupValueResponse `json:"value"`
	Count int64                        `json:"count"`
}

type issueTableGroupsResponse struct {
	QueryFingerprint string                              `json:"query_fingerprint"`
	Total            int64                               `json:"total"`
	Groups           []issueTableGroupDescriptorResponse `json:"groups"`
	NextCursor       *string                             `json:"next_cursor"`
}

type resolvedIssueTableGroup struct {
	kind              string
	propertyID        string
	propertyType      string
	groupExpr         string
	groupSortExpr     string
	activeOptionOrder []string
	activeOptions     map[string]struct{}
}

func issueTableGroupIdentity(group issueTableGroupSpec) string {
	if group.Kind == "property" {
		return "group:property:" + group.PropertyID
	}
	return "group:" + group.Kind
}

func (h *Handler) resolveIssueTableGroup(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, group issueTableGroupSpec, allowNone bool) (resolvedIssueTableGroup, bool) {
	switch group.Kind {
	case "none":
		if !allowNone {
			writeError(w, http.StatusBadRequest, "group.kind=none is not valid for group headers")
			return resolvedIssueTableGroup{}, false
		}
		return resolvedIssueTableGroup{kind: "none"}, true
	case "status":
		return resolvedIssueTableGroup{kind: "status", groupExpr: "i.status"}, true
	case "assignee":
		return resolvedIssueTableGroup{
			kind:      "assignee",
			groupExpr: "CASE WHEN i.assignee_type IS NULL OR i.assignee_id IS NULL THEN '__unassigned__' ELSE i.assignee_type || ':' || i.assignee_id::text END",
			// groupSortExpr runs after issues have been reduced to one row per
			// actor. Resolving display names before GROUP BY executes one lookup
			// per issue and turns large assignee groups into an N+1 query plan.
			groupSortExpr: `LOWER(COALESCE(CASE split_part(group_value, ':', 1)
  WHEN 'member' THEN (SELECT u.name FROM "user" u WHERE u.id = split_part(group_value, ':', 2)::uuid)
  WHEN 'agent' THEN (SELECT a.name FROM agent a WHERE a.workspace_id = $1 AND a.id = split_part(group_value, ':', 2)::uuid)
  WHEN 'squad' THEN (SELECT s.name FROM squad s WHERE s.workspace_id = $1 AND s.id = split_part(group_value, ':', 2)::uuid)
END, ''))`,
		}, true
	case "property":
		propertyUUID, err := util.ParseUUID(group.PropertyID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group.property_id")
			return resolvedIssueTableGroup{}, false
		}
		property, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{
			ID:          propertyUUID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeIssueTableUnsupportedGroup(w, "property_not_found", "The grouped property no longer exists.")
				return resolvedIssueTableGroup{}, false
			}
			slog.Warn("resolve table group property failed", append(logger.RequestAttrs(r), "error", err)...)
			writeIssueTableQueryFailure(w, r, "failed to resolve table group")
			return resolvedIssueTableGroup{}, false
		}
		if property.ArchivedAt.Valid {
			writeIssueTableUnsupportedGroup(w, "property_archived", "The grouped property is archived.")
			return resolvedIssueTableGroup{}, false
		}
		propertyID := util.UUIDToString(property.ID)
		quotedKey := "'" + propertyID + "'"
		resolved := resolvedIssueTableGroup{
			kind:          "property",
			propertyID:    propertyID,
			propertyType:  property.Type,
			activeOptions: map[string]struct{}{},
		}
		switch property.Type {
		case "select":
			config := parsePropertyConfig(property.Config)
			resolved.activeOptionOrder = make([]string, 0, len(config.Options))
			for _, option := range config.Options {
				resolved.activeOptions[option.ID] = struct{}{}
				resolved.activeOptionOrder = append(resolved.activeOptionOrder, "value:"+option.ID)
			}
			resolved.groupExpr = fmt.Sprintf(`CASE
  WHEN NOT (i.properties ? %s) THEN 'unset:'
  WHEN jsonb_typeof(i.properties -> %s) = 'string' AND i.properties ->> %s = ANY(%%s::text[]) THEN 'value:' || (i.properties ->> %s)
  WHEN jsonb_typeof(i.properties -> %s) = 'string' THEN 'unavailable:' || (i.properties ->> %s)
  ELSE 'unavailable:'
END`, quotedKey, quotedKey, quotedKey, quotedKey, quotedKey, quotedKey)
		case "checkbox":
			resolved.groupExpr = fmt.Sprintf(`CASE
  WHEN NOT (i.properties ? %s) THEN 'unset:'
  WHEN jsonb_typeof(i.properties -> %s) = 'boolean' THEN 'value:' || (i.properties ->> %s)
  ELSE 'unavailable:'
END`, quotedKey, quotedKey, quotedKey)
		default:
			writeIssueTableUnsupportedGroup(w, "property_type_unsupported", "This property type cannot be used for grouping.")
			return resolvedIssueTableGroup{}, false
		}
		return resolved, true
	default:
		writeIssueTableUnsupportedGroup(w, "group_kind_unsupported", "This group type is not supported.")
		return resolvedIssueTableGroup{}, false
	}
}

func writeIssueTableUnsupportedGroup(w http.ResponseWriter, code, message string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error":   "unsupported_group",
		"code":    code,
		"message": message,
	})
}

func (group resolvedIssueTableGroup) expression(addArg func(any) string) string {
	if group.kind == "property" && group.propertyType == "select" {
		active := make([]string, 0, len(group.activeOptions))
		for value := range group.activeOptions {
			active = append(active, value)
		}
		return fmt.Sprintf(group.groupExpr, addArg(active))
	}
	return group.groupExpr
}

func (group resolvedIssueTableGroup) sortExpression() string {
	if group.groupSortExpr != "" {
		return group.groupSortExpr
	}
	return "group_value"
}

func (group resolvedIssueTableGroup) orderExpression(addArg func(any) string) string {
	switch group.kind {
	case "status":
		return "CASE group_value WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'in_review' THEN 3 WHEN 'done' THEN 4 WHEN 'blocked' THEN 5 WHEN 'cancelled' THEN 6 ELSE 7 END"
	case "assignee":
		return "CASE WHEN group_value = '__unassigned__' THEN 1 ELSE 0 END"
	case "property":
		if group.propertyType == "select" {
			ref := addArg(group.activeOptionOrder)
			return fmt.Sprintf("CASE WHEN group_value LIKE 'value:%%' THEN COALESCE(array_position(%s::text[], group_value), 100000) WHEN group_value LIKE 'unavailable:%%' THEN 100001 ELSE 100002 END", ref)
		}
		return "CASE group_value WHEN 'value:false' THEN 0 WHEN 'value:true' THEN 1 WHEN 'unavailable:' THEN 2 ELSE 3 END"
	default:
		return "0"
	}
}

func (group resolvedIssueTableGroup) descriptor(raw string, count int64) (issueTableGroupDescriptorResponse, error) {
	descriptor := issueTableGroupDescriptorResponse{Count: count}
	switch group.kind {
	case "status":
		if !issueTableContainsString(validIssueStatuses, raw) {
			return descriptor, fmt.Errorf("unexpected status group value %q", raw)
		}
		descriptor.Key = "status:" + raw
		descriptor.Value = issueTableGroupValueResponse{Kind: "status", Status: raw}
	case "assignee":
		descriptor.Value.Kind = "assignee"
		if raw == "__unassigned__" {
			descriptor.Key = "assignee:unassigned"
			return descriptor, nil
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 || !isIssueActorType(parts[0]) {
			return descriptor, fmt.Errorf("unexpected assignee group value %q", raw)
		}
		if _, err := util.ParseUUID(parts[1]); err != nil {
			return descriptor, fmt.Errorf("unexpected assignee group value %q", raw)
		}
		descriptor.Key = "assignee:" + raw
		descriptor.Value.Actor = &issueTableActorRef{Type: parts[0], ID: parts[1]}
	case "property":
		state, rawValue, ok := strings.Cut(raw, ":")
		if !ok {
			return descriptor, fmt.Errorf("unexpected property group value %q", raw)
		}
		encoded := base64.RawURLEncoding.EncodeToString([]byte(rawValue))
		descriptor.Key = "property:" + group.propertyID + ":" + state + ":" + encoded
		descriptor.Value = issueTableGroupValueResponse{
			Kind:       "property",
			PropertyID: group.propertyID,
		}
		switch state {
		case "unset":
			descriptor.Value.ValueState = "unset"
		case "unavailable":
			descriptor.Value.ValueState = "unavailable"
		case "value":
			descriptor.Value.ValueState = "value"
			if group.propertyType == "checkbox" {
				value, err := strconv.ParseBool(rawValue)
				if err != nil {
					return descriptor, fmt.Errorf("unexpected checkbox group value %q", rawValue)
				}
				descriptor.Value.Value = value
			} else {
				descriptor.Value.Value = rawValue
			}
		default:
			return descriptor, fmt.Errorf("unexpected property group state %q", state)
		}
	default:
		return descriptor, fmt.Errorf("unsupported group kind %q", group.kind)
	}
	return descriptor, nil
}

func (group resolvedIssueTableGroup) predicate(w http.ResponseWriter, key string, addArg func(any) string) (string, bool) {
	switch group.kind {
	case "none":
		if key != "" {
			writeError(w, http.StatusBadRequest, "group_key must be empty when group.kind=none")
			return "", false
		}
		return "TRUE", true
	case "status":
		const prefix = "status:"
		if !strings.HasPrefix(key, prefix) || !issueTableContainsString(validIssueStatuses, strings.TrimPrefix(key, prefix)) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		return fmt.Sprintf("i.status = %s::text", addArg(strings.TrimPrefix(key, prefix))), true
	case "assignee":
		const prefix = "assignee:"
		if !strings.HasPrefix(key, prefix) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		raw := strings.TrimPrefix(key, prefix)
		if raw == "unassigned" {
			return "i.assignee_type IS NULL AND i.assignee_id IS NULL", true
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 || !isIssueActorType(parts[0]) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		id, err := util.ParseUUID(parts[1])
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		return fmt.Sprintf("i.assignee_type = %s::text AND i.assignee_id = %s::uuid", addArg(parts[0]), addArg(id)), true
	case "property":
		prefix := "property:" + group.propertyID + ":"
		if !strings.HasPrefix(key, prefix) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		rest := strings.TrimPrefix(key, prefix)
		state, encoded, ok := strings.Cut(rest, ":")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		value := string(decoded)
		keySQL := "'" + group.propertyID + "'"
		switch state {
		case "unset":
			if value != "" {
				writeError(w, http.StatusBadRequest, "invalid group_key")
				return "", false
			}
			return fmt.Sprintf("NOT (i.properties ? %s)", keySQL), true
		case "value":
			if group.propertyType == "select" {
				if _, exists := group.activeOptions[value]; !exists {
					writeError(w, http.StatusBadRequest, "invalid group_key")
					return "", false
				}
				return fmt.Sprintf("jsonb_typeof(i.properties -> %s) = 'string' AND i.properties ->> %s = %s::text", keySQL, keySQL, addArg(value)), true
			}
			if value != "true" && value != "false" {
				writeError(w, http.StatusBadRequest, "invalid group_key")
				return "", false
			}
			return fmt.Sprintf("jsonb_typeof(i.properties -> %s) = 'boolean' AND i.properties ->> %s = %s::text", keySQL, keySQL, addArg(value)), true
		case "unavailable":
			if group.propertyType == "select" && value != "" {
				return fmt.Sprintf("jsonb_typeof(i.properties -> %s) = 'string' AND i.properties ->> %s = %s::text", keySQL, keySQL, addArg(value)), true
			}
			return fmt.Sprintf("i.properties ? %s AND jsonb_typeof(i.properties -> %s) <> %s::text", keySQL, keySQL, addArg(map[string]string{"select": "string", "checkbox": "boolean"}[group.propertyType])), true
		default:
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid group_key")
		return "", false
	}
}

func (h *Handler) ListIssueTableGroups(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}
	var request issueTableGroupsRequest
	if !decodeIssueTableJSON(w, r, &request) {
		return
	}
	r, cancel := withIssueTableQueryTimeout(r)
	defer cancel()
	snapshot, tx, err := h.beginIssueTableSnapshot(r.Context())
	if err != nil {
		slog.Warn("ListIssueTableGroups snapshot failed", append(logger.RequestAttrs(r), "error", err)...)
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
	limit, cursor, ok := normalizeIssueTablePage(w, request.Page)
	if !ok {
		return
	}
	compiled, ok := h.compileIssueTableQuery(w, r, request.Query)
	if !ok {
		return
	}
	group, ok := h.resolveIssueTableGroup(w, r, compiled.workspaceID, request.Group, false)
	if !ok {
		return
	}
	groupIdentity := issueTableGroupIdentity(request.Group)
	if !issueTableCursorMatches(w, cursor, compiled.fingerprint, &groupIdentity, nil) {
		return
	}

	args := append([]any(nil), compiled.args...)
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	groupExpr := group.expression(addArg)
	groupSortExpr := group.sortExpression()
	orderExpr := group.orderExpression(addArg)
	cursorPredicate := "TRUE"
	if cursor != nil {
		if cursor.GroupOrder == nil || cursor.GroupSortKey == nil || cursor.GroupCursorKey == nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		orderRef := addArg(*cursor.GroupOrder)
		sortRef := addArg(*cursor.GroupSortKey)
		keyRef := addArg(*cursor.GroupCursorKey)
		cursorPredicate = fmt.Sprintf(`(group_order > %[1]s::int OR (
  group_order = %[1]s::int AND (
    group_sort > %[2]s::text OR (group_sort = %[2]s::text AND group_value > %[3]s::text)
  )
))`, orderRef, sortRef, keyRef)
	}
	limitRef := addArg(limit + 1)
	query := fmt.Sprintf(`WITH grouped AS (
	  SELECT %s AS group_value, COUNT(*)::bigint AS issue_count
	  FROM issue i
	  WHERE %s
	  GROUP BY 1
	), sorted AS (
	  SELECT group_value, issue_count, (%s)::text AS group_sort
	  FROM grouped
	), ranked AS (
	  SELECT group_value, issue_count, group_sort, (%s)::int AS group_order,
	         SUM(issue_count) OVER ()::bigint AS total
	  FROM sorted
	)
	SELECT group_value, issue_count, group_sort, group_order, total
	FROM ranked
	WHERE %s
	ORDER BY group_order ASC, group_sort ASC, group_value ASC
	LIMIT %s`, groupExpr, compiled.where, groupSortExpr, orderExpr, cursorPredicate, limitRef)

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		slog.Warn("ListIssueTableGroups query failed", append(logger.RequestAttrs(r), "error", err)...)
		writeIssueTableQueryFailure(w, r, "failed to list table groups")
		return
	}
	defer rows.Close()

	groups := make([]issueTableGroupDescriptorResponse, 0, limit+1)
	orders := make([]int, 0, limit+1)
	sortValues := make([]string, 0, limit+1)
	values := make([]string, 0, limit+1)
	var total int64
	for rows.Next() {
		var raw string
		var count int64
		var sortValue string
		var order int
		if err := rows.Scan(&raw, &count, &sortValue, &order, &total); err != nil {
			writeIssueTableQueryFailure(w, r, "failed to list table groups")
			return
		}
		descriptor, err := group.descriptor(raw, count)
		if err != nil {
			slog.Warn("ListIssueTableGroups descriptor failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to resolve table group")
			return
		}
		groups = append(groups, descriptor)
		orders = append(orders, order)
		sortValues = append(sortValues, sortValue)
		values = append(values, raw)
	}
	if err := rows.Err(); err != nil {
		writeIssueTableQueryFailure(w, r, "failed to list table groups")
		return
	}
	rows.Close()

	var nextCursor *string
	if len(groups) > limit {
		groups = groups[:limit]
		lastOrder := orders[limit-1]
		lastSort := sortValues[limit-1]
		lastKey := values[limit-1]
		nextCursor = encodeIssueTableCursor(issueTableCursor{
			Version:          1,
			QueryFingerprint: compiled.fingerprint,
			GroupKey:         &groupIdentity,
			GroupOrder:       &lastOrder,
			GroupSortKey:     &lastSort,
			GroupCursorKey:   &lastKey,
		})
	}
	response := issueTableGroupsResponse{
		QueryFingerprint: compiled.fingerprint,
		Total:            total,
		Groups:           groups,
		NextCursor:       nextCursor,
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ListIssueTableGroups snapshot commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeIssueTableQueryFailure(w, r, "failed to finish table query")
		return
	}
	committed = true
	writeJSON(w, http.StatusOK, response)
}
