package runtimepool

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	QuickCreateContextType     = "quick_create"
	QuickCreateContextSchemaV1 = "multica.quick-create/v1"
)

var ErrUnsupportedQuickCreateSchema = errors.New("unsupported Quick Create context schema")

// QuickCreateContext is the complete typed context persisted by the native
// Quick Create entry. Keeping the full envelope here lets placement validate
// the marker without dropping fields consumed later by daemon claim.
type QuickCreateContext struct {
	Type          string   `json:"type"`
	SchemaVersion string   `json:"schema_version"`
	Prompt        string   `json:"prompt"`
	RequesterID   string   `json:"requester_id"`
	WorkspaceID   string   `json:"workspace_id"`
	Priority      string   `json:"priority,omitempty"`
	DueDate       string   `json:"due_date,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	SquadID       string   `json:"squad_id,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	ParentIssueID string   `json:"parent_issue_id,omitempty"`
}

// ParseQuickCreateContext distinguishes ordinary Task context from the exact
// Quick Create marker. Once recognized, decoding is strict and fail-closed:
// unsupported schemas, duplicate keys, unknown fields and trailing JSON are
// errors rather than an ordinary-context fallback.
func ParseQuickCreateContext(raw json.RawMessage) (QuickCreateContext, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return QuickCreateContext{}, false, nil
	}
	var marker struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		return QuickCreateContext{}, false, err
	}
	if marker.Type != QuickCreateContextType {
		return QuickCreateContext{}, false, nil
	}
	if err := rejectDuplicateObjectKeys(raw); err != nil {
		return QuickCreateContext{}, true, err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value QuickCreateContext
	if err := decoder.Decode(&value); err != nil {
		return QuickCreateContext{}, true, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err != nil {
			return QuickCreateContext{}, true, err
		}
		return QuickCreateContext{}, true, errors.New("trailing Quick Create context")
	}
	if value.SchemaVersion != QuickCreateContextSchemaV1 {
		return QuickCreateContext{}, true, ErrUnsupportedQuickCreateSchema
	}
	return value, true, nil
}
