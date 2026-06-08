package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureDaemonID_Persists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first, err := EnsureDaemonID("", "")
	if err != nil {
		t.Fatalf("EnsureDaemonID first call: %v", err)
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("EnsureDaemonID returned non-UUID: %q", first)
	}

	path := filepath.Join(home, ".multica", "daemon.id")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("daemon.id not written: %v", err)
	}
	if strings.TrimSpace(string(data)) != first {
		t.Fatalf("file contents %q differ from returned UUID %q", data, first)
	}

	second, err := EnsureDaemonID("", "")
	if err != nil {
		t.Fatalf("EnsureDaemonID second call: %v", err)
	}
	if second != first {
		t.Fatalf("UUID changed on second call: %q → %q", first, second)
	}
}

func TestEnsureDaemonID_IsolatedByProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaultID, err := EnsureDaemonID("", "")
	if err != nil {
		t.Fatalf("default profile: %v", err)
	}
	stagingID, err := EnsureDaemonID("staging", "")
	if err != nil {
		t.Fatalf("staging profile: %v", err)
	}
	if defaultID == stagingID {
		t.Fatalf("different profiles should get distinct daemon ids, both were %s", defaultID)
	}

	// Default profile stores at ~/.multica/daemon.id
	defaultFile := filepath.Join(home, ".multica", "daemon.id")
	if data, err := os.ReadFile(defaultFile); err != nil {
		t.Fatalf("default daemon.id not written: %v", err)
	} else if strings.TrimSpace(string(data)) != defaultID {
		t.Fatalf("default file contents %q differ from returned UUID %q", data, defaultID)
	}

	// Named profile stores at ~/.multica/profiles/staging/daemon.id
	profileFile := filepath.Join(home, ".multica", "profiles", "staging", "daemon.id")
	if data, err := os.ReadFile(profileFile); err != nil {
		t.Fatalf("profile daemon.id not written: %v", err)
	} else if strings.TrimSpace(string(data)) != stagingID {
		t.Fatalf("profile file contents %q differ from returned UUID %q", data, stagingID)
	}
}

func TestEnsureDaemonID_IsolatedByExplicitConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configA := filepath.Join(home, "instances", "dev", "config.json")
	configB := filepath.Join(home, "instances", "local", "config.json")

	idA, err := EnsureDaemonID("", configA)
	if err != nil {
		t.Fatalf("config A: %v", err)
	}
	idB, err := EnsureDaemonID("", configB)
	if err != nil {
		t.Fatalf("config B: %v", err)
	}
	if idA == idB {
		t.Fatalf("explicit config paths should get distinct daemon ids, both were %s", idA)
	}

	pathA := filepath.Join(filepath.Dir(configA), "daemon.id")
	pathB := filepath.Join(filepath.Dir(configB), "daemon.id")
	if _, err := os.Stat(pathA); err != nil {
		t.Fatalf("daemon.id for config A not written: %v", err)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Fatalf("daemon.id for config B not written: %v", err)
	}
}

func TestEnsureDaemonID_ReusesExistingProfileFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a per-profile daemon.id (either pre-existing or from earlier run).
	legacyID := uuid.Must(uuid.NewV7()).String()
	profileDir := filepath.Join(home, ".multica", "profiles", "staging")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "daemon.id"), []byte(legacyID+"\n"), 0o600); err != nil {
		t.Fatalf("seed legacy id: %v", err)
	}

	// EnsureDaemonID with matching profile should find and reuse the existing file.
	got, err := EnsureDaemonID("staging", "")
	if err != nil {
		t.Fatalf("EnsureDaemonID: %v", err)
	}
	if got != legacyID {
		t.Fatalf("expected reused UUID %s, got %s", legacyID, got)
	}
}

func TestEnsureDaemonID_RegeneratesCorruptFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".multica")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "daemon.id")
	if err := os.WriteFile(path, []byte("not-a-uuid"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	id, err := EnsureDaemonID("", "")
	if err != nil {
		t.Fatalf("EnsureDaemonID: %v", err)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("expected valid UUID, got %q", id)
	}

	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != id {
		t.Fatalf("file not rewritten with new UUID")
	}
}

func TestLegacyDaemonUUIDs_AlwaysReturnsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Set up multiple profile daemon.id files (active identities in the new design).
	uuidA := uuid.Must(uuid.NewV7()).String()
	uuidB := uuid.Must(uuid.NewV7()).String()
	for name, id := range map[string]string{"prod": uuidA, "desktop-multica": uuidB} {
		dir := filepath.Join(home, ".multica", "profiles", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "daemon.id"), []byte(id+"\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// LegacyDaemonUUIDs must NOT return these active profile IDs.
	got, err := LegacyDaemonUUIDs()
	if err != nil {
		t.Fatalf("LegacyDaemonUUIDs: %v", err)
	}
	if got != nil {
		t.Fatalf("LegacyDaemonUUIDs should return nil (deprecated), got %v", got)
	}
}

func TestLegacyDaemonUUIDs_MissingProfilesDirIsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ids, err := LegacyDaemonUUIDs()
	if err != nil {
		t.Fatalf("LegacyDaemonUUIDs: %v", err)
	}
	if ids != nil {
		t.Fatalf("expected nil on missing profiles dir, got %v", ids)
	}
}

func TestLegacyDaemonUUIDs_DoesNotCausesCrossProfileMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Simulate two active profiles with their own daemon.id
	profileA := "default"
	profileB := "member"
	idA, err := EnsureDaemonID("", "")
	if err != nil {
		t.Fatalf("EnsureDaemonID default: %v", err)
	}
	idB, err := EnsureDaemonID(profileB, "")
	if err != nil {
		t.Fatalf("EnsureDaemonID member: %v", err)
	}

	// LegacyDaemonUUIDs must not return either active daemon ID
	legacyUUIDs, err := LegacyDaemonUUIDs()
	if err != nil {
		t.Fatalf("LegacyDaemonUUIDs: %v", err)
	}

	for _, legacyID := range legacyUUIDs {
		if legacyID == idA {
			t.Fatalf("LegacyDaemonUUIDs returned active %s profile daemon_id %s", profileA, idA)
		}
		if legacyID == idB {
			t.Fatalf("LegacyDaemonUUIDs returned active %s profile daemon_id %s", profileB, idB)
		}
	}
}

func TestLegacyDaemonIDs(t *testing.T) {
	cases := []struct {
		name     string
		hostname string
		profile  string
		want     []string
	}{
		{
			name:     "plain hostname, no profile",
			hostname: "MacBook-Pro",
			want:     []string{"MacBook-Pro", "MacBook-Pro.local"},
		},
		{
			name:     "dot-local hostname, no profile",
			hostname: "MacBook-Pro.local",
			want:     []string{"MacBook-Pro", "MacBook-Pro.local"},
		},
		{
			name:     "plain hostname with profile",
			hostname: "MacBook-Pro",
			profile:  "staging",
			want: []string{
				"MacBook-Pro",
				"MacBook-Pro.local",
				"MacBook-Pro-staging",
				"MacBook-Pro.local-staging",
			},
		},
		{
			name:     "dot-local hostname with profile",
			hostname: "MacBook-Pro.local",
			profile:  "staging",
			want: []string{
				"MacBook-Pro",
				"MacBook-Pro.local",
				"MacBook-Pro-staging",
				"MacBook-Pro.local-staging",
			},
		},
		{
			name:     "empty hostname",
			hostname: "",
			want:     nil,
		},
		{
			name:     "mixed case hostname preserved as-is",
			hostname: "Jiayuans-MacBook-Pro.local",
			want: []string{
				"Jiayuans-MacBook-Pro",
				"Jiayuans-MacBook-Pro.local",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LegacyDaemonIDs(tc.hostname, tc.profile)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("LegacyDaemonIDs(%q, %q) = %v, want %v", tc.hostname, tc.profile, got, tc.want)
			}
		})
	}
}
