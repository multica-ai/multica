package daemon

import "testing"

func TestSetCodexWindowsSandboxRegistrationMetadataWithConfig(t *testing.T) {
	entry := map[string]string{}
	setCodexWindowsSandboxRegistrationMetadataWithConfig(
		entry,
		true,
		[]string{"--profile", "research"},
	)

	if entry[codexWindowsSandboxArgConfiguredKey] != "false" {
		t.Fatalf("argument ownership = %q, want false", entry[codexWindowsSandboxArgConfiguredKey])
	}
	if entry[codexWindowsSandboxConfigConfiguredKey] != "true" {
		t.Fatalf("config ownership = %q, want true", entry[codexWindowsSandboxConfigConfiguredKey])
	}

	setCodexWindowsSandboxRegistrationMetadataWithConfig(
		entry,
		false,
		[]string{"-c", `windows.sandbox="elevated"`},
	)
	if entry[codexWindowsSandboxArgConfiguredKey] != "true" {
		t.Fatalf("argument ownership = %q, want true", entry[codexWindowsSandboxArgConfiguredKey])
	}
	if entry[codexWindowsSandboxConfigConfiguredKey] != "false" {
		t.Fatalf("config ownership = %q, want false", entry[codexWindowsSandboxConfigConfiguredKey])
	}
}
