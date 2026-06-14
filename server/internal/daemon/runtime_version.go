package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// probeManifestCLIVersionTimeout caps how long the startup probe waits for a
// runtime CLI's `--version` to print. Some CLIs hang on first launch (e.g.
// they ask for credentials interactively) and we don't want a misbehaving
// extension to block daemon startup. Stays small on purpose.
const probeManifestCLIVersionTimeout = 3 * time.Second

// probeManifestCLIVersion runs `<execPath> --version` and returns the first
// version-shaped token in the output (matching `vMAJOR.MINOR.PATCH`). The
// result is best-effort — failures, empty output, and unparsable strings all
// return ("", nil) so the manifest still loads with no warning instead of
// hard-failing on a CLI that doesn't implement --version. Returns an error
// only for genuinely surprising failures (binary missing) so the caller can
// log them at warning level.
func probeManifestCLIVersion(execPath string) (string, error) {
	if execPath == "" {
		return "", nil
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return "", fmt.Errorf("lookup %q: %w", execPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeManifestCLIVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, execPath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil
	}
	return extractSemverToken(string(out)), nil
}

var manifestVersionRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// extractSemverToken returns the first MAJOR.MINOR.PATCH token in s, or "" if
// none. The leading `v` is stripped so callers can compare results against
// bare-semver thresholds. Useful when a CLI prints something like
// "my-cli 1.4.2 (Company Internal)" — we want "1.4.2", not the rest.
func extractSemverToken(s string) string {
	m := manifestVersionRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2] + "." + m[3]
}

// compareSemver reports whether `detected` meets the `minimum` requirement.
// Returns nil when detected ≥ minimum, an error describing the gap otherwise.
// Both arguments tolerate a leading `v` and version suffixes; everything past
// the third number is ignored. An unparsable detected string returns nil
// (defensive — if we can't parse what the CLI printed we'd rather not block
// on a phantom mismatch).
func compareSemver(detected, minimum string) error {
	d, ok := parseManifestSemver(detected)
	if !ok {
		return nil
	}
	m, ok := parseManifestSemver(minimum)
	if !ok {
		return nil
	}
	if !d.lt(m) {
		return nil
	}
	return fmt.Errorf("installed CLI %s is below required minimum %s — upgrade is recommended", detected, minimum)
}

type manifestSemver struct{ major, minor, patch int }

func (a manifestSemver) lt(b manifestSemver) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

func parseManifestSemver(s string) (manifestSemver, bool) {
	tok := extractSemverToken(strings.TrimSpace(s))
	if tok == "" {
		return manifestSemver{}, false
	}
	parts := strings.SplitN(tok, ".", 3)
	if len(parts) != 3 {
		return manifestSemver{}, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return manifestSemver{}, false
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return manifestSemver{}, false
	}
	pat, err := strconv.Atoi(parts[2])
	if err != nil {
		return manifestSemver{}, false
	}
	return manifestSemver{maj, min, pat}, true
}
