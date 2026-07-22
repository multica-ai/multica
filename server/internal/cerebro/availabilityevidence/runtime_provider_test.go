package availabilityevidence

import "testing"

func TestRuntimeTypeForProviderMapsEveryKnownProvider(t *testing.T) {
	cases := map[string]RuntimeType{
		"firtal-gateway": RuntimeFirtalGateway,
		"claude-code":    RuntimeClaudeCode,
		"claude":         RuntimeClaudeCode,
		"codex":          RuntimeLocal,
		"gemini":         RuntimeLocal,
	}
	for provider, want := range cases {
		if got := RuntimeTypeForProvider(provider); got != want {
			t.Errorf("provider %q: got runtime %q, want %q", provider, got, want)
		}
	}
}

// An unknown provider must not be silently treated as the Gateway — the Gateway
// is the runtime with a live in-process probe, so guessing it there would let an
// unprobed runtime inherit another runtime's evidence.
func TestRuntimeTypeForProviderTreatsUnknownAsLocal(t *testing.T) {
	for _, provider := range []string{"", "something-new", "FIRTAL-GATEWAY-typo"} {
		if got := RuntimeTypeForProvider(provider); got != RuntimeLocal {
			t.Errorf("provider %q: got runtime %q, want %q", provider, got, RuntimeLocal)
		}
	}
}

func TestRuntimeTypeForProviderIgnoresCaseAndPadding(t *testing.T) {
	if got := RuntimeTypeForProvider("  Firtal-Gateway "); got != RuntimeFirtalGateway {
		t.Errorf("got %q, want %q", got, RuntimeFirtalGateway)
	}
}
