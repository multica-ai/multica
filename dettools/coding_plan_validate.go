package step

import "strings"

// Run validates the structured implementation_plan artifact produced by the
// Coding Team Planner. It checks objective shape and safety only; it does not
// judge semantic implementation correctness.
//
// Input:
//   - plan: implementation_plan artifact object. If an envelope with data is
//     supplied, data is validated.
//   - acceptance_criteria: optional []string from the task issue; every item
//     must appear in acceptance_criteria_coverage[].criterion.
//
// Output machine_data:
//   - valid: bool
//   - errors: []string
//   - warnings: []string
func Run(input map[string]any) map[string]any {
	plan := object(input["plan"])
	if data := object(plan["data"]); len(data) > 0 {
		plan = data
	}
	criteria := stringSlice(input["acceptance_criteria"])
	errors := []any{}
	warnings := []any{}

	if str(plan["artifact_type"]) != "implementation_plan" {
		errors = append(errors, "artifact_type must be implementation_plan")
	}
	if number(plan["artifact_version"]) <= 0 {
		errors = append(errors, "artifact_version must be positive")
	}
	requireString(plan, "task_issue_id", &errors)
	requireString(plan, "master_issue_id", &errors)
	language := str(plan["language"])
	if language != "python" && language != "csharp" && language != "unknown" {
		errors = append(errors, "language must be python, csharp, or unknown")
	}
	owningProject := str(plan["owning_project"])
	if owningProject == "" {
		errors = append(errors, "owning_project is required")
	} else if unsafePath(owningProject) {
		errors = append(errors, "owning_project must be a safe relative path")
	}
	requireString(plan, "owning_project_justification", &errors)
	if len(stringSlice(plan["key_decisions"])) == 0 {
		errors = append(errors, "key_decisions must contain at least one decision")
	}

	filesToCreate := stringSlice(plan["files_to_create"])
	filesToModify := stringSlice(plan["files_to_modify"])
	if len(filesToCreate)+len(filesToModify) == 0 {
		errors = append(errors, "at least one file must appear in files_to_create or files_to_modify")
	}
	for _, path := range append(filesToCreate, filesToModify...) {
		if unsafePath(path) {
			errors = append(errors, "unsafe file path: "+path)
			continue
		}
		if owningProject != "" && !strings.HasPrefix(path, strings.TrimSuffix(owningProject, "/")+"/") && path != owningProject {
			warnings = append(warnings, "file is outside owning_project and needs explicit justification: "+path)
		}
	}

	coverage := array(plan["acceptance_criteria_coverage"])
	covered := map[string]bool{}
	for i, raw := range coverage {
		item := object(raw)
		criterion := str(item["criterion"])
		if criterion == "" {
			errors = append(errors, "acceptance_criteria_coverage entry has empty criterion")
			continue
		}
		if str(item["planned_coverage"]) == "" && len(stringSlice(item["tests"])) == 0 {
			errors = append(errors, "acceptance_criteria_coverage entry lacks planned coverage for criterion: "+criterion)
		}
		covered[criterion] = true
		_ = i
	}
	for _, criterion := range criteria {
		if !covered[criterion] {
			errors = append(errors, "missing acceptance criteria coverage: "+criterion)
		}
	}

	data := map[string]any{"valid": len(errors) == 0, "errors": errors, "warnings": warnings}
	if len(errors) > 0 {
		return map[string]any{
			"status":       "error",
			"error_code":   "POLICY_FAILURE",
			"summary":      "Implementation plan artifact failed validation",
			"machine_data": data,
		}
	}
	return map[string]any{"status": "ok", "summary": "Implementation plan artifact is valid", "machine_data": data}
}

func requireString(plan map[string]any, key string, errors *[]any) {
	if str(plan[key]) == "" {
		*errors = append(*errors, key+" is required")
	}
}

func unsafePath(path string) bool {
	path = strings.TrimSpace(path)
	return path == "" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") || strings.Contains(path, "..") || strings.Contains(path, "\\")
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
		return strings.TrimSpace(s)
	}
	return ""
}

func number(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return 0
}

func stringSlice(v any) []string {
	items := array(v)
	out := []string{}
	for _, item := range items {
		if s := str(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}
