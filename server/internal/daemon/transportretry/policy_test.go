package transportretry

import "testing"

func TestDefaultPolicyDelayLadderBeforeFreshSession(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicies()[0]
	if got, want := delayForLaunch(policy, 0), 0; got != want {
		t.Fatalf("delayForLaunch(index 0) = %d, want %d", got, want)
	}
	if got, want := delayForLaunch(policy, 1), 0; got != want {
		t.Fatalf("delayForLaunch(index 1) = %d, want %d", got, want)
	}
	if got, want := delayForLaunch(policy, 2), 5000; got != want {
		t.Fatalf("delayForLaunch(index 2) = %d, want %d before fresh_session", got, want)
	}
	if got, want := sessionModeForLaunch(policy, 2), SessionRetryFresh; got != want {
		t.Fatalf("sessionModeForLaunch(index 2) = %q, want %q", got, want)
	}
}
