package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func withStagedPeerConfig(t *testing.T, profile, serverURL string) {
	t.Helper()
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: serverURL}, profile); err != nil {
		t.Fatalf("save peer profile %q: %v", profile, err)
	}
}

func stubRuntimeCoordination(t *testing.T, probe func(context.Context, string) peerCoordinationStatus, portOwned func(int) bool) {
	t.Helper()
	originalProbe, originalPort := peerProbeFunc, peerHealthPortOwnedFunc
	t.Cleanup(func() { peerProbeFunc, peerHealthPortOwnedFunc = originalProbe, originalPort })
	if probe != nil {
		peerProbeFunc = probe
	}
	if portOwned != nil {
		peerHealthPortOwnedFunc = portOwned
	}
}

func TestRuntimeCoordination_PeerDecision(t *testing.T) {
	const same = "https://same.example"
	const other = "https://other.example"
	cases := []struct {
		name, saved string
		status      peerCoordinationStatus
		portHeld    bool
		blocked     bool
	}{
		{"same legacy", same, peerCoordinationStatus{Alive: true, Backend: same}, true, true},
		{"same coordinating", same, peerCoordinationStatus{Alive: true, Backend: same, Coordinates: true}, true, false},
		{"saved differs from actual match", other, peerCoordinationStatus{Alive: true, Backend: same}, true, true},
		{"saved matches but actual differs", same, peerCoordinationStatus{Alive: true, Backend: other}, true, false},
		{"unresponsive held", same, peerCoordinationStatus{}, true, true},
		{"stale port free", same, peerCoordinationStatus{}, false, false},
		{"missing config held", "", peerCoordinationStatus{}, true, true},
		{"missing config unrelated", "", peerCoordinationStatus{Alive: true, Backend: other}, true, false},
		{"unreadable config held", "invalid", peerCoordinationStatus{}, true, true},
		{"unreadable config unrelated", "invalid", peerCoordinationStatus{Alive: true, Backend: other}, true, false},
		{"live unknown backend", same, peerCoordinationStatus{Alive: true, Coordinates: true}, true, true},
		{"malformed backend", same, peerCoordinationStatus{Alive: true, Backend: "http://"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if tc.saved != "" && tc.saved != "invalid" {
				withStagedPeerConfig(t, "desktop-host", tc.saved)
			} else {
				profileDir, err := cli.ProfileDir("desktop-host")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(profileDir, 0700); err != nil {
					t.Fatal(err)
				}
				if tc.saved == "invalid" {
					configPath, err := cli.CLIConfigPathForProfile("desktop-host")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(configPath, []byte("{broken"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			d := &Daemon{cfg: Config{ServerBaseURL: same}, logger: quietTaskLog()}
			stubRuntimeCoordination(t, func(context.Context, string) peerCoordinationStatus { return tc.status }, func(int) bool { return tc.portHeld })
			decision := d.checkRuntimeCoordinationPeers(context.Background())
			if decision.Blocked != tc.blocked {
				t.Fatalf("Blocked = %v, want %v (%v)", decision.Blocked, tc.blocked, decision.Peers)
			}
		})
	}
}

func TestRuntimeCoordination_ProbeUsesLiveServerURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"server_url":"https://actual.example","runtime_coordination_version":1}`))
	}))
	defer srv.Close()
	got := probeRuntimeCoordinationPeer(context.Background(), srv.URL)
	if !got.Alive || !got.Coordinates || got.Backend != "https://actual.example" {
		t.Fatalf("health identity = %+v", got)
	}
}

func TestRuntimeCoordination_DefaultPeerExistsWithoutProfilesDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	peers, err := runtimeCoordinationPeers("desktop-host")
	if err != nil || len(peers) != 1 || peers[0].Profile != "" {
		t.Fatalf("default peer discovery = %+v, %v", peers, err)
	}
}

func TestRuntimeCoordination_HealthPortsMatchTheCLI(t *testing.T) {
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
