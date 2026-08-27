//go:build !windows

package daemon

import "syscall"

// diskFreePercent returns percent free on the filesystem holding path
// (available/total blocks), or nil when the probe fails. Statfs is a quick
// metadata read — safe to call per heartbeat tick.
func diskFreePercent(path string) *float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return nil
	}
	pct := float64(st.Bavail) * 100 / float64(st.Blocks)
	return &pct
}
