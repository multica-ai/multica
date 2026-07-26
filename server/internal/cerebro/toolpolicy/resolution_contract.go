package toolpolicy

import "strings"

// agentBrowserCapabilityKey is the tool_key of the agent-browser unix-socket
// gate. The gate is deny-by-default and must never become openable.
const agentBrowserCapabilityKey = "tools:agent-browser"

// DeclaredResolutionMode is the single resolution-contract registry for
// ordinary tool-policy keys. Read surfaces and enforcement points call this
// function instead of maintaining their own hard-floor key lists.
//
// generalMode is the workspace's configured mode for ordinary permissions.
// Safety floors always return ModeHardFloor regardless of that setting.
func DeclaredResolutionMode(toolKey string, generalMode Mode) Mode {
	if generalMode != ModeOpenable || isHardFloorPermission(toolKey) {
		return ModeHardFloor
	}
	return ModeOpenable
}

func isHardFloorPermission(toolKey string) bool {
	if toolKey == agentBrowserCapabilityKey || toolKey == RegistryToolKey {
		return true
	}
	if strings.HasPrefix(toolKey, "credential.") {
		return true
	}
	return ActionOf(toolKey) != ""
}
