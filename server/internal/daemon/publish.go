package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/gitops"
)

// autoPublishCompletedTask publishes completed code changes when enabled.
// Failures are logged but do not fail the task itself.
func (d *Daemon) autoPublishCompletedTask(ctx context.Context, task Task, result TaskResult) {
	if !d.cfg.AutoPublish {
		return
	}
	if result.Status != "completed" || strings.TrimSpace(result.WorkDir) == "" {
		return
	}

	commitMessage := fmt.Sprintf("multica auto publish %s", shortID(task.ID))
	published, err := gitops.PublishWorkspace(result.WorkDir, gitops.PublishOptions{
		Remote:        d.cfg.PublishRemote,
		CommitMessage: commitMessage,
	})
	if err != nil {
		d.logger.Warn("auto publish failed", "task_id", task.ID, "workdir", result.WorkDir, "error", err)
		return
	}

	for _, repo := range published {
		d.logger.Info("auto published repo",
			"task_id", task.ID,
			"repo_root", repo.Root,
			"branch", repo.Branch,
			"remote", repo.Remote,
			"commit", repo.CommitHash,
			"committed", repo.Committed,
		)
	}
}
