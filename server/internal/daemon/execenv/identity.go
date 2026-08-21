package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TaskDirIdentity is the full unique binding of a daemon env root, assembled
// from live Prepare-time files first and completion-time .gc_meta.json last.
// A reused or prefix-collided directory can leave a stale gc_meta from a
// previous occupant; the marker and provenance written at Prepare win.
type TaskDirIdentity struct {
	TaskID        string
	IssueID       string
	ChatSessionID string
	AgentID       string
	ProjectID     string
	WorkspaceID   string
	Kind          GCMetaKind
}

// ReadTaskDirIdentity inspects envRoot without following the directory name.
// Missing files are ignored; an unreadable owner marker is ignored the same
// way disk-usage already treats a missing gc_meta — present on disk, but no
// parent record we can lock onto.
func ReadTaskDirIdentity(envRoot string) TaskDirIdentity {
	var id TaskDirIdentity
	if owner, err := readEnvRootOwner(envRoot); err == nil {
		id.TaskID = strings.TrimSpace(owner)
	}
	if prov, err := ReadManagedEnvProvenance(envRoot); err == nil && prov != nil &&
		prov.ManagedBy == ManagedEnvProvenanceManagedBy {
		if id.WorkspaceID == "" {
			id.WorkspaceID = strings.TrimSpace(prov.WorkspaceID)
		}
		id.IssueID = firstNonEmpty(id.IssueID, prov.IssueID)
		id.ChatSessionID = firstNonEmpty(id.ChatSessionID, prov.ChatSessionID)
		id.AgentID = firstNonEmpty(id.AgentID, prov.AgentID)
		id.ProjectID = firstNonEmpty(id.ProjectID, prov.ProjectID)
	}
	if marker := readTaskContextMarkerFile(filepath.Join(envRoot, "workdir")); marker != nil {
		id.IssueID = firstNonEmpty(id.IssueID, marker.IssueID)
		id.ChatSessionID = firstNonEmpty(id.ChatSessionID, marker.ChatSessionID)
		id.AgentID = firstNonEmpty(id.AgentID, marker.AgentID)
	}
	if projectID := readProjectID(filepath.Join(envRoot, "workdir")); projectID != "" {
		id.ProjectID = firstNonEmpty(id.ProjectID, projectID)
	}
	if id.IssueID != "" {
		id.Kind = GCKindIssue
	} else if id.ChatSessionID != "" {
		id.Kind = GCKindChat
	}
	if meta, err := ReadGCMeta(envRoot); err == nil && meta != nil {
		id.WorkspaceID = firstNonEmpty(id.WorkspaceID, meta.WorkspaceID)
		id.TaskID = firstNonEmpty(id.TaskID, strings.TrimSpace(meta.TaskID))
		if id.Kind == "" {
			id.Kind = meta.Kind
		}
		// gc_meta is last: only fill ids the live files did not already name.
		id.IssueID = firstNonEmpty(id.IssueID, meta.IssueID)
		id.ChatSessionID = firstNonEmpty(id.ChatSessionID, meta.ChatSessionID)
	}
	return id
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func readTaskContextMarkerFile(workDir string) *taskContextMarkerFile {
	data, err := os.ReadFile(filepath.Join(workDir, TaskContextMarkerRelPath))
	if err != nil {
		return nil
	}
	var marker taskContextMarkerFile
	if json.Unmarshal(data, &marker) != nil || marker.ManagedBy != TaskContextMarkerManagedBy {
		return nil
	}
	return &marker
}

func readProjectID(workDir string) string {
	data, err := os.ReadFile(filepath.Join(workDir, ".multica", "project", "resources.json"))
	if err != nil {
		return ""
	}
	var parsed struct {
		ProjectID string `json:"project_id"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return ""
	}
	return strings.TrimSpace(parsed.ProjectID)
}
