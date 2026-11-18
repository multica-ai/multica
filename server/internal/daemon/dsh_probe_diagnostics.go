package daemon

import (
	"fmt"
	"regexp"
	"strings"
)

// DSH's admission preflight writes this format before plugin logging starts.
// Restrict it to the core packages implicated in profile-local version skew;
// an optional plugin's warning does not establish an unusable runtime.
// Source: deepseek-harness dsh-v0.2.2-rc.3, compatibility-preflight.ts and
// plugin-compatibility.ts in packages/boot/app-boot/src.
var dshCoreCompatibilityRefusal = regexp.MustCompile(`^[^\r\n]*: disabling profile plugin [^\r\n]+: Plugin (@deepseek-ai/dsh-(?:agent|session|tools|llm))@([0-9][0-9A-Za-z.+-]{0,79}) is incompatible with dsh ([0-9][0-9A-Za-z.+-]{0,79}): requirements `)

func dshCoreCompatibilitySummary(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if match := dshCoreCompatibilityRefusal.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			return dshDiagnosticOutput([]byte(fmt.Sprintf("%s@%s is incompatible with DSH %s", match[1], match[2], match[3])))
		}
	}
	return ""
}

func dshProbeDiagnostic(err error, stdout, stderr []byte) string {
	var parts []string
	if err != nil {
		parts = append(parts, err.Error())
	}
	if len(stdout) != 0 {
		parts = append(parts, "stdout: "+string(stdout))
	}
	if len(stderr) != 0 {
		parts = append(parts, "stderr: "+string(stderr))
	}
	if len(parts) == 0 {
		parts = append(parts, "probe exited without a protocol frame")
	}
	return dshDiagnosticOutput([]byte(strings.Join(parts, "\n")))
}
