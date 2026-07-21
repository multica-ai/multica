package availabilityevidence

import "strings"

// Runtime provider strings as stored on the runtime row. They are the runtime's
// own vocabulary; RuntimeType is this package's. Mapping them here keeps the
// translation in one place instead of at every read site.
const (
	providerFirtalGateway = "firtal-gateway"
	providerClaudeCode    = "claude-code"
	providerClaude        = "claude"
)

// RuntimeTypeForProvider maps a runtime row's provider onto the runtime family
// whose evidence applies to an agent running there.
//
// An unrecognised provider maps to RuntimeLocal, never to RuntimeFirtalGateway.
// That direction matters: the Gateway is the runtime this server hosts and can
// probe in-process, so a wrong guess there would hand an unprobed runtime the
// Gateway's proofs and present them as this agent's reality. RuntimeLocal is the
// family whose evidence must be reported in, so an unknown provider degrades to
// "not proven" — which is the truth about a runtime nobody has probed.
func RuntimeTypeForProvider(provider string) RuntimeType {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case providerFirtalGateway:
		return RuntimeFirtalGateway
	case providerClaudeCode, providerClaude:
		return RuntimeClaudeCode
	default:
		return RuntimeLocal
	}
}
