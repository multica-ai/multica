package agentroute

// Mode is the rollout posture of transactional adaptive admission.
type Mode string

const (
	ModeOff    Mode = "off"
	ModeShadow Mode = "shadow"
	ModeActive Mode = "active"
)
