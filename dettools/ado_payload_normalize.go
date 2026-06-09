package step

import (
	"regexp"
	"strings"
)

var htmlTag = regexp.MustCompile(`<[^>]+>`)

// Run normalizes already-fetched Azure DevOps payloads. It does not call ADO.
//
// Input:
//   - work_item: object from az boards work-item show.
//   - comments_response: object with a value array from the comments API.
//   - child_items_response: object with a value array from workitemsbatch.
//   - ancestors: optional array of parent work item objects, nearest first.
//
// Output machine_data includes normalized work_item, comments, active_child_tasks,
// ancestor_chain, and nearest_component.
func Run(input map[string]any) map[string]any {
	workItem := normalizeWorkItem(object(input["work_item"]), 0)
	comments := normalizeComments(object(input["comments_response"]))
	children := normalizeChildren(object(input["child_items_response"]))
	ancestorChain, component := normalizeAncestors(array(input["ancestors"]))

	return map[string]any{
		"status":  "ok",
		"summary": "Normalized ADO payloads",
		"machine_data": map[string]any{
			"work_item":          workItem,
			"comments":           comments,
			"active_child_tasks": children,
			"ancestor_chain":     ancestorChain,
			"nearest_component":  component,
		},
	}
}

func normalizeWorkItem(item map[string]any, depth int) map[string]any {
	fields := object(item["fields"])
	return map[string]any{
		"id":                  numberOrString(item["id"]),
		"depth":               depth,
		"type":                str(fields["System.WorkItemType"]),
		"title":               str(fields["System.Title"]),
		"description":         stripHTML(str(fields["System.Description"])),
		"acceptance_criteria": splitAcceptanceCriteria(str(fields["Microsoft.VSTS.Common.AcceptanceCriteria"])),
		"state":               str(fields["System.State"]),
		"area_path":           str(fields["System.AreaPath"]),
		"iteration_path":      str(fields["System.IterationPath"]),
	}
}

func normalizeComments(resp map[string]any) []any {
	values := array(resp["value"])
	out := make([]any, 0, len(values))
	for _, raw := range values {
		comment := object(raw)
		createdBy := object(comment["createdBy"])
		out = append(out, map[string]any{
			"author":       str(createdBy["displayName"]),
			"created_date": str(comment["createdDate"]),
			"text":         stripHTML(str(comment["text"])),
		})
	}
	return out
}

func normalizeChildren(resp map[string]any) []any {
	values := array(resp["value"])
	out := []any{}
	for _, raw := range values {
		item := normalizeWorkItem(object(raw), 0)
		if strings.EqualFold(str(item["type"]), "Task") && !closedState(str(item["state"])) {
			out = append(out, item)
		}
	}
	return out
}

func normalizeAncestors(items []any) ([]any, any) {
	chain := make([]any, 0, len(items))
	var component any
	for i, raw := range items {
		item := normalizeWorkItem(object(raw), i+1)
		chain = append(chain, item)
		title := strings.ToLower(strings.TrimSpace(str(item["title"])))
		typ := strings.ToLower(strings.TrimSpace(str(item["type"])))
		if component == nil && (typ == "component" || strings.HasPrefix(title, "component:")) {
			component = item
		}
	}
	if component == nil {
		component = nil
	}
	return chain, component
}

func splitAcceptanceCriteria(html string) []any {
	if strings.TrimSpace(html) == "" {
		return []any{}
	}
	parts := strings.Split(html, "</li>")
	out := []any{}
	for _, part := range parts {
		text := stripHTML(part)
		text = strings.Trim(text, " \t\r\n-•")
		if text != "" {
			out = append(out, text)
		}
	}
	if len(out) == 0 {
		text := stripHTML(html)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = htmlTag.ReplaceAllString(s, "")
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return strings.TrimSpace(replacer.Replace(s))
}

func closedState(state string) bool {
	return strings.EqualFold(state, "Done") || strings.EqualFold(state, "Closed")
}

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func numberOrString(v any) any {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		return n
	default:
		return nil
	}
}
