package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

var darwinTaskMachServices = []string{
	"com.apple.SystemConfiguration.configd",
	"com.apple.cfprefsd.agent",
	"com.apple.system.opendirectoryd.libinfo",
	"com.apple.trustd.agent",
}

type darwinIsolation struct {
	helper string
}

func newDarwinIsolation(helper string) platformIsolation {
	return &darwinIsolation{helper: helper}
}

func (d *darwinIsolation) WrapBound(policy *boundIsolationPolicy, executable, cwd pathIdentity, args []string, leadingExtraFiles int) (string, []string, []*os.File, error) {
	if err := validateIsolationHelper(d.helper); err != nil {
		return "", nil, nil, err
	}
	if policy == nil {
		return "", nil, nil, fmt.Errorf("bound isolation policy is required")
	}
	if leadingExtraFiles < 0 {
		return "", nil, nil, fmt.Errorf("leading extra files count must not be negative")
	}
	// Seatbelt can only authorize by pathname. Retain and recheck descriptor
	// identities so a replaced root/cwd/executable fails closed before launch.
	if err := recheckBoundIsolation(policy); err != nil {
		return "", nil, nil, err
	}
	if err := recheckPathIdentity(&executable); err != nil {
		return "", nil, nil, err
	}
	if err := recheckPathIdentity(&cwd); err != nil {
		return "", nil, nil, err
	}
	profile, err := renderDarwinProfile(policy.policy())
	if err != nil {
		return "", nil, nil, err
	}
	wrapped := []string{"-p", profile, executable.Path}
	wrapped = append(wrapped, args...)
	// Darwin has no object-bound chdir. Use the revalidated pathname and rely
	// on PreparedCommand's launch-time recheck immediately before Start.
	return d.helper, wrapped, nil, nil
}

func renderDarwinProfile(policy TaskIsolationPolicy) (string, error) {
	validated, err := policy.Validated()
	if err != nil {
		return "", err
	}
	var lines []string
	lines = append(lines,
		"(version 1)",
		"(deny default)",
		"(allow process-exec process-fork process-info*)",
		"(allow signal (target self))",
		"(allow sysctl-read)",
	)
	for _, service := range darwinTaskMachServices {
		lines = append(lines, fmt.Sprintf("(allow mach-lookup (global-name \"%s\"))", escapeSandboxString(service)))
	}
	for _, root := range append(append([]string(nil), validated.ReadOnlyRoots...), validated.SystemRoots...) {
		lines = append(lines, sandboxPathRule("file-read*", root))
	}
	for _, root := range validated.WritableRoots {
		lines = append(lines, sandboxPathRule("file-read* file-write*", root))
	}
	if validated.Network == NetworkAccessPublicAndLoopback {
		lines = append(lines, "(allow network-outbound)", "(allow network-inbound (local ip))")
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func sandboxPathRule(operations, path string) string {
	return fmt.Sprintf("(allow %s (subpath \"%s\"))", operations, escapeSandboxString(path))
}

func escapeSandboxString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, "\r", `\r`)
	return value
}

func validateIsolationHelper(path string) error {
	clean, err := validateAbsolutePath("isolation helper", path)
	if err != nil {
		return err
	}

	type pathIdentity struct {
		path  string
		info  os.FileInfo
		owner uint64
	}
	chain := trustedPathChain(clean)
	identities := make([]pathIdentity, 0, len(chain))
	for i, component := range chain {
		info, err := os.Lstat(component)
		if err != nil {
			return fmt.Errorf("lstat isolation helper path %q: %w", component, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("isolation helper path %q must not be a symlink", component)
		}
		if i == len(chain)-1 {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				return fmt.Errorf("isolation helper %q is not an executable regular file", clean)
			}
		} else if !info.IsDir() {
			return fmt.Errorf("isolation helper parent %q is not a directory", component)
		}
		identities = append(identities, pathIdentity{path: component, info: info})
	}

	for i := len(identities) - 1; i >= 0; i-- {
		identity := &identities[i]
		owner, err := fileOwnerUID(identity.info)
		if err != nil {
			return fmt.Errorf("verify isolation helper path %q ownership: %w", identity.path, err)
		}
		identity.owner = owner
		if identity.owner != 0 {
			return fmt.Errorf("isolation helper path %q is owned by uid %d, want root", identity.path, identity.owner)
		}
		if identity.info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("isolation helper path %q is writable by group or other", identity.path)
		}
	}

	// Re-read the complete path after validation so a concurrently replaced
	// component fails closed instead of being used under a stale trust decision.
	for _, identity := range identities {
		info, err := os.Lstat(identity.path)
		if err != nil {
			return fmt.Errorf("recheck isolation helper path %q: %w", identity.path, err)
		}
		owner, err := fileOwnerUID(info)
		if err != nil {
			return fmt.Errorf("recheck isolation helper path %q ownership: %w", identity.path, err)
		}
		if !os.SameFile(identity.info, info) || info.Mode() != identity.info.Mode() || owner != identity.owner {
			return fmt.Errorf("isolation helper path %q changed during validation", identity.path)
		}
	}
	return nil
}

func trustedPathChain(path string) []string {
	var reversed []string
	for current := path; ; current = filepath.Dir(current) {
		reversed = append(reversed, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	chain := make([]string, len(reversed))
	for i := range reversed {
		chain[len(reversed)-1-i] = reversed[i]
	}
	return chain
}

// fileOwnerUID uses reflection so the package still compiles on platforms
// whose syscall.Stat_t has no Unix uid field. Isolation helpers are supported
// only where the host exposes a stable numeric owner identity.
func fileOwnerUID(info os.FileInfo) (uint64, error) {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return 0, fmt.Errorf("file identity is unavailable")
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, fmt.Errorf("file identity is unavailable")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, fmt.Errorf("file identity has unsupported type %T", info.Sys())
	}
	uid := value.FieldByName("Uid")
	if !uid.IsValid() {
		return 0, fmt.Errorf("file identity does not expose an owner uid")
	}
	switch uid.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return uid.Uint(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		owner := uid.Int()
		if owner < 0 {
			return 0, fmt.Errorf("file identity exposes invalid owner uid %d", owner)
		}
		return uint64(owner), nil
	default:
		return 0, fmt.Errorf("file identity owner uid has unsupported type %s", uid.Kind())
	}
}

func sortedDarwinRoots(values ...[]string) []string {
	var roots []string
	for _, value := range values {
		roots = append(roots, value...)
	}
	sort.Strings(roots)
	return roots
}
