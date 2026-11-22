package daemon

import (
	"sort"
	"strings"
)

const taskIdentityPinName = "reassert-task-env"

// taskIdentityEnv copies MULTICA_* keys from the assembled child environment.
// These are task identity, not user secrets-tool overlay, and must still be
// present on the process that actually runs the agent after a wrapper such as
// `doppler run --` rebuilds the grandchild env.
func taskIdentityEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string)
	for k, v := range env {
		if strings.HasPrefix(strings.ToUpper(k), "MULTICA_") {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// insertTaskIdentityPinAfterWrapper splices pinPath immediately after the
// first secrets-wrapper `--` in a custom-runtime launch prefix
// (`doppler run -- opencode`). Secrets wrappers treat everything after `--`
// as the command they exec, so the pin runs in that grandchild and re-exports
// task identity last.
//
// The separator is only recognized when a positional token precedes it
// (`run --`, `exec profile --`). A flag-only prefix that uses `--` merely to
// end options — including Go test helpers (`-test.run=^TestFoo$ -- helper`) —
// is returned unchanged, otherwise the pin would be taken as the helper's
// first argument and the real command would never start.
// Prefixes without `--` are also unchanged (built-in runtimes).
func insertTaskIdentityPinAfterWrapper(prefix []string, pinPath string) []string {
	if pinPath == "" {
		return prefix
	}
	for i, arg := range prefix {
		if arg != "--" {
			continue
		}
		if !prefixHasPositionalBefore(prefix, i) {
			return prefix
		}
		out := make([]string, 0, len(prefix)+1)
		out = append(out, prefix[:i+1]...)
		out = append(out, pinPath)
		out = append(out, prefix[i+1:]...)
		return out
	}
	return prefix
}

func prefixHasPositionalBefore(prefix []string, dashIndex int) bool {
	for _, arg := range prefix[:dashIndex] {
		if arg != "" && !strings.HasPrefix(arg, "-") {
			return true
		}
	}
	return false
}

func writeTaskIdentityPin(dir string, env map[string]string) (string, error) {
	vars := taskIdentityEnv(env)
	if dir == "" || len(vars) == 0 {
		return "", nil
	}
	return writeTaskIdentityPinScript(dir, vars)
}

func isSafeEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
			continue
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
