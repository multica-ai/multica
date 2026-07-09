package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureDaemonID_Persists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first, err := EnsureDaemonID("")
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

	second, err := EnsureDaemonID("")
	if err != nil {
		t.Fatalf("EnsureDaemonID second call: %v", err)
	}
	if second != first {
		t.Fatalf("UUID changed on second call: %q → %q", first, second)
	}
}

func TestEnsureDaemonID_SharedAcrossProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaultID, err := EnsureDaemonID("")
	if err != nil {
		t.Fatalf("default profile: %v", err)
	}
	stagingID, err := EnsureDaemonID("staging")
	if err != nil {
		t.Fatalf("staging profile: %v", err)
	}
	if defaultID != stagingID {
		t.Fatalf("profiles should share one machine id, got default=%s staging=%s", defaultID, stagingID)
	}

	// Profile-scoped file must not be created under the new layout — the
	// only source of truth is ~/.multica/daemon.id.
	profileFile := filepath.Join(home, ".multica", "profiles", "staging", "daemon.id")
	if _, err := os.Stat(profileFile); !os.IsNotExist(err) {
		t.Fatalf("profile-scoped daemon.id should not be created, stat err: %v", err)
	}
}

func TestEnsureDaemonID_PromotesPreChangeProfileFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a per-profile daemon.id the way pre-#1220 daemons laid it out.
	legacyID := uuid.Must(uuid.NewV7()).String()
	profileDir := filepath.Join(home, ".multica", "profiles", "staging")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "daemon.id"), []byte(legacyID+"\n"), 0o600); err != nil {
		t.Fatalf("seed legacy id: %v", err)
	}

	// First call on the post-change daemon with the matching profile must
	// reuse the pre-change UUID so existing runtime rows continue to match
	// without needing a merge round-trip.
	got, err := EnsureDaemonID("staging")
	if err != nil {
		t.Fatalf("EnsureDaemonID: %v", err)
	}
	if got != legacyID {
		t.Fatalf("expected promoted UUID %s, got %s", legacyID, got)
	}

	// The canonical file now holds that same UUID.
	data, err := os.ReadFile(filepath.Join(home, ".multica", "daemon.id"))
	if err != nil {
		t.Fatalf("read canonical file: %v", err)
	}
	if strings.TrimSpace(string(data)) != legacyID {
		t.Fatalf("canonical file %q != promoted %q", data, legacyID)
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

	id, err := EnsureDaemonID("")
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

func TestLegacyDaemonUUIDs_ScansProfileDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

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

	// A profile directory with a corrupt file must be skipped, not fail.
	corruptDir := filepath.Join(home, ".multica", "profiles", "corrupt")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("mkdir corrupt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "daemon.id"), []byte("not-a-uuid"), 0o600); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}

	got, err := LegacyDaemonUUIDs()
	if err != nil {
		t.Fatalf("LegacyDaemonUUIDs: %v", err)
	}
	sort.Strings(got)
	want := []string{uuidA, uuidB}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LegacyDaemonUUIDs = %v, want %v", got, want)
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
