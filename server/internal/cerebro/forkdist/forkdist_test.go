package forkdist

import "testing"

func TestUpdateRepo_DefaultIsFork(t *testing.T) {
	t.Setenv("MULTICA_UPDATE_REPO", "")
	if got := UpdateRepo(); got != "firtal-group/firtal-cerebro" {
		t.Fatalf("default UpdateRepo = %q, want firtal-group/firtal-cerebro", got)
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
	if got := UpdateRepo(); got != "firtal-group/firtal-cerebro" {
		t.Fatalf("whitespace-only env should fall back to default, got %q", got)
	}
}
