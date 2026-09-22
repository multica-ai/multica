package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Service) validateRunInputArtifacts(ctx context.Context, workspaceID, userID string, graph Graph, input string) error {
	if s.ValidateAttachment == nil {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(input), &values); err != nil {
		return nil
	}
	start := Node{}
	for _, node := range graph.Nodes {
		if node.Type == "start" {
			start = node
			break
		}
	}
	fields, _ := startInputFields(start)
	for _, field := range fields {
		value, ok := values[field.Key]
		if !ok || value == nil {
			continue
		}
		if err := s.validateArtifactValue(ctx, workspaceID, userID, field.Type, value); err != nil {
			return &Error{Status: 422, Code: "input_attachment_invalid", Message: fmt.Sprintf("input attachment %s is not accessible", field.Key), Details: []ValidationDetail{{Code: "input_attachment_invalid", NodeID: field.NodeID, Field: "input_values." + field.Key, Message: err.Error()}}}
		}
	}
	return nil
}

func (s *Service) validateNodeOutputArtifacts(ctx context.Context, workspaceID, userID string, node Node, output string) error {
	if s.ValidateAttachment == nil || strings.TrimSpace(output) == "" || len(node.Outputs) == 0 {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(output), &values); err != nil {
		return nil
	}
	for _, field := range node.Outputs {
		value, ok := values[field.Key]
		if !ok || value == nil {
			continue
		}
		if err := s.validateArtifactValue(ctx, workspaceID, userID, field.Type, value); err != nil {
			return &Error{Status: 422, Code: "output_attachment_invalid", Message: "node output attachment is not accessible", Details: []ValidationDetail{{Code: "output_attachment_invalid", NodeID: node.ID, Field: "outputs." + field.Key, Message: err.Error()}}}
		}
	}
	return nil
}

func (s *Service) validateArtifactValue(ctx context.Context, workspaceID, userID, typeName string, value any) error {
	baseType := strings.ToLower(strings.TrimSpace(typeName))
	if strings.HasSuffix(baseType, "[]") {
		baseType = strings.TrimSuffix(baseType, "[]")
	}
	if baseType == "array" {
		return nil
	}
	if baseType == "attachments" || strings.HasSuffix(strings.ToLower(strings.TrimSpace(typeName)), "[]") {
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("attachment list must be an array")
		}
		itemType := baseType
		if itemType == "attachments" {
			itemType = "attachment"
		}
		for _, item := range items {
			if err := s.validateArtifactValue(ctx, workspaceID, userID, itemType, item); err != nil {
				return err
			}
		}
		return nil
	}
	if baseType != "attachment" && baseType != "artifact" {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("artifact reference must be an object")
	}
	artifactID, _ := object["artifact_id"].(string)
	if artifactID == "" {
		artifactID, _ = object["id"].(string)
	}
	if strings.TrimSpace(artifactID) == "" {
		return fmt.Errorf("artifact_id is required")
	}
	return s.ValidateAttachment(ctx, workspaceID, userID, artifactID)
}
