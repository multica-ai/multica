package issuepropertytools

const (
	ListName  = "list_issue_properties"
	SetName   = "set_issue_property"
	UnsetName = "unset_issue_property"
)

func ListDescription() string {
	return "List custom property definitions and current values for a Multica issue. Returns option IDs required by select properties."
}

func SetDescription() string {
	return "Set one custom property value on a Multica issue. The property accepts a UUID or case-insensitive name; the value must match the listed property type."
}

func UnsetDescription() string {
	return "Remove one custom property value from a Multica issue. The property accepts a UUID or case-insensitive name."
}

func ListSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]any{"type": "string", "description": "Issue UUID or identifier, for example FIR-3447."},
		},
	}
}

func SetSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "property", "value"},
		"properties": map[string]any{
			"issue_id": map[string]any{"type": "string", "description": "Issue UUID or identifier, for example FIR-3447."},
			"property": map[string]any{"type": "string", "description": "Property UUID or case-insensitive property name."},
			"value":    map[string]any{"description": "Typed JSON value. Call list_issue_properties first to discover the type and select option IDs."},
		},
	}
}

func UnsetSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "property"},
		"properties": map[string]any{
			"issue_id": map[string]any{"type": "string", "description": "Issue UUID or identifier, for example FIR-3447."},
			"property": map[string]any{"type": "string", "description": "Property UUID or case-insensitive property name."},
		},
	}
}
