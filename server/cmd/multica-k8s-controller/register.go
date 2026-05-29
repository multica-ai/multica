package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
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
}

// RegisterAll posts one Register call per workspace tuple in cfg and returns
// the server-assigned runtime IDs.
func RegisterAll(ctx context.Context, cli *daemon.Client, cfg *Config) ([]Registered, error) {
	out := make([]Registered, 0, len(cfg.Workspaces))
	for _, w := range cfg.Workspaces {
		daemonID := stableDaemonID(cfg.DaemonIDPrefix, w.ID, w.Provider)
		cliVersion := cfg.CLIVersion
		if cliVersion == "" || cliVersion == "dev" {
			cliVersion = "v0.3.5"
		}
		req := map[string]any{
			"workspace_id":      w.ID,
			"daemon_id":         daemonID,
			"legacy_daemon_ids": []string{},
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
			WorkspaceID:  w.ID,
			Provider:     w.Provider,
			AgentName:    w.AgentName,
			RuntimeID:    runtimeID,
			Image:        w.RuntimeImage,
			PVCSize:      w.PVCSize,
			StorageClass: w.StorageClass,
		})
	}
	return out, nil
}

// stableDaemonID produces a deterministic daemon_id per (prefix, workspace,
// provider) tuple so the server merges the runtime row across controller
// restarts instead of churning rows.
func stableDaemonID(prefix, workspaceID, provider string) string {
	h := sha1.Sum([]byte(prefix + "|" + workspaceID + "|" + provider))
	return prefix + "-" + hex.EncodeToString(h[:8])
}

// RunHeartbeatLoop sends SendHeartbeat for every Registered runtime on each
// tick of `interval`, until ctx is cancelled. Heartbeats are sent
// sequentially; the daemon API already tolerates burstiness from one client.
func RunHeartbeatLoop(ctx context.Context, cli *daemon.Client, runtimes []Registered, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, r := range runtimes {
				_, _ = cli.SendHeartbeat(ctx, r.RuntimeID)
			}
		}
	}
}
