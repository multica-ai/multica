package handler

import (
	"encoding/json"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"path"
)

type TaskExecutionSource struct {
	Repositories []RepoData `json:"repositories,omitempty"`
	Kind         string     `json:"kind"`
	Mode         string     `json:"mode"`
	Label        string     `json:"label,omitempty"`
	DaemonID     string     `json:"daemon_id,omitempty"`
}

func (h *Handler) saveClaimExecutionSource(r *http.Request, task *db.AgentTaskQueue, resp *AgentTaskResponse, runtime db.AgentRuntime) error {
	var choice struct {
		NoCode bool `json:"source_no_code"`
	}
	_ = json.Unmarshal(task.Context, &choice)
	if choice.NoCode || resp.ProjectNoCode {
		resp.ProjectResources = nil
		resp.Repos = nil
		resp.PriorSessionID = ""
		resp.PriorWorkDir = ""
	}
	selected := TaskExecutionSource{Kind: "no_code", Mode: "no_code"}
	for _, res := range resp.ProjectResources {
		if res.ResourceType != "local_directory" {
			continue
		}
		var ref localDirectoryRef
		if json.Unmarshal(res.ResourceRef, &ref) == nil && ref.DaemonID == runtime.DaemonID.String {
			mode := ref.ExecutionMode
			if mode == "" {
				mode = "in_place"
			}
			selected = TaskExecutionSource{Kind: "local_directory", Mode: mode, Label: path.Base(ref.LocalPath), DaemonID: ref.DaemonID}
			break
		}
	}
	if selected.Kind == "no_code" && len(resp.Repos) > 0 {
		selected = TaskExecutionSource{Kind: "github_repo", Mode: "managed_checkout", Label: resp.Repos[0].URL, Repositories: resp.Repos}
	}
	raw, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	saved, err := h.Queries.SaveTaskExecutionSource(r.Context(), db.SaveTaskExecutionSourceParams{ID: task.ID, Column2: raw})
	if err != nil {
		return err
	}
	task.ExecutionSource = saved
	return json.Unmarshal(saved, &resp.ExecutionSource)
}
