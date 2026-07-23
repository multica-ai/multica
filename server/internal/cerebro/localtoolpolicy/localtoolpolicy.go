// Package localtoolpolicy maps unified policy verdicts to mandatory actions for
// daemon-spawned local runtimes.
package localtoolpolicy

import "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"

type DecisionKind string

const (
	KindAllow DecisionKind = "allow"
	KindDeny  DecisionKind = "deny"
	KindAsk   DecisionKind = "ask"
)

type Decision struct {
	Kind       DecisionKind
	Allowed    bool
	Enforced   bool
	WouldBlock bool
	Observed   toolpolicy.Setting
	Reason     string
}

func (d Decision) NeedsApproval() bool { return d.Kind == KindAsk }

// Decide always enforces. Unknown settings fail closed.
func Decide(eff toolpolicy.Effective) Decision {
	decision := Decision{
		Observed: eff.Setting,
		Reason:   eff.Reason,
		Enforced: true,
	}
	switch eff.Setting {
	case toolpolicy.SettingAllow:
		decision.Kind = KindAllow
		decision.Allowed = true
	case toolpolicy.SettingAsk:
		decision.Kind = KindAsk
		decision.WouldBlock = true
	default:
		decision.Kind = KindDeny
		decision.WouldBlock = true
	}
	return decision
}
