package traceupload

import (
	"encoding/json"
	"fmt"
)

// Sidecar is the per-task context the runtime transcript itself does not
// carry. It is serialized as the FIRST line of the uploaded JSONL — a
// self-describing `trace_meta` object — exactly as the registry endpoint
// expects (see lib/agent-traces route.ts / types.ts in firtal-data-registry).
// The registry merges these fields onto every event of the batch.
//
// All fields are best-effort: a value the daemon could not resolve is left
// empty and the registry stores null. Only Runtime, TaskID and SessionID are
// required for the endpoint to accept the batch.
type Sidecar struct {
	Runtime       string // "claude" | "codex"
	TaskID        string
	SessionID     string
	IssueID       string
	AgentID       string
	AgentName     string
	WorkspaceID   string
	ProjectID     string
	AutopilotID   string
	ParentTaskID  string
	CreatedByType string
}

// Valid reports whether the sidecar carries the minimum the registry requires:
// a known runtime plus task and session identity. An invalid sidecar can never
// be ingested, so we refuse to enqueue it.
func (s Sidecar) Valid() bool {
	if s.TaskID == "" || s.SessionID == "" {
		return false
	}
	return s.Runtime == "claude" || s.Runtime == "codex"
}

// Key uniquely identifies the (task, session) pair. It matches the registry's
// idempotency key, so it is also the natural ledger key.
func (s Sidecar) Key() string {
	return s.TaskID + "|" + s.SessionID
}

// metaLine is the JSON shape of the trace_meta first line. Field names and
// order mirror the registry's sidecarFromMeta() reader. `type` is always
// "trace_meta" so the endpoint can distinguish the meta line from transcript
// events. Empty strings are omitted so the registry reads them as null.
type metaLine struct {
	Type          string `json:"type"`
	Runtime       string `json:"runtime"`
	TaskID        string `json:"task_id"`
	SessionID     string `json:"session_id"`
	IssueID       string `json:"issue_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	AgentName     string `json:"agent_name,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	AutopilotID   string `json:"autopilot_id,omitempty"`
	ParentTaskID  string `json:"parent_task_id,omitempty"`
	CreatedByType string `json:"created_by_type,omitempty"`
}

// MetaLine returns the JSON-encoded trace_meta object (no trailing newline)
// that prefixes the uploaded JSONL batch.
func (s Sidecar) MetaLine() ([]byte, error) {
	b, err := json.Marshal(metaLine{
		Type:          "trace_meta",
		Runtime:       s.Runtime,
		TaskID:        s.TaskID,
		SessionID:     s.SessionID,
		IssueID:       s.IssueID,
		AgentID:       s.AgentID,
		AgentName:     s.AgentName,
		WorkspaceID:   s.WorkspaceID,
		ProjectID:     s.ProjectID,
		AutopilotID:   s.AutopilotID,
		ParentTaskID:  s.ParentTaskID,
		CreatedByType: s.CreatedByType,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal trace_meta: %w", err)
	}
	return b, nil
}
