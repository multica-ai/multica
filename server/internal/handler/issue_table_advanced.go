package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// issueTableDueDates are the relative due-date buckets `filters.due_dates`
// and advanced `due_dates` conditions accept. They resolve against
// `filters.today`, the viewer's calendar day, so a saved view never pins a
// date and "today" means the viewer's today rather than the server's.
var issueTableDueDates = []string{"overdue", "today", "this_week", "next_7_days", "none"}

// issueTableFilterFields are the dimensions an advanced condition can test,
// named like their flat `filters` counterparts. A condition can also test a
// custom property as `property:<definitionId>`.
var issueTableFilterFields = []string{
	"statuses", "priorities", "assignees", "creators",
	"project_ids", "project_statuses", "label_ids", "relations", "due_dates",
}

const issueTableFilterPropertyPrefix = "property:"

const (
	issueTableFilterMaxConditions = 50
	issueTableFilterMaxValues     = 500
	issueTableFilterNoneValue     = "none"
)

// issueTableFilterNode is one node of `filters.advanced`: a group when it has
// `children`, otherwise a condition. The root is a group; a group directly
// under the root may hold conditions only, so the tree is at most two levels
// of parentheses deep. Values are strings, except that a property condition
// takes the members `filters.properties` does, operator objects included.
type issueTableFilterNode struct {
	Match    string                 `json:"match,omitempty"`
	Children []issueTableFilterNode `json:"children,omitempty"`
	Field    string                 `json:"field,omitempty"`
	Negate   bool                   `json:"negate,omitempty"`
	Values   []json.RawMessage      `json:"values,omitempty"`
}

func (n issueTableFilterNode) isGroup() bool {
	return n.Field == ""
}

// canonicalIssueTableFilterNode copies the tree with condition values sorted
// and deduplicated, so equivalent trees share a fingerprint.
func canonicalIssueTableFilterNode(node *issueTableFilterNode) *issueTableFilterNode {
	if node == nil {
		return nil
	}
	out := issueTableFilterNode{
		Match:  node.Match,
		Field:  node.Field,
		Negate: node.Negate,
		Values: sortedUniqueRawJSON(node.Values),
	}
	if len(node.Children) > 0 {
		out.Children = make([]issueTableFilterNode, len(node.Children))
		for i := range node.Children {
			out.Children[i] = *canonicalIssueTableFilterNode(&node.Children[i])
		}
	}
	return &out
}

func parseIssueTableToday(raw string) (time.Time, bool) {
	today, err := time.Parse(time.DateOnly, raw)
	return today, err == nil
}

// issueTableDueDatePredicate matches one relative due-date bucket. Weeks run
// Monday to Sunday.
func issueTableDueDatePredicate(bucket string, today time.Time, addArg func(any) string) string {
	switch bucket {
	case "overdue":
		return fmt.Sprintf("i.due_date < %s::date", addArg(today))
	case "today":
		return fmt.Sprintf("i.due_date = %s::date", addArg(today))
	case "this_week":
		offset := (int(today.Weekday()) + 6) % 7
		monday := today.AddDate(0, 0, -offset)
		return fmt.Sprintf("(i.due_date >= %s::date AND i.due_date < %s::date)", addArg(monday), addArg(monday.AddDate(0, 0, 7)))
	case "next_7_days":
		return fmt.Sprintf("(i.due_date >= %s::date AND i.due_date < %s::date)", addArg(today), addArg(today.AddDate(0, 0, 7)))
	default: // none
		return "i.due_date IS NULL"
	}
}

// issueTableFilterCompiler turns `filters.advanced` into one SQL predicate.
// The user and the viewer's day are resolved only when a condition needs
// them, so a tree without relations or due dates does not require them.
type issueTableFilterCompiler struct {
	addArg     func(any) string
	userRef    func() (string, error)
	today      func() (time.Time, error)
	conditions int
}

type issueTableFilterError struct {
	status  int
	message string
}

func (e issueTableFilterError) Error() string { return e.message }

func invalidIssueTableFilter(format string, args ...any) error {
	return issueTableFilterError{status: http.StatusBadRequest, message: fmt.Sprintf(format, args...)}
}

// compile returns "" for a tree that constrains nothing.
func (c *issueTableFilterCompiler) compile(root issueTableFilterNode) (string, error) {
	if !root.isGroup() {
		return "", invalidIssueTableFilter("invalid filters.advanced: root must be a group")
	}
	return c.group(root, 0)
}

func (c *issueTableFilterCompiler) group(node issueTableFilterNode, depth int) (string, error) {
	joiner := " AND "
	switch node.Match {
	case "", "all":
	case "any":
		joiner = " OR "
	default:
		return "", invalidIssueTableFilter("invalid filters.advanced.match")
	}
	parts := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		var (
			predicate string
			err       error
		)
		if child.isGroup() {
			if depth > 0 {
				return "", invalidIssueTableFilter("invalid filters.advanced: groups nest one level deep")
			}
			predicate, err = c.group(child, depth+1)
		} else {
			predicate, err = c.condition(child)
		}
		if err != nil {
			return "", err
		}
		// Empty conditions and groups drop out rather than reading as TRUE,
		// which would make an "any" group match everything.
		if predicate != "" {
			parts = append(parts, predicate)
		}
	}
	switch len(parts) {
	case 0:
		return "", nil
	case 1:
		return parts[0], nil
	default:
		return "(" + strings.Join(parts, joiner) + ")", nil
	}
}

func (c *issueTableFilterCompiler) condition(node issueTableFilterNode) (string, error) {
	propertyID, isProperty := strings.CutPrefix(node.Field, issueTableFilterPropertyPrefix)
	if !isProperty && !issueTableContainsString(issueTableFilterFields, node.Field) {
		return "", invalidIssueTableFilter("invalid filters.advanced.field")
	}
	if len(node.Children) > 0 || node.Match != "" {
		return "", invalidIssueTableFilter("invalid filters.advanced: a condition has no children")
	}
	if len(node.Values) == 0 {
		return "", nil
	}
	if len(node.Values) > issueTableFilterMaxValues {
		return "", invalidIssueTableFilter("invalid filters.advanced: too many values")
	}
	c.conditions++
	if c.conditions > issueTableFilterMaxConditions {
		return "", invalidIssueTableFilter("invalid filters.advanced: too many conditions")
	}
	var (
		predicate string
		err       error
	)
	if isProperty {
		predicate, err = c.propertyPredicate(propertyID, node.Values)
	} else {
		values := make([]string, 0, len(node.Values))
		for _, raw := range node.Values {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return "", invalidIssueTableFilter("invalid filters.advanced values for %s", node.Field)
			}
			values = append(values, value)
		}
		predicate, err = c.fieldPredicate(node.Field, sortedUniqueStrings(values))
	}
	if err != nil {
		return "", err
	}
	if node.Negate {
		return "(" + predicate + ") IS NOT TRUE", nil
	}
	return predicate, nil
}

// propertyPredicate compiles a property condition exactly like the same
// members under `filters.properties`.
func (c *issueTableFilterCompiler) propertyPredicate(propertyID string, values []json.RawMessage) (string, error) {
	groups, err := compilePropertiesFilter(map[string][]json.RawMessage{propertyID: values})
	if err != nil {
		return "", invalidIssueTableFilter("invalid filters.advanced: %s", err.Error())
	}
	if len(groups) == 0 {
		return "", nil
	}
	return propertiesFilterPredicate(groups, c.addArg), nil
}

func (c *issueTableFilterCompiler) fieldPredicate(field string, values []string) (string, error) {
	invalid := invalidIssueTableFilter("invalid filters.advanced values for %s", field)
	ors := make([]string, 0, len(values))
	switch field {
	case "statuses":
		for _, status := range values {
			if len(status) > 64 {
				return "", invalid
			}
		}
		return fmt.Sprintf("i.status = ANY(%s::text[])", c.addArg(values)), nil
	case "priorities":
		for _, priority := range values {
			if !issueTableContainsString(validIssuePriorities, priority) {
				return "", invalid
			}
		}
		return fmt.Sprintf("i.priority = ANY(%s::text[])", c.addArg(values)), nil
	case "assignees", "creators":
		column := "assignee"
		if field == "creators" {
			column = "creator"
		}
		for _, value := range values {
			if value == issueTableFilterNoneValue && field == "assignees" {
				ors = append(ors, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
				continue
			}
			actorType, rawID, ok := strings.Cut(value, ":")
			if !ok || !isIssueActorType(actorType) {
				return "", invalid
			}
			id, err := util.ParseUUID(rawID)
			if err != nil {
				return "", invalid
			}
			ors = append(ors, fmt.Sprintf("(i.%[1]s_type = %[2]s::text AND i.%[1]s_id = %[3]s::uuid)", column, c.addArg(actorType), c.addArg(id)))
		}
	case "project_ids":
		ids := make([]pgtype.UUID, 0, len(values))
		for _, value := range values {
			if value == issueTableFilterNoneValue {
				ors = append(ors, "i.project_id IS NULL")
				continue
			}
			id, err := util.ParseUUID(value)
			if err != nil {
				return "", invalid
			}
			ids = append(ids, id)
		}
		if len(ids) > 0 {
			ors = append(ors, fmt.Sprintf("i.project_id = ANY(%s::uuid[])", c.addArg(ids)))
		}
	case "project_statuses":
		for _, status := range values {
			if !issueTableContainsString(validProjectStatuses, status) {
				return "", invalid
			}
		}
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM project p WHERE p.id = i.project_id AND p.workspace_id = i.workspace_id AND p.status = ANY(%s::text[]))",
			c.addArg(values),
		), nil
	case "label_ids":
		ids := make([]pgtype.UUID, 0, len(values))
		for _, value := range values {
			id, err := util.ParseUUID(value)
			if err != nil {
				return "", invalid
			}
			ids = append(ids, id)
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM issue_to_label itl WHERE itl.issue_id = i.id AND itl.label_id = ANY(%s::uuid[]))", c.addArg(ids)), nil
	case "relations":
		for _, relation := range values {
			if !issueTableContainsString(issueTableRelations, relation) {
				return "", invalid
			}
		}
		userRef, err := c.userRef()
		if err != nil {
			return "", err
		}
		for _, relation := range values {
			ors = append(ors, issueTableRelationPredicate(relation, userRef))
		}
	case "due_dates":
		for _, bucket := range values {
			if !issueTableContainsString(issueTableDueDates, bucket) {
				return "", invalid
			}
		}
		today, err := c.today()
		if err != nil {
			return "", err
		}
		for _, bucket := range values {
			ors = append(ors, issueTableDueDatePredicate(bucket, today, c.addArg))
		}
	}
	return "(" + strings.Join(ors, " OR ") + ")", nil
}
