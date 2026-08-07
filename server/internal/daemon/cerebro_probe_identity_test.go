package daemon

import "testing"

// TestMulticaProbeEnv_UsesDaemonProfileIdentity pins the second half of
// MUL-4634. The daemon runs from launchd or nohup with no MULTICA_TOKEN in its
// environment, so the Multica MCP channel started an unauthenticated
// `multica mcp serve`, which exits with "not authenticated" before the
// handshake. Every provider measured through that channel then reported
// not_measured with zero tools and every task claim failed with HTTP 500.
func TestMulticaProbeEnv_UsesDaemonProfileIdentity(t *testing.T) {
	original, originalURL := probeIdentity()
	t.Cleanup(func() { setProbeIdentity(original, originalURL) })

	setProbeIdentity("mul_daemon_profile_token", "https://multica-api.example.com")

	env := multicaProbeEnv()
	if env["MULTICA_TOKEN"] != "mul_daemon_profile_token" {
		t.Fatalf("MULTICA_TOKEN = %q, want the daemon's own profile token", env["MULTICA_TOKEN"])
	}
	if env["MULTICA_SERVER_URL"] != "https://multica-api.example.com" {
		t.Fatalf("MULTICA_SERVER_URL = %q, want the daemon's own server URL", env["MULTICA_SERVER_URL"])
	}
}

// An explicit environment still wins: a host that pins its own credentials for
// the probe must not be silently overridden by the daemon's profile.
func TestMulticaProbeEnv_EnvironmentWinsOverProfileIdentity(t *testing.T) {
	original, originalURL := probeIdentity()
	t.Cleanup(func() { setProbeIdentity(original, originalURL) })

	setProbeIdentity("mul_daemon_profile_token", "https://multica-api.example.com")
	t.Setenv("MULTICA_TOKEN", "mul_env_token")

	if got := multicaProbeEnv()["MULTICA_TOKEN"]; got != "mul_env_token" {
		t.Fatalf("MULTICA_TOKEN = %q, want the environment value", got)
	}
}
