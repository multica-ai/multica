package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"time"
)

func (d *Daemon) sharedSkillsSyncLoop(ctx context.Context) {
	interval := d.cfg.SharedSkillsSyncInterval
	if interval <= 0 || d.cfg.SharedSkillsDir == "" {
		return
	}
	d.syncSharedSkillsOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.syncSharedSkillsOnce(ctx)
		}
	}
}

func (d *Daemon) syncSharedSkillsOnce(ctx context.Context) {
	if !d.ready.Load() {
		return
	}
	if _, err := os.Stat(d.cfg.SharedSkillsDir); err != nil {
		if !os.IsNotExist(err) {
			d.logger.Warn("shared skills root unavailable", "path", d.cfg.SharedSkillsDir, "error", err)
		}
		return
	}
	for _, rt := range d.sharedSkillSyncRuntimes() {
		if err := d.syncSharedSkillsForRuntime(ctx, rt); err != nil && ctx.Err() == nil {
			d.logger.Warn("shared skills sync failed", "runtime_id", rt.ID, "provider", rt.Provider, "error", err)
		}
	}
}

func (d *Daemon) sharedSkillSyncRuntimes() []Runtime {
	d.mu.Lock()
	defer d.mu.Unlock()

	runtimes := make([]Runtime, 0, len(d.workspaces))
	for _, ws := range d.workspaces {
		for _, id := range ws.runtimeIDs {
			rt, ok := d.runtimeIndex[id]
			if !ok || rt.Status != "online" {
				continue
			}
			runtimes = append(runtimes, rt)
			break
		}
	}
	return runtimes
}

func (d *Daemon) syncSharedSkillsForRuntime(ctx context.Context, rt Runtime) error {
	summaries, _, err := listLocalSkillsFromRoot(rt.Provider, d.cfg.SharedSkillsDir)
	if err != nil {
		return err
	}
	bundles := make([]SharedSkillBundle, 0, len(summaries))
	for _, summary := range summaries {
		bundle, _, err := loadLocalSkillBundleFromRoot(rt.Provider, d.cfg.SharedSkillsDir, summary.Key)
		if err != nil {
			d.logger.Warn("shared skill bundle skipped", "key", summary.Key, "error", err)
			continue
		}
		files := make([]SkillFileData, len(bundle.Files))
		copy(files, bundle.Files)
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		bundles = append(bundles, SharedSkillBundle{
			Key:         summary.Key,
			Name:        bundle.Name,
			Description: bundle.Description,
			Content:     bundle.Content,
			SourcePath:  bundle.SourcePath,
			Provider:    rt.Provider,
			ContentHash: sharedSkillHash(bundle.Content, files),
			Files:       files,
		})
	}
	if len(bundles) == 0 {
		return nil
	}
	result, err := d.client.SyncSharedSkills(ctx, rt.ID, SharedSkillSyncPayload{Skills: bundles})
	if err != nil {
		return err
	}
	d.logger.Debug("shared skills synced", "runtime_id", rt.ID, "created", result.Created, "updated", result.Updated, "unchanged", result.Unchanged)
	return nil
}

func sharedSkillHash(content string, files []SkillFileData) string {
	h := sha256.New()
	_, _ = h.Write([]byte(content))
	for _, f := range files {
		_, _ = h.Write([]byte("\x00" + f.Path + "\x00" + f.Content))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
