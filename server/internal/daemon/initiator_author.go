package daemon

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

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

type initiatorAuthorRefresher interface {
	RefreshInitiatorAuthor(ctx context.Context, workDir string, params repocache.WorktreeParams) error
}

// refreshInitiatorAuthor points the managed checkouts in a reused workdir at
// this run's initiator before the agent starts. Checkout writes the author
// only when the agent calls it, and a resumed run often commits in the
// existing checkout directly. Failures are logged, not fatal: the run still
// proceeds with whatever identity the checkout already resolves.
func (d *Daemon) refreshInitiatorAuthor(ctx context.Context, task Task, env *execenv.Environment, taskLog *slog.Logger) {
	if env == nil || env.WorkDir == "" || env.LocalDirectory || env.LocalWorktree != nil {
		return
	}
	refresher, ok := d.repoCache.(initiatorAuthorRefresher)
	if !ok {
		return
	}
	params := repocache.WorktreeParams{
		CommitAuthorIsInitiatorEnabled: d.workspaceCommitAuthorIsInitiatorEnabled(task.WorkspaceID),
		InitiatorType:                  task.InitiatorType,
		InitiatorName:                  task.InitiatorName,
		InitiatorEmail:                 task.InitiatorEmail,
	}
	if err := refresher.RefreshInitiatorAuthor(ctx, env.WorkDir, params); err != nil && taskLog != nil {
		taskLog.Warn("could not refresh initiator commit author on reused checkouts", "error", err)
	}
}
