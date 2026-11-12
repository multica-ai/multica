package daemon

import (
	"os"
	"path/filepath"
	"time"
)

// AttachmentCacheDirName is the directory under the workspaces root where the
// CLI keeps attachment bytes by content hash (`multica attachment download`
// reads it before downloading). The task walk skips dot directories, so it is
// expired here instead.
const AttachmentCacheDirName = ".attachment-cache"

// attachmentCacheTTL bounds an entry no read or write has touched. Reads
// refresh the mtime, so a file agents keep using stays. A removed entry only
// costs one download.
const attachmentCacheTTL = 14 * 24 * time.Hour

// pruneAttachmentCache removes cache files, including temp files a crashed
// writer left behind, whose mtime is older than ttl, then drops emptied shard
// directories. It returns the files and bytes removed.
func pruneAttachmentCache(workspacesRoot string, ttl time.Duration, now time.Time) (int, int64) {
	dir := filepath.Join(workspacesRoot, AttachmentCacheDirName)
	shards, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	removed, bytes := 0, int64(0)
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		shardDir := filepath.Join(dir, shard.Name())
		entries, err := os.ReadDir(shardDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			info, err := entry.Info()
			if err != nil || now.Sub(info.ModTime()) < ttl {
				continue
			}
			if os.Remove(filepath.Join(shardDir, entry.Name())) == nil {
				removed++
				bytes += info.Size()
			}
		}
		if remaining, err := os.ReadDir(shardDir); err == nil && len(remaining) == 0 {
			_ = os.Remove(shardDir)
		}
	}
	return removed, bytes
}
