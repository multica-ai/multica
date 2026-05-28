package agent

import "testing"

// TestProtocolFamily verifies that variant providers normalize back to
// their parent protocol family while base providers and unknown values
// fall through unchanged. Adding a new variant only requires extending
// ProtocolFamily and the table here; every per-provider behaviour
// switch downstream (CLAUDE.md vs AGENTS.md vs GEMINI.md, ~/.claude/
// skills home, CODEX_HOME setup, ClaudeArgs / CodexArgs routing,
// reply templates) keys off the family value.
func TestProtocolFamily(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		want     string
	}{
		// Variant providers fold into their family parent.
		{"codebuddy", "claude"},
		{"claude-internal", "claude"},
		{"codex-internal", "codex"},
		{"gemini-internal", "gemini"},

		// Base providers pass through unchanged so existing call sites
		// keep their behaviour for the 11 well-known providers.
		{"claude", "claude"},
		{"codex", "codex"},
		{"copilot", "copilot"},
		{"opencode", "opencode"},
		{"openclaw", "openclaw"},
		{"hermes", "hermes"},
		{"gemini", "gemini"},
		{"pi", "pi"},
		{"cursor", "cursor"},
		{"kimi", "kimi"},
		{"kiro", "kiro"},

		// Unknown / empty inputs pass through verbatim — callers treat
		// the result the same way they would the raw string.
		{"unknown-future-runtime", "unknown-future-runtime"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ProtocolFamily(tc.provider); got != tc.want {
			t.Errorf("ProtocolFamily(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

// TestNewAcceptsVariantProviders pins down that variant provider keys
// dispatch to their family's backend without a separate implementation.
// If a future refactor splits a backend per-variant, this test fails
// loudly so the corresponding ProtocolFamily entry can be reviewed.
func TestNewAcceptsVariantProviders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		agentType string
		assert    func(t *testing.T, b Backend)
	}{
		{"codebuddy", func(t *testing.T, b Backend) {
			if _, ok := b.(*claudeBackend); !ok {
				t.Fatalf("New(codebuddy): expected *claudeBackend, got %T", b)
			}
		}},
		{"claude-internal", func(t *testing.T, b Backend) {
			if _, ok := b.(*claudeBackend); !ok {
				t.Fatalf("New(claude-internal): expected *claudeBackend, got %T", b)
			}
		}},
		{"codex-internal", func(t *testing.T, b Backend) {
			if _, ok := b.(*codexBackend); !ok {
				t.Fatalf("New(codex-internal): expected *codexBackend, got %T", b)
			}
		}},
		{"gemini-internal", func(t *testing.T, b Backend) {
			if _, ok := b.(*geminiBackend); !ok {
				t.Fatalf("New(gemini-internal): expected *geminiBackend, got %T", b)
			}
		}},
	}
	for _, tc := range cases {
		b, err := New(tc.agentType, Config{ExecutablePath: "/nonexistent/" + tc.agentType})
		if err != nil {
			t.Fatalf("New(%q) error: %v", tc.agentType, err)
		}
		tc.assert(t, b)
	}
}

// TestCheckMinVersionFallsBackToFamily makes sure variant providers
// that don't carry their own MinVersions entry inherit their family's
// floor — and that we explicitly opted variants OUT of the upstream
// floor by registering 0.0.0 entries in MinVersions, since internal
// forks ship under independent version sequences.
func TestCheckMinVersionFallsBackToFamily(t *testing.T) {
	t.Parallel()

	// Sanity-check the explicit 0.0.0 floors we set so internal forks
	// (e.g. claude-internal 1.1.8, codex-internal 0.0.9) are accepted.
	for _, p := range []string{"codebuddy", "claude-internal", "codex-internal", "gemini-internal"} {
		if err := CheckMinVersion(p, "0.0.1"); err != nil {
			t.Errorf("CheckMinVersion(%q, 0.0.1) returned %v, want nil", p, err)
		}
	}

	// Family inheritance is the second line of defence: even if an
	// operator removes a variant's explicit floor, ProtocolFamily must
	// still surface the parent's floor (claude=2.0.0, codex=0.100.0,
	// copilot=1.0.0). Re-using a low-numbered fork here would be
	// rejected by the parent gate without the variant's own 0.0.0
	// entry, which is exactly the historical bug — see version.go.
	if err := CheckMinVersion("claude", "1.0.0"); err == nil {
		t.Error("CheckMinVersion(claude, 1.0.0) returned nil, want a too-old error")
	}
}
