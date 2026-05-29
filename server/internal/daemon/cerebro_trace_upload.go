package daemon

// CEREBRO-PATCH(daemon-trace-upload): Fase 2 — daemon sends each finished
// task's JSONL transcript to the registry agent-trace ingest endpoint. The
// orchestration (ledger, retry, sweep) lives in the cerebro-zone subpackage
// internal/cerebro/traceupload; this file is the thin daemon-side glue:
// constructing the manager at boot and enqueuing at task close. Gated behind
// MULTICA_TRACE_UPLOAD_ENABLED (default off), and best-effort — an upload
// failure never touches the task outcome.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cerebro/traceupload"
)

// startTraceUpload builds the trace-upload manager from the environment and
// launches its workers + boot-sweep. With the feature flag off NewManager
// returns nil and every later Enqueue is a no-op — not a single HTTP call or
// disk write happens. A misconfiguration (flag on, endpoint/key missing) is
// logged and the daemon continues without uploading.
func (d *Daemon) startTraceUpload(ctx context.Context) {
	cfg := traceupload.ConfigFromEnv(d.cfg.WorkspacesRoot)
	mgr, err := traceupload.NewManager(cfg, d.logger)
	if err != nil {
		d.logger.Warn("trace upload disabled: misconfigured", "error", err)
		return
	}
	d.traceUploader = mgr
	if mgr != nil {
		mgr.Start(ctx)
	}
}

// enqueueTraceUpload records a finished task's transcript for upload. It runs
// after ReportTaskUsage and is strictly best-effort: any failure (no session
// id, transcript not found, ledger error) is logged and dropped — the task has
// already completed and must not be affected.
func (d *Daemon) enqueueTraceUpload(task Task, provider string, result TaskResult) {
	if d.traceUploader == nil {
		return // flag off — zero work
	}
	if provider != "claude" && provider != "codex" {
		return // only these runtimes have a transcript the registry normalizes
	}

	sidecar := traceupload.Sidecar{
		Runtime:     provider,
		TaskID:      task.ID,
		SessionID:   result.SessionID,
		IssueID:     task.IssueID,
		AgentID:     task.AgentID,
		WorkspaceID: task.WorkspaceID,
		ProjectID:   task.ProjectID,
		AutopilotID: task.AutopilotID,
		// created_by_type: the kind of actor whose action produced this run.
		// The daemon's nearest signal is the triggering comment's author type;
		// empty for runs with no triggering comment (the registry stores null).
		CreatedByType: task.TriggerAuthorType,
	}
	if task.Agent != nil {
		sidecar.AgentName = task.Agent.Name
	}

	path, err := traceupload.LocateJSONL(traceupload.LocateParams{
		Runtime:   provider,
		SessionID: result.SessionID,
		EnvRoot:   result.EnvRoot,
		WorkDir:   result.WorkDir,
	})
	if err != nil {
		d.logger.Warn("trace upload: transcript not located, skipping",
			"task", shortID(task.ID), "provider", provider, "error", err)
		return
	}
	if err := d.traceUploader.Enqueue(sidecar, path); err != nil {
		d.logger.Warn("trace upload: enqueue failed, skipping", "task", shortID(task.ID), "error", err)
	}
}
