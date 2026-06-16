package forkdist

import "testing"

func TestUpdateRepo_DefaultIsPublicTap(t *testing.T) {
	t.Setenv("MULTICA_UPDATE_REPO", "")
	// MUST be the public tap repo: the daemon downloads assets unauthenticated,
	// so the private code repo (firtal-group/firtal-cerebro) would 404.
	if got := UpdateRepo(); got != "firtal-group/homebrew-tap" {
		t.Fatalf("default UpdateRepo = %q, want firtal-group/homebrew-tap", got)
	}
}

func TestUpdateRepo_EnvOverride(t *testing.T) {
	t.Setenv("MULTICA_UPDATE_REPO", "multica-ai/multica")
	if got := UpdateRepo(); got != "multica-ai/multica" {
		t.Fatalf("override UpdateRepo = %q, want multica-ai/multica", got)
	}
}

func TestBrewTap_DefaultIsFork(t *testing.T) {
	t.Setenv("MULTICA_BREW_TAP", "")
	if got := BrewTap(); got != "firtal-group/tap/multica" {
		t.Fatalf("default BrewTap = %q, want firtal-group/tap/multica", got)
	}
}

func TestEnvOr_TrimsWhitespace(t *testing.T) {
	t.Setenv("MULTICA_UPDATE_REPO", "  \t ")
	if got := UpdateRepo(); got != "firtal-group/homebrew-tap" {
		t.Fatalf("whitespace-only env should fall back to default, got %q", got)
	}
}

func TestAutoUpdateDefaultOn(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		// Real fleet servers -> ON: the fork self-updates from its own channel.
		{"https remote", "https://cerebro.example.com", true},
		{"http remote with port", "http://10.20.30.40:8080", true},
		{"ws remote", "ws://runtime.firtal.example/ws", true},
		{"public ip literal", "https://93.184.216.34", true},
		// Local dev -> OFF: a developer's machine is never surprised by a self-update.
		{"localhost", "http://localhost:8080", false},
		{"localhost ws", "ws://localhost:8080/ws", false},
		{"127.0.0.1", "http://127.0.0.1:8080", false},
		{"127.0.0.x", "http://127.1.2.3:8080", false},
		{"ipv6 loopback", "http://[::1]:8080", false},
		{"sub.localhost", "http://api.localhost:8080", false},
		// Unparseable / empty -> OFF (conservative).
		{"empty", "", false},
		{"no host", "://nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AutoUpdateDefaultOn(tc.url); got != tc.want {
				t.Fatalf("AutoUpdateDefaultOn(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
