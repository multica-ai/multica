package runcontext

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// IssueFields is the machine-readable issue snapshot injected into agent runs.
// It intentionally carries only first-class fields needed before prose
// description/comments are fetched.
type IssueFields struct {
	ID            string `json:"id"`
	Identifier    string `json:"identifier"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Priority      string `json:"priority"`
	ParentIssueID string `json:"parent_issue_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
}

// ParentFields is the summarized parent issue snapshot injected alongside the
// current issue so agents don't have to parse prose to discover hierarchy.
type ParentFields struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}

// IssueSnapshot is the dispatch-time snapshot persisted on issue-bound task
// rows. Task-scoped fields like attempt/kind live on the task row already and
// are layered in by the daemon when it writes the final run context file.
type IssueSnapshot struct {
	Issue      *IssueFields    `json:"issue"`
	Parent     *ParentFields   `json:"parent"`
	Properties json.RawMessage `json:"properties"`
}

// TaskFields is the task-scoped portion of the run context file that is
// available directly from the claimed task row.
type TaskFields struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Attempt     int32  `json:"attempt"`
	MaxAttempts int32  `json:"max_attempts"`
}

// File is the final machine-readable run context written into the agent's
// workdir and referenced by MULTICA_RUN_CONTEXT.
type File struct {
	Task       TaskFields      `json:"task"`
	Issue      *IssueFields    `json:"issue"`
	Parent     *ParentFields   `json:"parent"`
	Properties json.RawMessage `json:"properties"`
}

func EmptyProperties() json.RawMessage {
	return json.RawMessage("{}")
}

func NormalizeProperties(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return EmptyProperties()
	}
	return append(json.RawMessage(nil), raw...)
}

func FormatIssueIdentifier(prefix string, number int32) string {
	if prefix == "" {
		return strconv.Itoa(int(number))
	}
	return prefix + "-" + strconv.Itoa(int(number))
}

func BuildFile(task TaskFields, snapshot IssueSnapshot) File {
	return File{
		Task:       task,
		Issue:      snapshot.Issue,
		Parent:     snapshot.Parent,
		Properties: NormalizeProperties(snapshot.Properties),
	}
}

func ParseIssueSnapshot(data []byte) (IssueSnapshot, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return IssueSnapshot{Properties: EmptyProperties()}, nil
	}
	var snapshot IssueSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return IssueSnapshot{}, err
	}
	snapshot.Properties = NormalizeProperties(snapshot.Properties)
	return snapshot, nil
}
