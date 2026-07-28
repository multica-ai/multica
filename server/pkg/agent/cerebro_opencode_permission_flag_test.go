package agent

import (
	"log/slog"
	"os/exec"
	"strings"
	"testing"
)

// Help text as the two live OpenCode majors actually print it. Verified
// against both binaries on 2026-07-28 (FIR-3945): 1.14.31 advertises only
// --dangerously-skip-permissions and exits 1 with a usage dump on --auto;
// 1.18.8 advertises only --auto and silently ignores the old spelling.
const (
	opencodeHelp114 = `Options:
      --pure                          run without external plugins   [boolean]
      --format                        format: default or json         [string]
      --dangerously-skip-permissions  auto-approve permissions that are not explicitly denied
                                      (dangerous!)                   [boolean]
`
	opencodeHelp118 = `Options:
      --pure         run without external plugins                    [boolean]
      --format       format: default or json                          [string]
      --auto         auto-approve permissions that are not explicitly denied (dangerous!)
`
)

func TestSelectOpencodePermissionFlagMatchesInstalledMajor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		help string
		want string
	}{
		{"1.18 advertises --auto", opencodeHelp118, "--auto"},
		{"1.14 advertises the legacy spelling", opencodeHelp114, "--dangerously-skip-permissions"},
		// Probe failed (binary unreachable, timeout, garbage output). Emitting
		// no flag at all would leave the run blocked on the first permission
		// prompt, so we fall back to the current upstream spelling.
		{"probe returned nothing", "", "--auto"},
		{"unrelated output", "some unrelated banner", "--auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := selectOpencodePermissionFlag(tc.help, "opencode", slog.Default()); got != tc.want {
				t.Errorf("selectOpencodePermissionFlag(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// The regression this whole file exists for: whichever flag we pick must be
// one the installed binary really accepts. Hardcoding either spelling broke
// the half of the fleet on the other major — twice, in both directions.
func TestOpencodePermissionFlagIsAcceptedByInstalledCLI(t *testing.T) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed on this host; skipping real-binary probe")
	}

	flag := opencodePermissionFlag(path, slog.Default())
	if flag != "--auto" && flag != "--dangerously-skip-permissions" {
		t.Fatalf("probe returned an unknown flag %q", flag)
	}

	help := lookupOpencodeHelp(t)
	if !strings.Contains(help, flag) {
		t.Fatalf("probe chose %s but `opencode run --help` does not advertise it:\n%s", flag, help)
	}
	// The other spelling must NOT be emitted — on 1.14 it is a hard exit 1,
	// on 1.18 it is silently ignored and permissions never get auto-approved.
	other := "--auto"
	if flag == "--auto" {
		other = "--dangerously-skip-permissions"
	}
	if strings.Contains(help, other) {
		t.Logf("installed CLI advertises both spellings; %s chosen", flag)
	}
}
