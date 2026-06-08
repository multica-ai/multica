package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// Registered is one (workspace, provider) tuple after the server has assigned
// it a runtime_id.
type Registered struct {
	WorkspaceID  string
	Provider     string
	AgentName    string
	RuntimeID    string
	Image        string
	PVCSize      string
	StorageClass string
	// ServiceAccountName is set on the worker Job pod so the projected SA
	// token at /var/run/secrets/kubernetes.io/serviceaccount/ carries the
	// rights bound to that SA (via the chart's worker-rbac.yaml). Empty =
	// pod uses the namespace `default` SA, i.e. no cluster API access.
	ServiceAccountName string
}

// RegisterAll posts one Register call per workspace tuple in cfg and returns
// the server-assigned runtime IDs.
func RegisterAll(ctx context.Context, cli *daemon.Client, cfg *Config) ([]Registered, error) {
	out := make([]Registered, 0, len(cfg.Workspaces))
	for _, w := range cfg.Workspaces {
		daemonID := stableDaemonID(cfg.DaemonIDPrefix, w.ID, w.Provider, w.AgentName)
		cliVersion := cfg.CLIVersion
		if cliVersion == "" || cliVersion == "dev" {
			cliVersion = "v0.3.5"
		}
		// Pre-multi-agent installs hashed only (prefix, workspace, provider) — so
		// upgrading from one of those would orphan the old daemon row. List the
		// legacy hash so the server merges that row's runtime into this one
		// instead of leaving it as a stale offline ghost. Harmless when no such
		// row exists.
		req := map[string]any{
			"workspace_id":      w.ID,
			"daemon_id":         daemonID,
			"legacy_daemon_ids": []string{legacyStableDaemonID(cfg.DaemonIDPrefix, w.ID, w.Provider)},
			"device_name":       cfg.DeviceName,
			"cli_version":       cliVersion,
			"runtimes": []map[string]any{
				{"name": w.AgentName, "type": w.Provider, "version": "unknown", "status": "online"},
			},
		}
		resp, err := cli.Register(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("register ws=%s provider=%s: %w", w.ID, w.Provider, err)
		}
		// Find the runtime in the response by matching name + provider.
		runtimeID := ""
		for _, rt := range resp.Runtimes {
			if rt.Provider == w.Provider && rt.Name == w.AgentName {
				runtimeID = rt.ID
				break
			}
		}
		if runtimeID == "" {
			return nil, fmt.Errorf("server did not return a runtime_id for ws=%s provider=%s", w.ID, w.Provider)
		}
		out = append(out, Registered{
			WorkspaceID:        w.ID,
			Provider:           w.Provider,
			AgentName:          w.AgentName,
			RuntimeID:          runtimeID,
			Image:              w.RuntimeImage,
			PVCSize:            w.PVCSize,
			StorageClass:       w.StorageClass,
			ServiceAccountName: w.ServiceAccountName,
		})
	}
	return out, nil
}

// stableDaemonID produces a deterministic daemon_id per (prefix, workspace,
// provider, agentName) tuple so the server merges the runtime row across
// controller restarts instead of churning rows. agentName is part of the hash
// because the DB enforces UNIQUE (workspace_id, daemon_id, provider) on
// agent_runtime — two agents on the same (ws, provider) with the same daemon_id
// would upsert onto the same row, hiding all but one agent in the UI.
func stableDaemonID(prefix, workspaceID, provider, agentName string) string {
	h := sha1.Sum([]byte(prefix + "|" + workspaceID + "|" + provider + "|" + agentName))
	return prefix + "-" + hex.EncodeToString(h[:8])
}

// legacyStableDaemonID returns the pre-agentName hash. Sent in legacy_daemon_ids
// so the server merges a pre-upgrade single-agent row into the corresponding
// post-upgrade agent on first registration after rollout. Same shape as
// stableDaemonID minus the agentName component.
func legacyStableDaemonID(prefix, workspaceID, provider string) string {
	h := sha1.Sum([]byte(prefix + "|" + workspaceID + "|" + provider))
	return prefix + "-" + hex.EncodeToString(h[:8])
}

// RunHeartbeatLoop sends SendHeartbeat for every Registered runtime on each
// tick of `interval`, until ctx is cancelled. Heartbeats are sent
// sequentially; the daemon API already tolerates burstiness from one client.
//
// The heartbeat ack carries pending server-initiated requests (model list,
// local skills, …). The controller services the ones that make sense from a
// pod-dispatching, no-host-CLI host — today that's model_list only.
// PendingLocalSkills / PendingLocalSkillImport are deliberately not handled:
// there are no host-installed skill bundles to enumerate from inside the
// cluster, so we let those server-side requests time out rather than reply
// with synthetic "failed" payloads that would look like real errors in the UI.
func RunHeartbeatLoop(ctx context.Context, cli *daemon.Client, runtimes []Registered, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, r := range runtimes {
				ack, _ := cli.SendHeartbeat(ctx, r.RuntimeID)
				dispatchHeartbeatActions(ctx, cli, r, ack)
			}
		}
	}
}

// dispatchHeartbeatActions reacts to the pending-actions block in a heartbeat
// ack. Each action is dispatched in its own goroutine so a slow report cannot
// stall the next heartbeat tick.
func dispatchHeartbeatActions(ctx context.Context, cli *daemon.Client, r Registered, ack *daemon.HeartbeatResponse) {
	if ack == nil {
		return
	}
	if ack.PendingModelList != nil {
		go handleModelList(ctx, cli, r, ack.PendingModelList.ID)
	}
}

// handleModelList answers a server-side model-list request by running
// provider discovery in-process. The controller has no agent CLI binary
// installed in its pod — for "claude" that means agent.ListModels returns
// the static catalog and skips thinking-level probing, which is the right
// answer here: the static catalog already carries every advertised model.
//
// Best-effort: single attempt, no retry. On a transient server-side failure
// the request will reach the 60s server-side timeout and the UI's polling
// loop will surface a clear "model discovery timed out" error rather than
// silently hanging. The host daemon's retry loop is a `*Daemon`-bound helper
// we deliberately don't carry into the controller — adding retry here can
// happen later if we observe real transient failures in this path.
func handleModelList(ctx context.Context, cli *daemon.Client, r Registered, requestID string) {
	slog.Default().Info("model list requested",
		"runtime_id", r.RuntimeID, "request_id", requestID, "provider", r.Provider)
	payload := daemon.BuildModelListPayload(ctx, r.Provider, "")
	if err := cli.ReportModelListResult(ctx, r.RuntimeID, requestID, payload); err != nil {
		slog.Default().Error("model list report failed",
			"runtime_id", r.RuntimeID, "request_id", requestID, "error", err)
	}
}
