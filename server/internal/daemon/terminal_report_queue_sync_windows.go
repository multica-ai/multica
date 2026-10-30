//go:build windows

package daemon

// Windows does not support fsync on a directory handle. The report file itself
// is flushed before the atomic rename; duplicate replay remains safe if a crash
// loses the directory entry or resurrects an acknowledged one. Go's 0600/0700
// mode arguments also do not create Windows ACLs: confidentiality relies on the
// current user's profile/workspace ACL. No credential is stored in the queue.
func syncTerminalReportDir(string) error { return nil }
