package featureflagdispatch

// RuntimeBriefSlimFlag is the daemon-bound flag that switches runtime brief
// rendering between the legacy verbose prompt and the slim prompt.
const RuntimeBriefSlimFlag = "runtime_brief_slim"

// DaemonBoundFlags lists every feature flag whose evaluated decision is safe
// and useful to send to daemons. Flags not listed here stay server-only.
var DaemonBoundFlags = []string{
	RuntimeBriefSlimFlag,
}
