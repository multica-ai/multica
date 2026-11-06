package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

const chatArtifactSweepFile = ".gc_chat_artifacts"

func (d *Daemon) idleChatArtifactEligible(meta *execenv.GCMeta) bool {
	return !meta.LocalDirectory && d.cfg.GCArtifactTTL > 0 &&
		!meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL
}

// cleanIdleChatArtifacts runs under the env root reservation. A chat can run
// again while its server status is being checked, so revalidate the completion
// before touching the workdir or recording a sweep.
func (d *Daemon) cleanIdleChatArtifacts(ctx context.Context, taskDir string, checked *execenv.GCMeta, stats *gcStats) {
	if checked == nil || ctx.Err() != nil {
		return
	}
	meta, err := execenv.ReadGCMeta(taskDir)
	if err != nil || meta.Kind != execenv.GCKindChat || !d.idleChatArtifactEligible(meta) ||
		meta.WorkspaceID != checked.WorkspaceID || meta.ChatSessionID != checked.ChatSessionID ||
		meta.TaskID != checked.TaskID || !meta.CompletedAt.Equal(checked.CompletedAt) {
		return
	}

	matcher := newArtifactMatcher(d.cfg.GCArtifactPatterns, execenv.ManagedReclaimableArtifactSubpaths())
	marker := chatArtifactSweepMarker(meta, matcher)
	if chatArtifactSweepRecorded(taskDir, marker) {
		return
	}

	removed, size, perPattern, complete := d.sweepTaskArtifacts(ctx, taskDir, matcher)
	recordArtifactCleanup(stats, removed, size, perPattern)
	if complete && ctx.Err() == nil {
		if err := writeChatArtifactSweep(filepath.Join(taskDir, chatArtifactSweepFile), marker[:]); err != nil {
			d.logger.Warn("gc: write chat artifact sweep marker failed", "dir", taskDir, "error", err)
		}
	}
}

func chatArtifactSweepMarker(meta *execenv.GCMeta, matcher artifactMatcher) [sha256.Size]byte {
	return sha256.Sum256(fmt.Appendf(nil, "v1:%q:%q:%q:%q:%q:%q", meta.WorkspaceID, meta.ChatSessionID, meta.TaskID,
		meta.CompletedAt.UTC().Format(time.RFC3339Nano), slices.Sorted(maps.Keys(matcher.basenames)), matcher.managedSubpaths()))
}

func chatArtifactSweepRecorded(taskDir string, marker [sha256.Size]byte) bool {
	path := filepath.Join(taskDir, chatArtifactSweepFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(marker)) {
		return false
	}
	saved, err := os.ReadFile(path)
	return err == nil && bytes.Equal(saved, marker[:])
}

func writeChatArtifactSweep(path string, marker []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), chatArtifactSweepFile+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(marker)
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}
