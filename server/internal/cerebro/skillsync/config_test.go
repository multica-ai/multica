package skillsync

import "testing"

// clearSyncEnv blanks every env var LoadConfig reads so each case starts clean.
func clearSyncEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CEREBRO_SKILLS_SYNC_REPO", "CEREBRO_SKILLS_SYNC_TOKEN", "GITHUB_TOKEN",
		"CEREBRO_SKILLS_SYNC_BRANCH", "CEREBRO_SKILLS_SYNC_INTERVAL",
		"CEREBRO_SKILLS_SYNC_DEFAULT_DOMAIN", "CEREBRO_SKILLS_SYNC_WORKDIR",
		"CEREBRO_SKILLS_SYNC_AUTHOR_NAME", "CEREBRO_SKILLS_SYNC_AUTHOR_EMAIL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadConfigCredentialResolution(t *testing.T) {
	repo := "https://github.com/firtal-group/firtal-skills.git"

	t.Run("disabled when repo unset", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("GITHUB_TOKEN", "ghp_x")
		if LoadConfig().Enabled {
			t.Fatal("expected disabled when CEREBRO_SKILLS_SYNC_REPO is unset")
		}
	})

	t.Run("disabled when no credential at all", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("CEREBRO_SKILLS_SYNC_REPO", repo)
		if LoadConfig().Enabled {
			t.Fatal("expected disabled when neither sync token nor GITHUB_TOKEN is set")
		}
	})

	t.Run("reuses GITHUB_TOKEN — no dedicated token needed", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("CEREBRO_SKILLS_SYNC_REPO", repo)
		t.Setenv("GITHUB_TOKEN", "ghp_shared")
		cfg := LoadConfig()
		if !cfg.Enabled {
			t.Fatal("expected enabled when repo is set and GITHUB_TOKEN is present")
		}
		if cfg.Token != "ghp_shared" {
			t.Errorf("Token = %q, want the shared GITHUB_TOKEN", cfg.Token)
		}
	})

	t.Run("dedicated token wins over GITHUB_TOKEN", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("CEREBRO_SKILLS_SYNC_REPO", repo)
		t.Setenv("GITHUB_TOKEN", "ghp_shared")
		t.Setenv("CEREBRO_SKILLS_SYNC_TOKEN", "ghp_dedicated")
		if got := LoadConfig().Token; got != "ghp_dedicated" {
			t.Errorf("Token = %q, want the dedicated sync token", got)
		}
	})
}
