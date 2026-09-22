package workflow

import (
	"encoding/json"
	"math"
	"strings"
)

func validOutputTypeName(typeName string) bool {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	if typeName == "" {
		return true
	}
	if strings.HasSuffix(typeName, "[]") {
		return validOutputTypeName(strings.TrimSuffix(typeName, "[]"))
	}
	switch typeName {
	case "text", "string", "number", "integer", "boolean", "json", "select", "attachment", "artifact", "attachments", "array":
		return true
	default:
		return false
	}
}

func validArtifactReference(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	artifactID, _ := object["artifact_id"].(string)
	if artifactID == "" {
		artifactID, _ = object["id"].(string)
	}
	if strings.TrimSpace(artifactID) == "" {
		return false
	}
	// A path is never an artifact identity. Accepting one here would make a
	// successful node result depend on the filesystem of the worker that ran it.
	if path, ok := object["path"].(string); ok && strings.TrimSpace(path) != "" {
		return false
	}
	if url, ok := object["url"].(string); ok {
		url = strings.TrimSpace(strings.ToLower(url))
		if strings.HasPrefix(url, "file:") || strings.HasPrefix(url, "/") || strings.HasPrefix(url, "~/") || strings.HasPrefix(url, "../") || strings.HasPrefix(url, "./") {
			return false
		}
	}
	return true
}

func validateNodeOutput(node Node, output string) error {
	if len(node.Outputs) == 0 {
		return nil
	}
	if strings.TrimSpace(output) == "" {
		for _, field := range node.Outputs {
			if field.Required {
				return outputInvalid(node.ID, "outputs."+field.Key, "required output is missing")
			}
		}
		return nil
	}
	var values map[string]any
	if json.Unmarshal([]byte(output), &values) != nil {
		if len(node.Outputs) == 1 && node.Outputs[0].Required && (node.Outputs[0].Type == "" || node.Outputs[0].Type == "text" || node.Outputs[0].Type == "string") {
			return nil
		}
		return outputInvalid(node.ID, "outputs", "output must contain the declared fields")
	}
	declared := make(map[string]OutputField, len(node.Outputs))
	for _, field := range node.Outputs {
		declared[field.Key] = field
	}
	for key := range values {
		if _, ok := declared[key]; !ok {
			return outputInvalid(node.ID, "outputs."+key, "output field is not declared by the node")
		}
	}
	for _, field := range node.Outputs {
		value, ok := values[field.Key]
		if !ok || value == nil {
			if field.Required {
				return outputInvalid(node.ID, "outputs."+field.Key, "required output field is missing")
			}
			continue
		}
		if field.Required && (field.Type == "text" || field.Type == "string") && strings.TrimSpace(toText(value)) == "" {
			return outputInvalid(node.ID, "outputs."+field.Key, "required text output is empty")
		}
		if !validOutputType(field.Type, value) {
			return outputInvalid(node.ID, "outputs."+field.Key, "output value does not match the declared type")
		}
	}
	return nil
}

func validOutputType(typeName string, value any) bool {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	if strings.HasSuffix(typeName, "[]") {
		items, ok := value.([]any)
		if !ok {
			return false
		}
		itemType := strings.TrimSuffix(typeName, "[]")
		for _, item := range items {
			if !validOutputType(itemType, item) {
				return false
			}
		}
		return true
	}
	switch typeName {
	case "", "json":
		return true
	case "text", "string":
		_, ok := value.(string)
		return ok
	case "number":
		return validJSONNumber(value, false)
	case "integer":
		return validJSONNumber(value, true)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "select":
		_, ok := value.(string)
		return ok
	case "attachment", "artifact":
		return validArtifactReference(value)
	case "attachments", "array":
		items, ok := value.([]any)
		if !ok {
			return false
		}
		if typeName == "attachments" {
			for _, item := range items {
				if !validArtifactReference(item) {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func validJSONNumber(value any, integer bool) bool {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	default:
		return false
	}
	return !math.IsNaN(number) && !math.IsInf(number, 0) && (!integer || number == math.Trunc(number))
}

func outputInvalid(nodeID, field, message string) error {
	return &Error{Status: 422, Message: "node output is invalid", Code: "output_invalid", Details: []ValidationDetail{{Code: "output_invalid", NodeID: nodeID, Field: field, Message: message}}}
}

func toText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
