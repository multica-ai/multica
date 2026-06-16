// CEREBRO-PATCH(fork-auto-update): fork-specific regression guards for the
// auto-update default. Upstream defaults self-host to OFF (MUL-2381); the fork
// flips that to ON for any real fleet server because the self-updater targets
// the fork's own release channel (forkdist) — see config.go. These tests live
// in a cerebro-prefixed file so the intent survives upstream syncs. (TECH-3602)
package daemon

import "testing"

// TestLoadConfig_AutoUpdateDefault_ForkRemoteOn is the regression guard for
// TECH-3602: a fork daemon pointed at a real (non-loopback) self-hosted server
// must default AutoUpdateEnabled to true, so the fleet self-updates from the
// fork's own channel without anyone running brew/restart by hand. This is the
// deliberate inverse of upstream's MUL-2381 self-host-off default, which is
// safe here because forkdist points the updater at firtal-group/homebrew-tap
// (binaries that already carry every cerebro patch).
func TestLoadConfig_AutoUpdateDefault_ForkRemoteOn(t *testing.T) {
	stageFakeAgent(t)
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "wss://cerebro.firtal.example/ws",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = false for fork remote server, want true")
	}
}

// TestLoadConfig_AutoUpdateDefault_ForkLocalhostOff confirms a developer's
// local daemon stays off by default even on the fork — only real fleet servers
// flip on. (Source builds are additionally skipped by the auto-update loop's
// isReleaseVersion gate; this asserts the config-level default.)
func TestLoadConfig_AutoUpdateDefault_ForkLocalhostOff(t *testing.T) {
	stageFakeAgent(t)
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true for localhost, want false")
	}
}

// TestLoadConfig_AutoUpdateEnv_ForcesOffForForkRemote is the kill switch: an
// operator can opt any fork machine out with MULTICA_DAEMON_AUTO_UPDATE=false,
// overriding the new fleet-on default — the safety valve Jesper asked for.
func TestLoadConfig_AutoUpdateEnv_ForcesOffForForkRemote(t *testing.T) {
	stageFakeAgent(t)
	t.Setenv("MULTICA_DAEMON_AUTO_UPDATE", "false")
	cfg, err := LoadConfig(Overrides{
		ServerURL:      "wss://cerebro.firtal.example/ws",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoUpdateEnabled {
		t.Fatalf("AutoUpdateEnabled = true after MULTICA_DAEMON_AUTO_UPDATE=false, want false")
	}
}
