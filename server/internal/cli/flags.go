package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// FlagOrEnv returns the flag value if set, otherwise the environment variable value,
// otherwise the fallback.
func FlagOrEnv(cmd *cobra.Command, flagName, envKey, fallback string) string {
	if cmd.Flags().Changed(flagName) {
		val, _ := cmd.Flags().GetString(flagName)
		return val
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	return fallback
}

// FlagOrEnvArray is the multi-value sibling of FlagOrEnv. The flag is
// expected to be registered as a StringArray (cobra.StringArrayVar or
// equivalent); this helper flattens every repetition into one slice in
// declaration order. The environment variable is parsed as one
// newline-separated spec — the same "Name: Value" per-line format the
// daemon's MULTICA_EXTRA_HEADERS / config extra_headers accept — so an
// operator can keep a long list of headers out of their shell history
// while the CLI flag still wins when both are set.
//
// Flag takes precedence over env: a `--extra-header "X-A: 1"` repeated
// twice always produces ["X-A: 1", "X-A: 1"] regardless of what the env
// holds. Empty trailing entries (whitespace-only lines, blank trailing
// newlines) are dropped so an accidental `MULTICA_EXTRA_HEADERS=$'\nX-A: 1\n'`
// still surfaces X-A: 1 rather than a leading blank entry. Returns nil
// when neither source has anything to say, matching the rest of the
// Config-ExtraHeaders stack's "nil == unset" contract.
func FlagOrEnvArray(cmd *cobra.Command, flagName, envKey string) []string {
	if cmd.Flags().Changed(flagName) {
		vals, _ := cmd.Flags().GetStringArray(flagName)
		out := make([]string, 0, len(vals))
		for _, v := range vals {
			if strings.TrimSpace(v) == "" {
				continue
			}
			out = append(out, v)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
