package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// refreshWorktreeReadiness is independent of task slots. A blocked source is
// therefore remeasured even when no work can be claimed. One sweep at a time
// avoids overlapping measurements overwriting newer source observations.
func (d *Daemon) refreshWorktreeReadiness(ctx context.Context, runtimeID string) {
	if d.client == nil || d.cfg.DaemonID == "" {
		return
	}
	key := runtimeID
	d.mu.Lock()
	for wsID, ws := range d.workspaces {
		for _, id := range ws.runtimeIDs {
			if id == runtimeID {
				key = wsID
			}
		}
	}
	d.mu.Unlock()
	value, _ := d.worktreeReadinessSweeps.LoadOrStore(key, &sync.Mutex{})
	sweep := value.(*sync.Mutex)
	if !sweep.TryLock() {
		return
	}
	defer sweep.Unlock()
	listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
	defer listCancel()
	path := fmt.Sprintf("/api/daemon/runtimes/%s/worktree-readiness", runtimeID)
	var response struct {
		Resources []protocol.WorktreeReadinessResource `json:"resources"`
	}
	if err := d.client.getJSON(listCtx, path, &response); err != nil {
		d.logger.Debug("resource readiness unavailable", "error", err)
		return
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	defer wg.Wait()
	for _, resource := range response.Resources {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		wg.Add(1)
		go func(resource protocol.WorktreeReadinessResource) {
			defer wg.Done()
			defer func() { <-slots }()
			scanCtx, stopScan := context.WithTimeout(ctx, 10*time.Second)
			defer stopScan()

			var ref localDirectoryRef
			if json.Unmarshal(resource.ResourceRef, &ref) != nil || ref.DaemonID != d.cfg.DaemonID || ref.ExecutionMode != localDirectoryModeWorktree {
				return
			}
			m := protocol.WorktreeReadiness{Status: "unavailable", MaxFiles: protocol.WorktreeReplayMaxFiles, MaxBytes: protocol.WorktreeReplayMaxBytes, LargestPaths: []protocol.WorktreeReadinessPath{}}
			if err := validateLocalPath(ref.LocalPath); err == nil {
				check, err := execenv.InspectUntrackedReplay(scanCtx, ref.LocalPath)
				if err == nil {
					m.Status = "ready"
					if check.Err() != nil {
						m.Status = "blocked"
					}
					m.FileCount = check.Files
					m.TotalBytes = check.Bytes
					m.SymlinkCount = check.Symlinks
					for _, p := range check.Largest {
						m.LargestPaths = append(m.LargestPaths, protocol.WorktreeReadinessPath{Path: p.Path, FileCount: p.Files, TotalBytes: p.Bytes})
					}
				}
			}
			report := protocol.WorktreeReadinessReport{WorktreeReadinessResource: resource, Measurement: m}
			reportCtx, stopReport := context.WithTimeout(ctx, 5*time.Second)
			defer stopReport()
			if err := d.client.postJSON(reportCtx, path, report, nil); err != nil {
				d.logger.Debug("resource readiness report failed", "resource_id", resource.ID, "error", err)
			}
		}(resource)
	}
}
