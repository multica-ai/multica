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
