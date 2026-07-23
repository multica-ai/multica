package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	Kind       string               `json:"kind"`
	Status     string               `json:"status,omitempty"`
	Actor      *issueTableActorRef  `json:"actor"`
	ProjectID  *string              `json:"project_id,omitempty"`
	ParentID   *string              `json:"parent_id,omitempty"`
	Parent     *issueTableParentRef `json:"parent,omitempty"`
	PropertyID string               `json:"property_id,omitempty"`
	Value      any                  `json:"value,omitempty"`
	ValueState string               `json:"value_state,omitempty"`
}

type issueTableParentRef struct {
	ID         string `json:"id"`
	Number     int32  `json:"number"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}

type issueTableGroupContext struct {
	Parent *issueTableParentRef `json:"parent,omitempty"`
}

type issueTableGroupDescriptorResponse struct {
	Key             string                              `json:"key"`
	Value           issueTableGroupValueResponse        `json:"value"`
	Count           int64                               `json:"count"`
	SecondaryGroups []issueTableGroupDescriptorResponse `json:"secondary_groups,omitempty"`
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
	primary           *resolvedIssueTableGroup
	secondaryValues   []string
	secondaryFiltered bool
}

func issueTableGroupIdentity(group issueTableGroupSpec) string {
	if group.Kind == "property" {
		return "group:property:" + group.PropertyID + ":empty=" + strconv.FormatBool(group.IncludeEmpty)
	}
	if group.Kind == "compound" {
		identity := "group:compound:" + group.Primary + ":" + group.Secondary
		if group.SecondaryValues != nil {
			identity += ":visible=" + strings.Join(group.SecondaryValues, ",")
		}
		return identity
	}
	return "group:" + group.Kind
}

func (h *Handler) resolveIssueTableGroup(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, group issueTableGroupSpec, allowNone bool) (resolvedIssueTableGroup, bool) {
	if group.Kind != "compound" && group.SecondaryValues != nil {
		writeError(w, http.StatusBadRequest, "group.secondary_values requires group.kind=compound")
		return resolvedIssueTableGroup{}, false
	}
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
	case "project":
		return resolvedIssueTableGroup{
			kind:      "project",
			groupExpr: "COALESCE(i.project_id::text, '__no_project__')",
			groupSortExpr: `CASE WHEN group_value = '__no_project__' THEN '' ELSE LOWER(COALESCE(
  (SELECT p.title FROM project p WHERE p.workspace_id = $1 AND p.id = group_value::uuid),
  ''
)) END`,
		}, true
	case "parent":
		return resolvedIssueTableGroup{
			kind:      "parent",
			groupExpr: "COALESCE(i.parent_issue_id::text, '__no_parent__')",
			groupSortExpr: `CASE WHEN group_value = '__no_parent__' THEN '' ELSE LOWER(COALESCE(
  (SELECT p.title FROM issue p WHERE p.workspace_id = $1 AND p.id = group_value::uuid),
  ''
)) END`,
		}, true
	case "compound":
		if group.Secondary != "status" {
			writeIssueTableUnsupportedGroup(w, "secondary_group_unsupported", "Only status is supported as a secondary group.")
			return resolvedIssueTableGroup{}, false
		}
		if group.Primary != "assignee" && group.Primary != "project" && group.Primary != "parent" {
			writeIssueTableUnsupportedGroup(w, "primary_group_unsupported", "This primary group is not supported.")
			return resolvedIssueTableGroup{}, false
		}
		seenSecondaryValues := make(map[string]struct{}, len(group.SecondaryValues))
		for _, value := range group.SecondaryValues {
			if !issueTableContainsString(validIssueStatuses, value) {
				writeError(w, http.StatusBadRequest, "invalid group.secondary_values")
				return resolvedIssueTableGroup{}, false
			}
			if _, exists := seenSecondaryValues[value]; exists {
				writeError(w, http.StatusBadRequest, "duplicate group.secondary_values")
				return resolvedIssueTableGroup{}, false
			}
			seenSecondaryValues[value] = struct{}{}
		}
		primary, ok := h.resolveIssueTableGroup(w, r, workspaceID, issueTableGroupSpec{Kind: group.Primary}, false)
		if !ok {
			return resolvedIssueTableGroup{}, false
		}
		return resolvedIssueTableGroup{
			kind:              "compound",
			primary:           &primary,
			secondaryValues:   append([]string(nil), group.SecondaryValues...),
			secondaryFiltered: group.SecondaryValues != nil,
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
	if group.kind == "compound" && group.primary != nil {
		return group.primary.expression(addArg)
	}
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
	if group.kind == "compound" && group.primary != nil {
		return group.primary.sortExpression()
	}
	if group.groupSortExpr != "" {
		return group.groupSortExpr
	}
	return "group_value"
}

func (group resolvedIssueTableGroup) orderExpression(addArg func(any) string) string {
	if group.kind == "compound" && group.primary != nil {
		return group.primary.orderExpression(addArg)
	}
	switch group.kind {
	case "status":
		return "CASE group_value WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'in_review' THEN 3 WHEN 'done' THEN 4 WHEN 'blocked' THEN 5 WHEN 'cancelled' THEN 6 ELSE 7 END"
	case "assignee":
		return "CASE split_part(group_value, ':', 1) WHEN 'member' THEN 0 WHEN 'agent' THEN 1 WHEN 'squad' THEN 2 ELSE 3 END"
	case "project":
		return "CASE WHEN group_value = '__no_project__' THEN 0 ELSE 1 END"
	case "parent":
		return "CASE WHEN group_value = '__no_parent__' THEN 0 ELSE 1 END"
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

func (group resolvedIssueTableGroup) contextExpression(addArg func(any) string, issuePrefix string) string {
	if group.kind == "compound" && group.primary != nil {
		return group.primary.contextExpression(addArg, issuePrefix)
	}
	if group.kind != "parent" {
		return "'{}'::jsonb"
	}
	prefixRef := addArg(issuePrefix)
	return fmt.Sprintf(`CASE WHEN group_value = '__no_parent__' THEN '{}'::jsonb ELSE COALESCE((
  SELECT jsonb_build_object('parent', jsonb_build_object(
    'id', p.id::text,
    'number', p.number,
    'identifier', %s::text || '-' || p.number::text,
    'title', p.title,
    'status', p.status
  ))
  FROM issue p
  WHERE p.workspace_id = $1 AND p.id = group_value::uuid
), '{}'::jsonb) END`, prefixRef)
}

func compoundCellGroupKey(primaryKey, status string) string {
	return "compound:" + base64.RawURLEncoding.EncodeToString([]byte(primaryKey)) + ":status:" + status
}

func (group resolvedIssueTableGroup) descriptor(raw string, count int64, context issueTableGroupContext, secondaryCounts map[string]int64) (issueTableGroupDescriptorResponse, error) {
	if group.kind == "compound" && group.primary != nil {
		descriptor, err := group.primary.descriptor(raw, count, context, nil)
		if err != nil {
			return descriptor, err
		}
		descriptor.SecondaryGroups = make([]issueTableGroupDescriptorResponse, 0, len(secondaryCounts))
		for _, status := range validIssueStatuses {
			statusCount := secondaryCounts[status]
			descriptor.SecondaryGroups = append(descriptor.SecondaryGroups, issueTableGroupDescriptorResponse{
				Key: compoundCellGroupKey(descriptor.Key, status),
				Value: issueTableGroupValueResponse{
					Kind:   "status",
					Status: status,
				},
				Count: statusCount,
			})
		}
		return descriptor, nil
	}
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
	case "project":
		descriptor.Value.Kind = "project"
		if raw == "__no_project__" {
			descriptor.Key = "project:none"
			return descriptor, nil
		}
		if _, err := util.ParseUUID(raw); err != nil {
			return descriptor, fmt.Errorf("unexpected project group value %q", raw)
		}
		descriptor.Key = "project:" + raw
		descriptor.Value.ProjectID = &raw
	case "parent":
		descriptor.Value.Kind = "parent"
		if raw == "__no_parent__" {
			descriptor.Key = "parent:none"
			descriptor.Value.ValueState = "unset"
			return descriptor, nil
		}
		if _, err := util.ParseUUID(raw); err != nil {
			return descriptor, fmt.Errorf("unexpected parent group value %q", raw)
		}
		descriptor.Key = "parent:" + raw
		descriptor.Value.ParentID = &raw
		descriptor.Value.Parent = context.Parent
		if context.Parent == nil {
			descriptor.Value.ValueState = "unavailable"
		} else {
			descriptor.Value.ValueState = "value"
		}
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
			if rawValue != "" {
				descriptor.Value.Value = rawValue
			}
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
	if group.kind == "compound" && group.primary != nil {
		const prefix = "compound:"
		if !strings.HasPrefix(key, prefix) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		encodedAndStatus := strings.TrimPrefix(key, prefix)
		encoded, status, ok := strings.Cut(encodedAndStatus, ":status:")
		if !ok || !issueTableContainsString(validIssueStatuses, status) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		primaryPredicate, ok := group.primary.predicate(w, string(decoded), addArg)
		if !ok {
			return "", false
		}
		return fmt.Sprintf("(%s) AND i.status = %s::text", primaryPredicate, addArg(status)), true
	}
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
	case "project":
		const prefix = "project:"
		if !strings.HasPrefix(key, prefix) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		raw := strings.TrimPrefix(key, prefix)
		if raw == "none" {
			return "i.project_id IS NULL", true
		}
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		return fmt.Sprintf("i.project_id = %s::uuid", addArg(id)), true
	case "parent":
		const prefix = "parent:"
		if !strings.HasPrefix(key, prefix) {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		raw := strings.TrimPrefix(key, prefix)
		if raw == "none" {
			return "i.parent_issue_id IS NULL", true
		}
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_key")
			return "", false
		}
		return fmt.Sprintf("i.parent_issue_id = %s::uuid", addArg(id)), true
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
	contextExpr := group.contextExpression(addArg, h.getIssuePrefix(r.Context(), compiled.workspaceID))
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
	groupedCTE := fmt.Sprintf(`grouped AS (
  SELECT %s AS group_value, COUNT(*)::bigint AS issue_count,
         COUNT(*)::bigint AS visible_count, '{}'::jsonb AS secondary_counts
  FROM issue i
  WHERE %s
  GROUP BY 1
)`, groupExpr, compiled.where)
	if request.Group.IncludeEmpty && group.kind == "property" {
		expectedValues := []string{"unset:"}
		if group.propertyType == "select" {
			expectedValues = append(append([]string(nil), group.activeOptionOrder...), "unset:")
		} else {
			expectedValues = []string{"value:false", "value:true", "unset:"}
		}
		expectedRef := addArg(expectedValues)
		groupedCTE = fmt.Sprintf(`actual AS (
  SELECT %s AS group_value, COUNT(*)::bigint AS issue_count
  FROM issue i
  WHERE %s
  GROUP BY 1
), expected AS (
  SELECT unnest(%s::text[]) AS group_value
), grouped AS (
  SELECT e.group_value, COALESCE(a.issue_count, 0)::bigint AS issue_count,
         COALESCE(a.issue_count, 0)::bigint AS visible_count,
         '{}'::jsonb AS secondary_counts
  FROM expected e
  LEFT JOIN actual a USING (group_value)
  UNION ALL
  SELECT a.group_value, a.issue_count, a.issue_count AS visible_count,
         '{}'::jsonb AS secondary_counts
  FROM actual a
  WHERE NOT (a.group_value = ANY(%s::text[]))
)`, groupExpr, compiled.where, expectedRef, expectedRef)
	}
	if group.kind == "compound" {
		groupedCTE = fmt.Sprintf(`cells AS (
  SELECT %s AS group_value, i.status AS secondary_value, COUNT(*)::bigint AS cell_count
  FROM issue i
  WHERE %s
  GROUP BY 1, 2
), grouped AS (
  SELECT group_value,
         SUM(cell_count)::bigint AS issue_count,
         SUM(cell_count)::bigint AS visible_count,
         jsonb_object_agg(secondary_value, cell_count)::jsonb AS secondary_counts
  FROM cells
  GROUP BY group_value
)`, groupExpr, compiled.where)
		if group.secondaryFiltered {
			visibleRef := addArg(group.secondaryValues)
			headerPredicate := "TRUE"
			promotedParentsCTE := ""
			if group.primary != nil && group.primary.kind == "parent" {
				headerPredicate = `NOT (
    i.parent_issue_id IS NULL AND
    EXISTS (SELECT 1 FROM promoted_parents p WHERE p.id = i.id)
  )`
				promotedParentsCTE = fmt.Sprintf(`, promoted_parents AS (
  SELECT DISTINCT child.parent_issue_id AS id
  FROM membership child
  WHERE child.parent_issue_id IS NOT NULL
    AND child.status = ANY(%s::text[])
)`, visibleRef)
			}
			groupedCTE = fmt.Sprintf(`membership AS NOT MATERIALIZED (
  SELECT i.*
  FROM issue i
  WHERE %s
)%s, cells AS (
  SELECT %s AS group_value, i.status AS secondary_value,
         COUNT(*) FILTER (WHERE %s)::bigint AS cell_count
  FROM membership i
  GROUP BY 1, 2
), grouped AS (
  SELECT group_value,
         SUM(cell_count)::bigint AS issue_count,
         COALESCE(
           SUM(cell_count) FILTER (WHERE secondary_value = ANY(%s::text[])),
           0
         )::bigint AS visible_count,
         jsonb_object_agg(secondary_value, cell_count)::jsonb AS secondary_counts
  FROM cells
  GROUP BY group_value
  HAVING COALESCE(
    SUM(cell_count) FILTER (WHERE secondary_value = ANY(%s::text[])),
    0
  ) > 0
)`, compiled.where, promotedParentsCTE, groupExpr, headerPredicate, visibleRef, visibleRef)
		}
	}
	query := fmt.Sprintf(`WITH %s, sorted AS (
	  SELECT group_value, issue_count, visible_count, secondary_counts, (%s)::text AS group_sort,
	         (%s)::jsonb AS group_context
	  FROM grouped
	), ranked AS (
	  SELECT group_value, issue_count, visible_count, secondary_counts, group_sort, group_context, (%s)::int AS group_order,
	         SUM(visible_count) OVER ()::bigint AS total
	  FROM sorted
	)
	SELECT group_value, issue_count, secondary_counts, group_sort, group_context, group_order, total
	FROM ranked
	WHERE %s
	ORDER BY group_order ASC, group_sort ASC, group_value ASC
	LIMIT %s`, groupedCTE, groupSortExpr, contextExpr, orderExpr, cursorPredicate, limitRef)

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
		var secondaryJSON []byte
		var sortValue string
		var contextJSON []byte
		var order int
		if err := rows.Scan(&raw, &count, &secondaryJSON, &sortValue, &contextJSON, &order, &total); err != nil {
			writeIssueTableQueryFailure(w, r, "failed to list table groups")
			return
		}
		var context issueTableGroupContext
		if len(contextJSON) > 0 {
			if err := json.Unmarshal(contextJSON, &context); err != nil {
				writeIssueTableQueryFailure(w, r, "failed to resolve table group")
				return
			}
		}
		secondaryCounts := map[string]int64{}
		if len(secondaryJSON) > 0 {
			if err := json.Unmarshal(secondaryJSON, &secondaryCounts); err != nil {
				writeIssueTableQueryFailure(w, r, "failed to resolve table group")
				return
			}
		}
		descriptor, err := group.descriptor(raw, count, context, secondaryCounts)
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
