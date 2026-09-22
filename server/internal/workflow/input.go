package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

type workflowInputField struct {
	Key       string
	Type      string
	Required  bool
	MaxLength int
	NodeID    string
}

func startInputFields(node Node) ([]workflowInputField, []ValidationDetail) {
	if node.Config == nil {
		return nil, nil
	}
	raw, exists := node.Config["fields"]
	if !exists || raw == nil {
		return nil, nil
	}
	var values []any
	switch fields := raw.(type) {
	case []any:
		values = fields
	case []map[string]any:
		values = make([]any, len(fields))
		for i := range fields {
			values[i] = fields[i]
		}
	default:
		return nil, []ValidationDetail{nodeDetail("invalid_input_fields", node.ID, "config.fields", "input fields must be an array")}
	}
	fields := make([]workflowInputField, 0, len(values))
	seen := map[string]bool{}
	details := make([]ValidationDetail, 0)
	for _, rawField := range values {
		field, ok := rawField.(map[string]any)
		if !ok {
			details = append(details, nodeDetail("invalid_input_field", node.ID, "config.fields", "each input field must be an object"))
			continue
		}
		key, _ := field["key"].(string)
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 100 || strings.ContainsAny(key, "./\\") {
			details = append(details, nodeDetail("invalid_input_field", node.ID, "config.fields", "input field keys must be non-empty and cannot contain path separators"))
			continue
		}
		if seen[key] {
			details = append(details, nodeDetail("duplicate_input_field", node.ID, "config.fields", "input field keys must be unique"))
			continue
		}
		seen[key] = true
		typeName, _ := field["type"].(string)
		if typeName == "" {
			typeName = "text"
		}
		switch typeName {
		case "text", "string", "number", "integer", "boolean", "json", "attachment", "attachments", "array":
		default:
			details = append(details, nodeDetail("invalid_input_type", node.ID, "config.fields."+key+".type", "supported input types are text, number, integer, boolean, json, and attachment"))
			continue
		}
		maxLength := 0
		switch value := field["max_length"].(type) {
		case float64:
			if value != math.Trunc(value) || value < 0 || value > 100000 {
				details = append(details, nodeDetail("invalid_input_length", node.ID, "config.fields."+key+".max_length", "max_length must be a non-negative integer no greater than 100000"))
			} else {
				maxLength = int(value)
			}
		case int:
			if value < 0 || value > 100000 {
				details = append(details, nodeDetail("invalid_input_length", node.ID, "config.fields."+key+".max_length", "max_length must be a non-negative integer no greater than 100000"))
			} else {
				maxLength = value
			}
		case nil:
		default:
			details = append(details, nodeDetail("invalid_input_length", node.ID, "config.fields."+key+".max_length", "max_length must be a non-negative integer no greater than 100000"))
		}
		required, _ := field["required"].(bool)
		fields = append(fields, workflowInputField{Key: key, Type: typeName, Required: required, MaxLength: maxLength, NodeID: node.ID})
	}
	return fields, details
}

func validateRunInput(graph Graph, input string) error {
	var start Node
	for _, node := range graph.Nodes {
		if node.Type == "start" {
			start = node
			break
		}
	}
	fields, details := startInputFields(start)
	if len(details) > 0 {
		return validationError(details)
	}
	if len(fields) == 0 {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(input), &values); err != nil || values == nil {
		return validationError([]ValidationDetail{{Code: "input_invalid", NodeID: start.ID, Field: "input_values", Message: "start inputs must be a JSON object"}})
	}
	known := make(map[string]workflowInputField, len(fields))
	for _, field := range fields {
		known[field.Key] = field
		value, exists := values[field.Key]
		if field.Required && (!exists || value == nil || ((field.Type == "text" || field.Type == "string") && strings.TrimSpace(fmt.Sprint(value)) == "")) {
			details = append(details, ValidationDetail{Code: "input_required", NodeID: field.NodeID, Field: "input_values." + field.Key, Message: "required start input is missing"})
			continue
		}
		if !exists || value == nil {
			continue
		}
		if field.MaxLength > 0 && (field.Type == "text" || field.Type == "string") {
			text, ok := value.(string)
			if !ok || utf8.RuneCountInString(text) > field.MaxLength {
				details = append(details, ValidationDetail{Code: "input_invalid", NodeID: field.NodeID, Field: "input_values." + field.Key, Message: "text input is invalid or exceeds max_length"})
				continue
			}
		}
		if !validInputType(field.Type, value) {
			details = append(details, ValidationDetail{Code: "input_invalid", NodeID: field.NodeID, Field: "input_values." + field.Key, Message: "input value does not match the declared type"})
		}
	}
	for key := range values {
		if _, ok := known[key]; !ok {
			details = append(details, ValidationDetail{Code: "unknown_input_field", NodeID: start.ID, Field: "input_values." + key, Message: "input field is not declared by Start"})
		}
	}
	return validationError(details)
}

func validInputType(typeName string, value any) bool {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	if strings.HasSuffix(typeName, "[]") {
		values, ok := value.([]any)
		if !ok {
			return false
		}
		itemType := strings.TrimSuffix(typeName, "[]")
		for _, item := range values {
			if !validInputType(itemType, item) {
				return false
			}
		}
		return true
	}
	switch typeName {
	case "text", "string":
		_, ok := value.(string)
		return ok
	case "number":
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
	case "integer":
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0) && number == math.Trunc(number)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "attachment":
		return validArtifactReference(value)
	case "attachments":
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if !validArtifactReference(item) {
				return false
			}
		}
		return true
	case "array":
		_, ok := value.([]any)
		return ok
	case "json":
		return true
	default:
		return false
	}
}
