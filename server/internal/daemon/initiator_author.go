package daemon

import "encoding/json"

func (d *Daemon) workspaceCommitAuthorIsInitiatorEnabled(workspaceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return false
	}
	var settings struct {
		GitHubEnabled                  *bool `json:"github_enabled"`
		CommitAuthorIsInitiatorEnabled bool  `json:"commit_author_is_initiator_enabled"`
	}
	if err := json.Unmarshal(ws.settings, &settings); err != nil {
		return false
	}
	return settings.CommitAuthorIsInitiatorEnabled && (settings.GitHubEnabled == nil || *settings.GitHubEnabled)
}
