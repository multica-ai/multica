package cerebra

import "testing"

func TestPolicy_Allow(t *testing.T) {
	policy := &Policy{
		ModelAllowlist: []string{"claude-haiku-3-5", "claude-sonnet-4-5"},
		ModelDenylist:  []string{"claude-opus-legacy"},
	}

	if !policy.Allow("rt-1", "claude-haiku-3-5") {
		t.Errorf("expected haiku to be allowed")
	}

	if policy.Allow("rt-1", "gpt-4o") {
		t.Errorf("expected gpt-4o to be blocked by allowlist")
	}

	if policy.Allow("rt-1", "claude-opus-legacy") {
		t.Errorf("expected claude-opus-legacy to be blocked by denylist")
	}
}
