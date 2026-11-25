package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/codechange"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// codeChangeReportTimeout bounds the whole snapshot and upload, so a huge
// checkout cannot hold up task completion for long. A failure only costs the
// snapshot.
const codeChangeReportTimeout = 45 * time.Second

// reportCodeChanges snapshots the checkouts under root against the run's
// baselines and uploads the result. A run whose directory holds no git
// checkout at all reports nothing; otherwise an empty snapshot is still sent,
// so the page can tell "changed nothing" from "not recorded".
func (d *Daemon) reportCodeChanges(taskID, root string, baselines *codechange.Baselines, taskLog *slog.Logger) {
	if taskID == "" || root == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), codeChangeReportTimeout)
	defer cancel()
	snapshot := codechange.Snapshot(ctx, root, baselines)
	if snapshot.Checkouts == 0 {
		return
	}
	// Patches are file contents. Mask secret-shaped text before it leaves
	// this machine, as tool inputs are; the server masks again on ingest.
	for i := range snapshot.Repos {
		snapshot.Repos[i].Patch = redact.Text(snapshot.Repos[i].Patch)
	}
	repos, reposDropped := codechange.Fit(snapshot.Repos)
	if reposDropped || snapshot.ScanCapped {
		taskLog.Info("code changes: some checkouts left out of the report",
			"reported", len(repos), "scan_capped", snapshot.ScanCapped)
	}
	if err := d.client.ReportTaskCodeChanges(ctx, taskID, repos, reposDropped || snapshot.ScanCapped); err != nil {
		var reqErr *requestError
		if errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusNotFound || reqErr.StatusCode == http.StatusMethodNotAllowed) {
			taskLog.Debug("server does not accept code changes; skipping", "error", err)
			return
		}
		taskLog.Warn("failed to report code changes", "error", err)
		return
	}
	taskLog.Debug("code changes reported", "repos", len(repos))
}
