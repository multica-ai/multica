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
		// Origin labels (FIR-2438 v1.1): where the run was triggered + who
		// triggered it. surface is derived from the task's own fields; the
		// triggerer's display name is the nearest identity the daemon holds
		// (its UUID is not on the daemon Task struct — a separate follow-up).
		Surface:         traceSurface(task), // CEREBRO-PATCH(daemon-trace-upload-channel-label): label channel/dm + trigger user id (FIR-2438)
		TriggerUserID:   task.TriggerUserID,
		TriggerUserName: traceTriggerUserName(task),
		// CEREBRO-PATCH(daemon-trace-upload-display-titles): FIR-2763 M1 display
		// titles resolved at claim time; empty when unknown.
		IssueTitle:       task.IssueTitle,
		ParentIssueTitle: task.ParentIssueTitle,
		ProjectName:      task.ProjectTitle,
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

// traceSurface classifies where a run was triggered from the task's own fields.
// Order matters: chat and autopilot are unambiguous; channels (issues with
// kind 'channel'/'dm') are distinguished from ordinary issues so the trace can
// be sliced by surface (Jesper 30/5: "channel er issues med kind = channel").
// An ordinary issue task is "issue" when a triggering comment exists, else
// "issue_direct" (a direct assignment). Empty when none apply (registry → null).
func traceSurface(task Task) string {
	switch {
	case task.ChatSessionID != "":
		return "chat"
	case task.AutopilotID != "":
		return "autopilot"
	case task.IssueKind == "channel":
		return "channel"
	case task.IssueKind == "dm":
		return "dm"
	case task.IssueID != "" && task.TriggerCommentID != "":
		return "issue"
	case task.IssueID != "":
		return "issue_direct"
	default:
		return ""
	}
}

// traceTriggerUserName is the display name of whoever triggered the run: the
// triggering comment's author for issue tasks, else the requesting user (chat /
// direct). Empty when neither is set.
func traceTriggerUserName(task Task) string {
	if task.TriggerAuthorName != "" {
		return task.TriggerAuthorName
	}
	return task.RequestingUserName
}
