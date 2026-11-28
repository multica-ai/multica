package daemon

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultDeviceName(t *testing.T) {
	for _, tc := range []struct {
		name, account, envUser, envUsername, want string
		lookupErr                                 bool
	}{
		{"system user wins", "alice", "other", "another", "alice@build-01", false},
		{"Windows domain", `CORP\bob`, "", "", "bob@build-01", false},
		{"lookup fails", "", "bob", "other", "bob@build-01", true},
		{"empty account", " ", "bob", "", "bob@build-01", false},
		{"USERNAME fallback", "", " ", `CORP\bob`, "bob@build-01", true},
		{"missing username", "", "", "", "build-01", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USER", tc.envUser)
			t.Setenv("USERNAME", tc.envUsername)
			lookup := func() (*user.User, error) {
				if tc.lookupErr {
					return nil, errors.New("user lookup unavailable")
				}
				return &user.User{Username: tc.account}, nil
			}
			if got := defaultDeviceName("build-01", lookup); got != tc.want {
				t.Fatalf("device name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadConfig_DeviceNamePrecedenceKeepsIdentity(t *testing.T) {
	stageFakeAgent(t)
	redirectTestHome(t, t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	t.Setenv("USER", "fallback-user")
	t.Setenv("USERNAME", "")
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	base := Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir(), Profile: "device-test"}
	var first Config
	for _, tc := range []struct{ name, env, override, want string }{
		{"default", "", "", defaultDeviceName(host, user.Current)},
		{"environment", "Shared build server", "", "Shared build server"},
		{"override", "Shared build server", "Explicit device", "Explicit device"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_DAEMON_DEVICE_NAME", tc.env)
			overrides := base
			overrides.DeviceName = tc.override
			cfg, err := LoadConfig(overrides)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DeviceName != tc.want {
				t.Fatalf("device name = %q, want %q", cfg.DeviceName, tc.want)
			}
			if first.DaemonID == "" {
				first = cfg
			}
			if cfg.DaemonID != first.DaemonID || !reflect.DeepEqual(cfg.LegacyDaemonIDs, first.LegacyDaemonIDs) {
				t.Fatal("changing the display name changed daemon identity")
			}
			wantLegacy := filterLegacyIDs(LegacyDaemonIDs(host, base.Profile), cfg.DaemonID)
			if !reflect.DeepEqual(cfg.LegacyDaemonIDs, wantLegacy) {
				t.Fatalf("legacy IDs = %v, want hostname-based %v", cfg.LegacyDaemonIDs, wantLegacy)
			}
		})
	}
}
