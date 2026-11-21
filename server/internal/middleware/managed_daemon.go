package middleware

import "strings"

// managedDaemonPrefixes maps the reserved daemon_id prefix of each server-run
// cloud agent bridge to the one provider its token may register.
var managedDaemonPrefixes = map[string]string{
	"qoder-managed-":  "qoder_cloud",
	"claude-managed-": "claude_managed",
}

// ManagedDaemonProvider returns the provider of a server-run bridge's daemon
// ID, or "" for an ordinary daemon.
func ManagedDaemonProvider(daemonID string) string {
	for prefix, provider := range managedDaemonPrefixes {
		if strings.HasPrefix(daemonID, prefix) {
			return provider
		}
	}
	return ""
}
