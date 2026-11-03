package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A saved view's `query` is written by the web client. The translation below
// mirrors viewSidebarRequest in packages/core/issues/sidebar-list.ts (with the
// sanitizing of baselineFromQuery and sanitizeAdvancedFilter), so a view lists
// the same issues here as its sidebar entry does. Values the client would drop
// are dropped here too; values the client would send but the table query
// rejects are left for the table query to reject.

// issueViewFilterDimensions are the client's negatable dimensions in its
// canonical order, with their table query field.
var issueViewFilterDimensions = []struct {
	dimension string
	field     string
}{
	{"status", "statuses"},
	{"priority", "priorities"},
	{"assignee", "assignees"},
	{"creator", "creators"},
	{"project", "project_ids"},
	{"projectStatus", "project_statuses"},
	{"label", "label_ids"},
	{"relation", "relations"},
	{"dueDate", "due_dates"},
}

var issueViewSorts = []string{
	"position", "status", "priority", "title", "created_at", "updated_at",
	"last_activity", "start_date", "due_date",
}

var issueViewPropertyFilterOps = []string{"contains", "gt", "gte", "lt", "lte", "before", "after"}

var issueViewActorTypes = []string{"member", "agent", "squad"}

var errIssueViewScopeUnlistable = errors.New("this view's scope cannot be listed")

func issueViewFilterField(dimension string) (string, bool) {
	for _, d := range issueViewFilterDimensions {
		if d.dimension == dimension {
			return d.field, true
		}
	}
	return "", false
}

func decodeIssueViewObject(raw []byte) map[string]json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	return object
}

func issueViewBool(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func issueViewString(raw json.RawMessage) (string, bool) {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func issueViewArray(raw json.RawMessage) []json.RawMessage {
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

// issueViewStrings keeps the string members of a JSON array that pass keep.
func issueViewStrings(raw json.RawMessage, keep func(string) bool) []string {
	var result []string
	for _, member := range issueViewArray(raw) {
		if value, ok := issueViewString(member); ok && keep(value) {
			result = append(result, value)
		}
	}
	return result
}

func issueViewUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func issueViewOneOf(allowed []string) func(string) bool {
	return func(value string) bool { return issueTableContainsString(allowed, value) }
}

func issueViewNonEmpty(value string) bool { return value != "" }

func issueViewAny(string) bool { return true }

// issueViewActors keeps array members that are objects with a string id.
func issueViewActors(raw json.RawMessage) []issueTableActorRef {
	var result []issueTableActorRef
	for _, member := range issueViewArray(raw) {
		object := decodeIssueViewObject(member)
		if object == nil {
			continue
		}
		id, ok := issueViewString(object["id"])
		if !ok {
			continue
		}
		actorType, _ := issueViewString(object["type"])
		result = append(result, issueTableActorRef{Type: actorType, ID: id})
	}
	return result
}

// issueViewPropertyMember returns one property filter member in table query
// form plus its dedup key: a string, or an operator object with a known op.
func issueViewPropertyMember(raw json.RawMessage, allowEmpty bool) (json.RawMessage, string, bool) {
	if value, ok := issueViewString(raw); ok {
		if value == "" && !allowEmpty {
			return nil, "", false
		}
		encoded, _ := json.Marshal(value)
		return encoded, value, true
	}
	object := decodeIssueViewObject(raw)
	if object == nil {
		return nil, "", false
	}
	op, opOK := issueViewString(object["op"])
	value, valueOK := issueViewString(object["value"])
	if !opOK || !valueOK || !issueTableContainsString(issueViewPropertyFilterOps, op) {
		return nil, "", false
	}
	encoded, _ := json.Marshal(map[string]string{"op": op, "value": value})
	return encoded, op + "\x00" + value, true
}

func issueViewPropertyFilters(raw json.RawMessage) map[string][]json.RawMessage {
	object := decodeIssueViewObject(raw)
	result := make(map[string][]json.RawMessage, len(object))
	for propertyID, membersRaw := range object {
		var members []json.RawMessage
		for _, member := range issueViewArray(membersRaw) {
			if encoded, _, ok := issueViewPropertyMember(member, true); ok {
				members = append(members, encoded)
			}
		}
		if len(members) > 0 {
			result[propertyID] = members
		}
	}
	return result
}

// issueViewConditionValues keeps the advanced condition values the client
// can represent for a built-in dimension.
func issueViewConditionValues(dimension string, values []string) []string {
	switch dimension {
	case "priority":
		return issueViewKeep(values, issueViewOneOf(validIssuePriorities))
	case "projectStatus":
		return issueViewKeep(values, issueViewOneOf(validProjectStatuses))
	case "relation":
		return issueViewKeep(values, issueViewOneOf(issueTableRelations))
	case "dueDate":
		return issueViewKeep(values, issueViewOneOf(issueTableDueDates))
	case "assignee", "creator":
		return issueViewKeep(values, func(value string) bool {
			if value == issueTableFilterNoneValue {
				return dimension == "assignee"
			}
			parts := strings.Split(value, ":")
			return len(parts) >= 2 && parts[0] != "" && parts[1] != "" &&
				issueTableContainsString(issueViewActorTypes, parts[0])
		})
	default:
		return issueViewKeep(values, issueViewNonEmpty)
	}
}

func issueViewKeep(values []string, keep func(string) bool) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if keep(value) {
			result = append(result, value)
		}
	}
	return result
}

// issueViewAdvancedNode sanitizes, prunes and converts one advanced filter
// node: the root group is depth 0 and may hold groups, a nested group holds
// conditions only. Nodes the client would drop, conditions without values and
// groups left empty return ok=false.
func issueViewAdvancedNode(raw json.RawMessage, depth int) (issueTableFilterNode, bool) {
	object := decodeIssueViewObject(raw)
	if object == nil {
		return issueTableFilterNode{}, false
	}
	nodeType, _ := issueViewString(object["type"])
	switch nodeType {
	case "group":
		var children []json.RawMessage
		if depth > 1 || json.Unmarshal(object["children"], &children) != nil || children == nil {
			return issueTableFilterNode{}, false
		}
		match := "all"
		if value, _ := issueViewString(object["match"]); value == "any" {
			match = "any"
		}
		group := issueTableFilterNode{Match: match}
		for _, child := range children {
			if node, ok := issueViewAdvancedNode(child, depth+1); ok {
				group.Children = append(group.Children, node)
			}
		}
		return group, len(group.Children) > 0
	case "condition":
		negate := issueViewBool(object["negated"])
		field, _ := issueViewString(object["field"])
		if len(field) > len(issueTableFilterPropertyPrefix) && strings.HasPrefix(field, issueTableFilterPropertyPrefix) {
			seen := map[string]struct{}{}
			var values []json.RawMessage
			for _, member := range issueViewArray(object["values"]) {
				encoded, key, ok := issueViewPropertyMember(member, false)
				if !ok {
					continue
				}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				values = append(values, encoded)
			}
			return issueTableFilterNode{Field: field, Negate: negate, Values: values}, len(values) > 0
		}
		tableField, ok := issueViewFilterField(field)
		if !ok {
			return issueTableFilterNode{}, false
		}
		strs := issueViewConditionValues(field, issueViewUniqueStrings(issueViewStrings(object["values"], issueViewAny)))
		values := make([]json.RawMessage, len(strs))
		for i, value := range strs {
			values[i], _ = json.Marshal(value)
		}
		return issueTableFilterNode{Field: tableField, Negate: negate, Values: values}, len(values) > 0
	default:
		return issueTableFilterNode{}, false
	}
}

func issueViewAdvancedUsesField(node issueTableFilterNode, field string) bool {
	if !node.isGroup() {
		return node.Field == field
	}
	for _, child := range node.Children {
		if issueViewAdvancedUsesField(child, field) {
			return true
		}
	}
	return false
}

func issueViewTableScope(view db.IssueView) (issueTableScope, error) {
	var assigneeTypes []string
	switch view.ScopeVariant.String {
	case "members":
		assigneeTypes = []string{"member"}
	case "agents":
		assigneeTypes = []string{"agent", "squad"}
	}
	switch view.ScopeType {
	case "workspace":
		return issueTableScope{Kind: "workspace", AssigneeTypes: assigneeTypes}, nil
	case "project":
		if !view.ScopeID.Valid {
			return issueTableScope{}, errIssueViewScopeUnlistable
		}
		return issueTableScope{Kind: "project", ProjectID: uuidToString(view.ScopeID), AssigneeTypes: assigneeTypes}, nil
	case "my":
		relation := "any"
		if view.ScopeVariant.Valid {
			relation = view.ScopeVariant.String
		}
		switch relation {
		case "assigned", "created", "involved", "any":
			return issueTableScope{Kind: "my", Relation: relation}, nil
		}
	}
	return issueTableScope{}, errIssueViewScopeUnlistable
}

// issueViewTableQuery is the table query listing what a saved view shows.
// today is the viewer's calendar day, used only by due-date conditions.
func issueViewTableQuery(view db.IssueView, today string) (issueTableQuerySpec, error) {
	scope, err := issueViewTableScope(view)
	if err != nil {
		return issueTableQuerySpec{}, err
	}
	query := decodeIssueViewObject(view.Query)
	display := decodeIssueViewObject(view.Display)

	statuses := issueViewStrings(query["statusFilters"], issueViewNonEmpty)
	priorities := issueViewStrings(query["priorityFilters"], issueViewOneOf(validIssuePriorities))
	assignees := issueViewActors(query["assigneeFilters"])
	includeNoAssignee := issueViewBool(query["includeNoAssignee"])
	creators := issueViewActors(query["creatorFilters"])
	projectIDs := issueViewStrings(query["projectFilters"], issueViewAny)
	includeNoProject := issueViewBool(query["includeNoProject"])
	projectStatuses := issueViewStrings(query["projectStatusFilters"], issueViewOneOf(validProjectStatuses))
	labelIDs := issueViewStrings(query["labelFilters"], issueViewAny)
	relations := issueViewStrings(query["relationFilters"], issueViewOneOf(issueTableRelations))
	dueDates := issueViewUniqueStrings(issueViewStrings(query["dueDateFilters"], issueViewOneOf(issueTableDueDates)))
	negated := issueViewStrings(query["negatedFilters"], func(dimension string) bool {
		_, ok := issueViewFilterField(dimension)
		return ok
	})
	// Same as the page the view opens: a project view drops the project
	// dimension, a "my" view the viewer-relation one.
	switch scope.Kind {
	case "project":
		projectIDs, includeNoProject = nil, false
	case "my":
		relations = nil
	}

	hasSelection := map[string]bool{
		"status":        len(statuses) > 0,
		"priority":      len(priorities) > 0,
		"assignee":      len(assignees) > 0 || includeNoAssignee,
		"creator":       len(creators) > 0,
		"project":       len(projectIDs) > 0 || includeNoProject,
		"projectStatus": len(projectStatuses) > 0,
		"label":         len(labelIDs) > 0,
		"relation":      len(relations) > 0,
		"dueDate":       len(dueDates) > 0,
	}
	var negate []string
	for _, d := range issueViewFilterDimensions {
		if hasSelection[d.dimension] && issueTableContainsString(negated, d.dimension) {
			negate = append(negate, d.field)
		}
	}

	filters := issueTableFiltersRequest{
		Statuses:          statuses,
		Priorities:        priorities,
		IncludeNoAssignee: includeNoAssignee,
		Creators:          creators,
		ProjectIDs:        projectIDs,
		IncludeNoProject:  includeNoProject,
		ProjectStatuses:   projectStatuses,
		LabelIDs:          labelIDs,
		Relations:         relations,
		DueDates:          dueDates,
		Negate:            negate,
		WorkingOnly:       issueViewBool(query["agentRunningFilter"]),
	}
	// An explicitly empty assignee list means "match none" to the table query.
	if len(assignees) > 0 {
		filters.Assignees = assignees
	}
	usesDueDates := len(dueDates) > 0
	if advanced, ok := issueViewAdvancedNode(query["advancedFilter"], 0); ok && advanced.isGroup() {
		filters.Advanced = &advanced
		usesDueDates = usesDueDates || issueViewAdvancedUsesField(advanced, "due_dates")
	}
	if usesDueDates {
		filters.Today = today
	}
	if properties := issueViewPropertyFilters(query["propertyFilters"]); len(properties) > 0 {
		filters.Properties = properties
	}
	showSubIssues := true
	if raw, ok := display["showSubIssues"]; ok {
		var value bool
		if json.Unmarshal(raw, &value) == nil && !value {
			showSubIssues = false
		}
	}
	filters.IncludeSubIssues = &showSubIssues

	sort := issueTableSortRequest{Field: "position", Direction: "asc"}
	if field, ok := issueViewString(display["sortBy"]); ok &&
		(issueTableContainsString(issueViewSorts, field) || strings.HasPrefix(field, issueTableFilterPropertyPrefix)) {
		sort.Field = field
	}
	if direction, _ := issueViewString(display["sortDirection"]); direction == "desc" {
		sort.Direction = "desc"
	}

	return issueTableQuerySpec{Scope: scope, Filters: filters, Sort: sort}, nil
}

type issueViewIssuesResponse struct {
	Issues []IssueResponse `json:"issues"`
	// Total counts every issue the view shows; only the first page carries it.
	Total      *int64  `json:"total,omitempty"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// ListIssueViewIssues lists the issues a saved view shows, in the view's
// saved order, under the caller's identity: "my" scopes and viewer relations
// resolve against the requesting user.
func (h *Handler) ListIssueViewIssues(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	view, _, ok := h.loadIssueViewForUser(w, r, userID)
	if !ok {
		return
	}

	params := r.URL.Query()
	today := time.Now().UTC().Format(time.DateOnly)
	if raw := params.Get("today"); raw != "" {
		if _, ok := parseIssueTableToday(raw); !ok {
			writeError(w, http.StatusBadRequest, "today must be a YYYY-MM-DD date")
			return
		}
		today = raw
	}
	limit := issueTableDefaultPageSize
	if raw := params.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > issueTableMaxPageSize {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", issueTableMaxPageSize))
			return
		}
		limit = parsed
	}
	page := issueTablePageRequest{Limit: limit}
	if raw := params.Get("cursor"); raw != "" {
		page.Cursor = &raw
	}

	spec, err := issueViewTableQuery(view, today)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, ok := h.queryIssueTableRows(w, r, issueTableRowsRequest{
		Query: spec,
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  page,
	})
	if !ok {
		return
	}

	response := issueViewIssuesResponse{
		Issues:     make([]IssueResponse, len(rows.Rows)),
		NextCursor: rows.NextCursor,
		HasMore:    rows.NextCursor != nil,
	}
	for i, row := range rows.Rows {
		response.Issues[i] = row.Issue
	}
	if page.Cursor == nil {
		total := rows.Total
		response.Total = &total
	}
	writeJSON(w, http.StatusOK, response)
}
