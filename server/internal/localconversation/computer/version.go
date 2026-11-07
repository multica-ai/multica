package computer

import "strings"

// CapabilityVersion is the Computer capability contract this daemon speaks.
// It must equal packages/computer-use/contract/computer.json; the contract
// test enforces that and the version-bump rules.
const CapabilityVersion = "1.0.1"

// Compatible reports whether two semver versions interoperate: major and
// minor must match, and a patch difference is compatible.
func Compatible(a, b string) bool {
	x, okA := majorMinor(a)
	y, okB := majorMinor(b)
	return okA && okB && x == y
}

func majorMinor(version string) (string, bool) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return "", false
		}
	}
	return parts[0] + "." + parts[1], true
}
