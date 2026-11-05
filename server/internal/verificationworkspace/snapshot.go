//go:build verificationdev

package verificationworkspace

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

type Content struct {
	SourceOrigin  string           `json:"sourceOrigin"`
	SchemaVersion int              `json:"schemaVersion"`
	Workspace     map[string]any   `json:"workspace"`
	Statuses      []map[string]any `json:"statuses"`
	Workflows     []map[string]any `json:"workflows"`
	Projects      []map[string]any `json:"projects"`
	Issues        []map[string]any `json:"issues"`
	Comments      []map[string]any `json:"comments"`
	Labels        []map[string]any `json:"labels"`
	Attachments   []Attachment     `json:"attachments"`
}
type Attachment struct {
	ID          string  `json:"id"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"content_type"`
	SizeBytes   int     `json:"size_bytes"`
	IssueID     *string `json:"issue_id"`
	CommentID   *string `json:"comment_id"`
	Data        string  `json:"data"`
	SHA256      string  `json:"sha256"`
}

func Text(row map[string]any, key string) string { value, _ := row[key].(string); return value }
func (content *Content) Validate() error {
	if content.SchemaVersion != 1 || len(content.Projects) > 100 || len(content.Issues) > 1000 || len(content.Comments) > 10000 || len(content.Attachments) > 1000 {
		return fmt.Errorf("unsupported snapshot version or scope")
	}
	if _, err := uuid.Parse(Text(content.Workspace, "id")); err != nil {
		return fmt.Errorf("invalid workspace")
	}
	all := map[string]bool{}
	byKind := map[string]map[string]bool{}
	for kind, rows := range map[string][]map[string]any{"statuses": content.Statuses, "workflows": content.Workflows, "projects": content.Projects, "issues": content.Issues, "comments": content.Comments, "labels": content.Labels} {
		byKind[kind] = map[string]bool{}
		for _, row := range rows {
			id := Text(row, "id")
			if _, err := uuid.Parse(id); err != nil || all[id] {
				return fmt.Errorf("invalid or duplicate %s id", kind)
			}
			all[id] = true
			byKind[kind][id] = true
			if workspace := Text(row, "workspace_id"); workspace != "" && workspace != Text(content.Workspace, "id") {
				return fmt.Errorf("cross-workspace entity")
			}
		}
	}
	check := func(row map[string]any, key, kind string, required bool) error {
		id := Text(row, key)
		if id == "" && !required {
			return nil
		}
		if !byKind[kind][id] {
			return fmt.Errorf("missing %s relationship", key)
		}
		return nil
	}
	statuses := map[string]bool{}
	for _, row := range content.Statuses {
		key := Text(row, "key")
		if key == "" || statuses[key] {
			return fmt.Errorf("invalid status key")
		}
		statuses[key] = true
	}
	for _, row := range content.Workflows {
		steps, ok := row["steps"].([]any)
		if !ok || len(steps) == 0 {
			return fmt.Errorf("missing workflow steps")
		}
		for _, raw := range steps {
			step, ok := raw.(map[string]any)
			if !ok || !statuses[Text(step, "status_key")] {
				return fmt.Errorf("unknown workflow status")
			}
		}
	}
	for _, row := range content.Projects {
		if Text(row, "title") == "" {
			return fmt.Errorf("missing project title")
		}
		if err := check(row, "workflow_id", "workflows", false); err != nil {
			return err
		}
	}
	for _, row := range content.Issues {
		if Text(row, "title") == "" || !statuses[Text(row, "status")] {
			return fmt.Errorf("missing issue title or status")
		}
		for key, kind := range map[string]string{"project_id": "projects", "parent_issue_id": "issues"} {
			if err := check(row, key, kind, false); err != nil {
				return err
			}
		}
		if props, ok := row["properties"].(map[string]any); ok && len(props) > 0 {
			return fmt.Errorf("custom property definitions require a newer snapshot format")
		}
		if row["source_context"] != nil {
			return fmt.Errorf("source-context execution cannot be restored as content")
		}
		if labels, ok := row["labels"].([]any); ok {
			for _, raw := range labels {
				label, ok := raw.(map[string]any)
				if !ok || !byKind["labels"][Text(label, "id")] {
					return fmt.Errorf("missing issue label")
				}
			}
		}
	}
	for _, row := range content.Comments {
		if err := check(row, "issue_id", "issues", true); err != nil {
			return err
		}
		if err := check(row, "parent_id", "comments", false); err != nil {
			return err
		}
	}
	total := 0
	for _, attachment := range content.Attachments {
		if _, err := uuid.Parse(attachment.ID); err != nil || all[attachment.ID] {
			return fmt.Errorf("invalid attachment id")
		}
		all[attachment.ID] = true
		data, err := base64.StdEncoding.DecodeString(attachment.Data)
		sum := sha256.Sum256(data)
		total += len(data)
		if err != nil || len(data) != attachment.SizeBytes || len(data) > 25<<20 || total > 40<<20 || hex.EncodeToString(sum[:]) != attachment.SHA256 {
			return fmt.Errorf("attachment integrity failure")
		}
		if attachment.IssueID != nil && !byKind["issues"][*attachment.IssueID] {
			return fmt.Errorf("missing attachment issue")
		}
		if attachment.CommentID != nil && !byKind["comments"][*attachment.CommentID] {
			return fmt.Errorf("missing attachment comment")
		}
	}
	issues, err := sortParents(content.Issues, "parent_issue_id")
	if err != nil {
		return err
	}
	comments, err := sortParents(content.Comments, "parent_id")
	if err != nil {
		return err
	}
	content.Issues, content.Comments = issues, comments
	return nil
}
func sortParents(rows []map[string]any, key string) ([]map[string]any, error) {
	byID := map[string]map[string]any{}
	for _, row := range rows {
		byID[Text(row, "id")] = row
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	result := make([]map[string]any, 0, len(rows))
	var visit func(string) error
	visit = func(id string) error {
		if done[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("cyclic parent relationship")
		}
		visiting[id] = true
		row := byID[id]
		if parent := Text(row, key); parent != "" {
			if _, ok := byID[parent]; !ok {
				return fmt.Errorf("missing parent")
			}
			if err := visit(parent); err != nil {
				return err
			}
		}
		done[id] = true
		result = append(result, row)
		return nil
	}
	for _, row := range rows {
		if err := visit(Text(row, "id")); err != nil {
			return nil, err
		}
	}
	return result, nil
}
