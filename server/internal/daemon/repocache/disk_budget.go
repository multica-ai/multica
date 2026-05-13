package repocache

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DiskBudget gauges disk usage of the per-task worktree tree and decides
// whether a new worktree can be spawned without exceeding the configured
// per-agent and global caps.
//
// PUL-94 constraints (from plan Constraints section):
//
//   - Per-agent: hard cap 5 concurrent worktrees, soft warn 3.
//   - Global: total /srv/agent-worktrees/* < 50% of free space on its mount.
//
// Per-agent count is cheap (one ReadDir of the worktrees root, no caching).
// Global free/used are cached for 30s to avoid rescan storms when multiple
// tasks spawn back-to-back.
//
// All paths are absolute. Zero values are safe (used/free will return errors
// until WorktreesRoot is set).
type DiskBudget struct {
	// WorktreesRoot is the directory holding per-task worktrees, e.g.
	// "/srv/agent-worktrees". Counted entries are direct children matching
	// "<agent>-*" (the per-task naming pattern). Legacy "<agent>" entries
	// without a suffix are NOT counted toward per-agent cap.
	WorktreesRoot string

	// CacheTTL controls how long Global* results are reused. Zero → 30s.
	CacheTTL time.Duration

	mu         sync.Mutex
	cachedFree int64
	cachedUsed int64
	computedAt time.Time
}

// defaultCacheTTL is the staleness window for cached global gauges. Tuned to
// avoid rescan on back-to-back spawn bursts (~ms apart) while staying fresh
// enough for "another task completed, can I spawn now" retries.
const defaultCacheTTL = 30 * time.Second

func (b *DiskBudget) ttl() time.Duration {
	if b.CacheTTL > 0 {
		return b.CacheTTL
	}
	return defaultCacheTTL
}

// PerAgentWorktreeCount returns the number of per-task worktree directories
// in WorktreesRoot whose name starts with "<agentName>-" (the per-task naming
// pattern, distinguishing them from legacy "<agentName>" entries). Always
// fresh — no cache, no syscall fanout.
//
// Returns 0 + error if WorktreesRoot is unreadable. Returns 0 + nil if the
// dir is empty.
func (b *DiskBudget) PerAgentWorktreeCount(agentName string) (int, error) {
	if b.WorktreesRoot == "" {
		return 0, fmt.Errorf("WorktreesRoot not set")
	}
	entries, err := os.ReadDir(b.WorktreesRoot)
	if err != nil {
		return 0, fmt.Errorf("read worktrees root: %w", err)
	}

	prefix := agentName + "-"
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			n++
		}
	}
	return n, nil
}

// GlobalFreeBytes returns free bytes on the filesystem containing
// WorktreesRoot. Result is cached for CacheTTL (default 30s) — repeated
// calls within the window reuse the prior reading.
func (b *DiskBudget) GlobalFreeBytes() (int64, error) {
	free, _, err := b.globalStats()
	return free, err
}

// GlobalUsedBytes returns the cumulative size of WorktreesRoot subtree in
// bytes. Result is cached for CacheTTL.
func (b *DiskBudget) GlobalUsedBytes() (int64, error) {
	_, used, err := b.globalStats()
	return used, err
}

// globalStats refreshes both gauges under a single lock if the cache has
// expired, then returns them. Both gauges share one cache slot so we never
// half-refresh.
func (b *DiskBudget) globalStats() (free, used int64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.computedAt.IsZero() && time.Since(b.computedAt) < b.ttl() {
		return b.cachedFree, b.cachedUsed, nil
	}
	if b.WorktreesRoot == "" {
		return 0, 0, fmt.Errorf("WorktreesRoot not set")
	}

	free, err = statfsFreeBytes(b.WorktreesRoot)
	if err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", b.WorktreesRoot, err)
	}
	used, err = treeSize(b.WorktreesRoot)
	if err != nil {
		return 0, 0, fmt.Errorf("tree size %s: %w", b.WorktreesRoot, err)
	}

	b.cachedFree = free
	b.cachedUsed = used
	b.computedAt = time.Now()
	return free, used, nil
}

// GlobalFractionUsed returns the share of (used + free) occupied by the
// worktrees tree, in [0, 1]. Useful for the "< 50% of free space" check —
// callers can simply compare against 0.5. Returns 0 + error on stat failure.
//
// Note: "fraction" here is used / (used + free), not used / mount_total.
// Aligns with `df`'s "use of the available space on this volume" framing
// rather than "of the disk." Both gauges come from the cached snapshot.
func (b *DiskBudget) GlobalFractionUsed() (float64, error) {
	free, used, err := b.globalStats()
	if err != nil {
		return 0, err
	}
	if free+used == 0 {
		return 0, nil
	}
	return float64(used) / float64(free+used), nil
}

// statfsFreeBytes returns bytes available to non-root on the filesystem
// containing path. Uses syscall.Statfs; cast accommodates the Bsize field's
// platform-dependent type (int64 on Linux, uint32 on Darwin).
func statfsFreeBytes(path string) (int64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, err
	}
	// Bavail = blocks available to non-root. Bsize = block size in bytes.
	// Multiply as uint64 then convert; the product is unlikely to exceed
	// int64 max on any realistic filesystem (>9 exabytes).
	return int64(s.Bavail) * int64(s.Bsize), nil
}

// treeSize walks the directory tree rooted at root and sums file sizes.
// Symlinks are not followed; their own size (the link target string) is
// counted. Errors on individual files are logged via the walk's error chan
// behavior — io/fs.WalkDir surfaces them at the failing entry.
//
// Linux equivalent of `du -sB1 <root>` (apparent size, not block-allocated).
// For PUL-94's budget we care about "how much does this take up logically,"
// not "how many filesystem blocks does it consume," so apparent-size suffices.
func treeSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Don't fail the whole walk on a transiently-missing file
			// (e.g. a worktree just got swept while we were walking).
			// Skip silently — the next refresh will resync.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			if os.IsNotExist(ierr) {
				return nil
			}
			return ierr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
