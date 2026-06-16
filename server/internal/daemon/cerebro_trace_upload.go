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
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/traceupload"
)

// startTraceUpload launches the trace-upload manager. Config resolution order:
//  1. Local env fully configures it (enabled + endpoint + key) → use env.
//  2. Local env EXPLICITLY disables it (MULTICA_TRACE_UPLOAD_ENABLED set falsey)
//     → do nothing, and do not contact the server.
//  3. Otherwise → fetch the fleet config from the server OFF the boot path
//     (TECH-3621 rollout), so one server-side setting turns trace upload on for
//     every machine without delaying startup.
//
// With the feature off, no manager is created and every later Enqueue is a
// no-op — not a single disk write happens.
func (d *Daemon) startTraceUpload(ctx context.Context) {
	cfg := traceupload.ConfigFromEnv(d.cfg.WorkspacesRoot)
	if cfg.Enabled && cfg.Endpoint != "" && cfg.APIKey != "" {
		d.startTraceUploadWith(ctx, cfg) // local env fully configures it
		return
	}
	// CEREBRO-PATCH(daemon-trace-upload-fleet-fetch): TECH-3621 — respect an
	// explicit local opt-out, else fetch the fleet config asynchronously so a
	// slow/unreachable server never delays task claiming.
	if raw, ok := os.LookupEnv(traceupload.EnvEnabled); ok && strings.TrimSpace(raw) != "" && !cfg.Enabled {
		return // operator explicitly disabled trace upload on this machine
	}
	go d.fetchAndStartTraceUpload(ctx)
}

// startTraceUploadWith builds the manager from a resolved config and records it
// under the mutex (the async fleet fetch can call this after boot, concurrently
// with enqueueTraceUpload). A nil manager (feature off / misconfigured) is left
// unset so enqueue stays a no-op.
func (d *Daemon) startTraceUploadWith(ctx context.Context, cfg traceupload.Config) {
	mgr, err := traceupload.NewManager(cfg, d.logger)
	if err != nil {
		d.logger.Warn("trace upload disabled: misconfigured", "error", err)
		return
	}
	if mgr == nil {
		return
	}
	d.traceUploaderMu.Lock()
	d.traceUploader = mgr
	d.traceUploaderMu.Unlock()
	mgr.Start(ctx)
}

// fetchAndStartTraceUpload pulls the fleet trace-upload config from the server
// (TECH-3621 rollout path) and starts the manager if the server has the feature
// on. Runs in its own goroutine; best-effort with a short timeout, so it never
// blocks boot. preflightAuth has already run, so d.client is authenticated.
func (d *Daemon) fetchAndStartTraceUpload(ctx context.Context) {
	runtimeIDs := d.allRuntimeIDs()
	if len(runtimeIDs) == 0 {
		return // no runtime to scope the request to
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := d.client.FetchTraceConfig(fetchCtx, runtimeIDs[0])
	if err != nil {
		d.logger.Debug("trace upload: server config fetch failed, not uploading this boot", "error", err)
		return
	}
	if resp == nil || !resp.Enabled || resp.Endpoint == "" || resp.APIKey == "" {
		return // server has the feature off
	}
	cfg := traceupload.ConfigFromValues(d.cfg.WorkspacesRoot, resp.Endpoint, resp.APIKey, resp.MaxAgeHours)
	d.logger.Info("trace upload: enabled from server fleet config", "endpoint", resp.Endpoint)
	d.startTraceUploadWith(ctx, cfg)
}

// traceUploaderSnapshot returns the current manager under the mutex, so readers
// (enqueueTraceUpload) never race the async fleet-fetch writer.
func (d *Daemon) traceUploaderSnapshot() *traceupload.Manager {
	d.traceUploaderMu.Lock()
	defer d.traceUploaderMu.Unlock()
	return d.traceUploader
}

// enqueueTraceUpload records a finished task's transcript for upload. It runs
// after ReportTaskUsage and is strictly best-effort: any failure (no session
// id, transcript not found, ledger error) is logged and dropped — the task has
// already completed and must not be affected.
func (d *Daemon) enqueueTraceUpload(task Task, provider string, result TaskResult) {
	uploader := d.traceUploaderSnapshot()
	if uploader == nil {
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
	if err := uploader.Enqueue(sidecar, path); err != nil {
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

// traceTriggerUserName is the display name attributed to the run's "user" in
// the trace. TECH-3295 R3: prefer the chain's human origin (TriggerUserName,
// resolved server-side from task.OriginalUserID) so an agent→agent handoff is
// counted against the human behind it, not the handoff agent. Falls back to the
// immediate triggering author, then the requesting (runtime-owner) user.
func traceTriggerUserName(task Task) string {
	if task.TriggerUserName != "" { // CEREBRO-PATCH(daemon-trace-upload-prefer-human-origin): TECH-3295 R3 — chain's human origin wins over the handoff agent's name
		return task.TriggerUserName
	}
	if task.TriggerAuthorName != "" {
		return task.TriggerAuthorName
	}
	return task.RequestingUserName
}
