package daemon

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Mixed-version coordination tests (GH #8280 spec items 11-12).
//
// The owner claim is an OS lock, so it can only exclude processes that take it.
// A peer from a release that predates it registers and serves runtimes without
// ever looking at the lock, which is why a new daemon must refuse to activate a
// runtime while such a peer is alive.

func withStagedPeerConfig(t *testing.T, profile, serverURL string) {
	t.Helper()
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: serverURL}, profile); err != nil {
		t.Fatalf("save peer profile %q: %v", profile, err)
	}
}

// TestRuntimeCoordination_LegacyPeerBlocksActivation is spec case 11: a live
// same-machine, same-backend peer that does not advertise the capability stops
// this daemon from activating a runtime.
func TestRuntimeCoordination_LegacyPeerBlocksActivation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const backend = "https://same.example"
	withStagedPeerConfig(t, "desktop-host", backend)

	d := &Daemon{
		cfg:    Config{ServerBaseURL: backend, Profile: ""},
		logger: quietTaskLog(),
	}

	// The peer is alive but cannot be coordinated with (a pre-claim daemon).
	original := peerProbeFunc
	t.Cleanup(func() { peerProbeFunc = original })
	probed := 0
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		probed++
		return peerCoordinationStatus{Alive: true}
	}
	decision := d.checkRuntimeCoordinationPeers(context.Background())
	if probed == 0 {
		t.Fatal("no peer was probed")
	}
	if !decision.Blocked {
		t.Fatal("a live legacy peer did not block activation")
	}
	if len(decision.Peers) != 1 {
		t.Fatalf("decision.Peers = %v, want the legacy peer named", decision.Peers)
	}
}

// TestRuntimeCoordination_CoordinatingPeerDoesNotBlock is the other half: a peer
// that takes the same claim is not a blocker, because the claim itself decides
// which of the two serves a runtime.
func TestRuntimeCoordination_CoordinatingPeerDoesNotBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const backend = "https://same.example"
	withStagedPeerConfig(t, "desktop-host", backend)

	d := &Daemon{
		cfg:    Config{ServerBaseURL: backend, Profile: ""},
		logger: quietTaskLog(),
	}
	original := peerProbeFunc
	t.Cleanup(func() { peerProbeFunc = original })
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		return peerCoordinationStatus{Alive: true, Coordinates: true}
	}
	if decision := d.checkRuntimeCoordinationPeers(context.Background()); decision.Blocked {
		t.Fatalf("a coordinating peer blocked activation: %v", decision.Peers)
	}
}

// TestRuntimeCoordination_PeerOnAnotherBackendIsIgnored pins the scope: only
// peers aimed at the SAME backend can contend, because a different backend is a
// different work-state scope and a different server runtime.
func TestRuntimeCoordination_PeerOnAnotherBackendIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	withStagedPeerConfig(t, "desktop-host", "https://other.example")

	d := &Daemon{
		cfg:    Config{ServerBaseURL: "https://same.example", Profile: ""},
		logger: quietTaskLog(),
	}
	original := peerProbeFunc
	t.Cleanup(func() { peerProbeFunc = original })
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		t.Error("a peer on another backend was probed")
		return peerCoordinationStatus{Alive: true}
	}
	if peers := runtimeCoordinationPeers("https://same.example", ""); len(peers) != 0 {
		t.Fatalf("peers on another backend were enumerated: %v", peers)
	}
	if decision := d.checkRuntimeCoordinationPeers(context.Background()); decision.Blocked {
		t.Fatal("a peer on another backend blocked activation")
	}
}

// TestRuntimeCoordination_LegacyPeerGoneAllowsActivation is spec case 12: once the
// legacy peer stops answering, this daemon may take ownership normally.
//
// "Stopped" is the second half of the rule and needs both signals: the health
// probe is unanswered AND nothing holds the profile's health port. A profile
// directory outliving its daemon is stale; a peer whose port is still held is
// alive and only prevented from answering, which must keep this process in
// standby (see TestRuntimeCoordination_UnresponsivePeerBlocksActivation).
func TestRuntimeCoordination_LegacyPeerGoneAllowsActivation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const backend = "https://same.example"
	withStagedPeerConfig(t, "desktop-host", backend)

	d := &Daemon{
		cfg:    Config{ServerBaseURL: backend, Profile: ""},
		logger: quietTaskLog(),
	}
	originalProbe, originalPort := peerProbeFunc, peerHealthPortOwnedFunc
	t.Cleanup(func() {
		peerProbeFunc = originalProbe
		peerHealthPortOwnedFunc = originalPort
	})
	// Not alive, and the port is free: no process is running under that profile,
	// so the profile directory is stale and activation may proceed.
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		return peerCoordinationStatus{}
	}
	peerHealthPortOwnedFunc = func(int) bool { return false }
	if decision := d.checkRuntimeCoordinationPeers(context.Background()); decision.Blocked {
		t.Fatalf("a stopped peer blocked activation: %v", decision.Peers)
	}
}

// TestRuntimeCoordination_HealthPortsMatchTheCLI pins the peer-inventory port
// derivation against the CLI copy in cmd/multica, so a peer can never be probed
// on a port its daemon does not listen on.
func TestRuntimeCoordination_HealthPortsMatchTheCLI(t *testing.T) {
	// Values pinned by cmd/multica.TestWorkStateSharedWhileProfilesStayIsolated.
	if got := healthPortForProfile(""); got != DefaultHealthPort {
		t.Fatalf("default health port = %d, want %d", got, DefaultHealthPort)
	}
	for _, profile := range []string{"desktop-host", "staging"} {
		var sum int
		for _, b := range []byte(profile) {
			sum += int(b)
		}
		want := DefaultHealthPort + 1 + (sum % 1000)
		if got := healthPortForProfile(profile); got != want {
			t.Fatalf("health port for %q = %d, want %d", profile, got, want)
		}
	}
}
