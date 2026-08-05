package account

import "testing"

// The expected service names are golden values taken from Claude Code's own
// naming (`Claude Code${OAUTH_FILE_SUFFIX}-credentials${-sha256(NFC(dir))[:8]}`,
// production ships an empty OAUTH_FILE_SUFFIX). Recomputing the hash inside
// the test would only prove the function agrees with itself.
func TestClaudeKeychainService(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		configDir string
		want      string
	}{
		{"default profile", "", "Claude Code-credentials"},
		{"blank is default profile", "   ", "Claude Code-credentials"},
		{"named profile", "/Users/hvejsel/.claude-account-17", "Claude Code-credentials-13c6d484"},
		{"non-ascii profile is NFC normalized", "/Users/hvejsel/.cläude", "Claude Code-credentials-a64f2315"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClaudeKeychainService(tc.configDir); got != tc.want {
				t.Fatalf("ClaudeKeychainService(%q) = %q, want %q", tc.configDir, got, tc.want)
			}
		})
	}
}

func TestClaudeKeychainAccount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		username string
		want     string
	}{
		{"plain username", "hvejsel", "hvejsel"},
		{"dots and dashes allowed", "jesper.hvejsel-1", "jesper.hvejsel-1"},
		{"empty falls back", "", ClaudeKeychainFallbackAccount},
		{"space falls back", "jesper hvejsel", ClaudeKeychainFallbackAccount},
		{"non-ascii falls back", "jespær", ClaudeKeychainFallbackAccount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClaudeKeychainAccount(tc.username); got != tc.want {
				t.Fatalf("ClaudeKeychainAccount(%q) = %q, want %q", tc.username, got, tc.want)
			}
		})
	}
}
