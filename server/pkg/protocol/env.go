package protocol

import "strings"

// DaemonReservedEnvPrefix is the namespace the daemon owns in the agent
// process environment: every MULTICA_* variable is constructed by the daemon
// from the current task and must not be overridden by anything the server or
// an agent configuration supplies.
const DaemonReservedEnvPrefix = "MULTICA_"

// daemonReservedEnvNames are the daemon-internal and critical system variables
// the daemon refuses to let agent custom_env or server-issued task tokens
// override. This is the single source for that list: the daemon enforces it at
// injection time (isBlockedEnvKey) and the server's task-token catalog checks
// template env names against it at boot, so a name the daemon would silently
// drop is a startup error instead. Keep it here, not copied in either place —
// the two lists had already drifted once.
var daemonReservedEnvNames = map[string]struct{}{
	"HOME": {}, "PATH": {}, "USER": {}, "SHELL": {}, "TERM": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {},
	"CODEX_HOME": {}, "REASONIX_STATE_HOME": {}, "CURSOR_DATA_DIR": {},
	// execenv.CursorMcpAuthSourceEnv; spelled out because execenv is a daemon
	// package and this one must stay dependency-free. A daemon test pins the
	// two spellings together.
	"CURSOR_MCP_AUTH_SOURCE": {},
	"OPENCLAW_CONFIG_PATH":   {}, "OPENCLAW_INCLUDE_ROOTS": {},
}

// IsDaemonReservedEnvName reports whether name is one the daemon will refuse
// to inject from custom_env or a task token. Case-insensitive, matching the
// daemon's own check.
func IsDaemonReservedEnvName(name string) bool {
	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, DaemonReservedEnvPrefix) {
		return true
	}
	_, reserved := daemonReservedEnvNames[upper]
	return reserved
}
