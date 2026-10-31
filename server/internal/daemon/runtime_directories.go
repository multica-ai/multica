package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Browsing is read-only. Execution separately checks write access with
// validateLocalPath, whose probe file must never be created by the browser.
func listRuntimeDirectories(path string) (*protocol.DirectoryListing, error) {
	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("directory path must be absolute")
	}
	path = filepath.Clean(path)
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve directory: %w", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	listing := &protocol.DirectoryListing{Path: path, Entries: []protocol.DirectoryEntry{}, Selectable: true}
	if parent := filepath.Dir(path); parent != path {
		listing.ParentPath = &parent
	}
	if _, blocked := isBlacklistedLocalPath(path); blocked {
		listing.Selectable = false
	}
	if _, blocked := isBlacklistedRealPath(realPath); blocked {
		listing.Selectable = false
	}
	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		directory := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(childPath)
			directory = err == nil && info.IsDir()
		}
		if directory {
			listing.Entries = append(listing.Entries, protocol.DirectoryEntry{Name: entry.Name(), Path: childPath})
		}
	}
	return listing, nil
}

func (d *Daemon) handleDirectoryList(ctx context.Context, runtimeID string, pending protocol.DaemonHeartbeatPendingDirectories) {
	var result *protocol.DirectoryListing
	var err error
	if pending.DaemonID != d.cfg.DaemonID {
		err = fmt.Errorf("directory request belongs to another daemon")
	} else {
		result, err = listRuntimeDirectories(pending.Path)
	}
	payload := map[string]any{"status": "completed", "result": result}
	if err != nil {
		payload = map[string]any{"status": "failed", "error": err.Error()}
	}
	d.reportRuntimeResultWithRetry(ctx, "directories", runtimeID, pending.ID, func(ctx context.Context) error {
		return d.client.ReportDirectoryListResult(ctx, runtimeID, pending.ID, payload)
	})
}
