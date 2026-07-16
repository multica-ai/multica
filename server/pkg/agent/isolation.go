package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// NetworkAccess is the network namespace exposed to an agent task.
type NetworkAccess uint8

const (
	NetworkAccessNone NetworkAccess = iota
	NetworkAccessPublicAndLoopback
)

// TaskIsolationPolicy is the complete filesystem and network authority for one
// task. Roots are resolved to stable, existing paths before a process is built.
type TaskIsolationPolicy struct {
	WritableRoots  []string
	ReadOnlyRoots  []string
	ReadOnlyFiles  []ReadOnlyFileMount
	SystemRoots    []string
	ForbiddenRoots []string
	Network        NetworkAccess
}

type ReadOnlyFileMount struct {
	Source string
	Target string
}

type platformIsolation interface {
	// leadingExtraFiles is the number of caller-owned ExtraFiles that will
	// occupy child FDs starting at 3 before isolation-owned descriptors.
	WrapBound(*boundIsolationPolicy, pathIdentity, pathIdentity, []string, int) (string, []string, []*os.File, error)
}

func newPlatformIsolation() platformIsolation {
	switch runtime.GOOS {
	case "darwin":
		return newDarwinIsolation("/usr/bin/sandbox-exec")
	case "linux":
		return newLinuxIsolation("/usr/bin/bwrap")
	default:
		return newUnsupportedIsolation(runtime.GOOS)
	}
}

// Validated returns a canonical copy whose roots exist and contain no symlink
// aliases. Any overlap with a forbidden root rejects the entire policy.
func (p TaskIsolationPolicy) Validated() (TaskIsolationPolicy, error) {
	if len(p.WritableRoots) == 0 {
		return TaskIsolationPolicy{}, fmt.Errorf("at least one writable root is required")
	}
	if p.Network != NetworkAccessNone && p.Network != NetworkAccessPublicAndLoopback {
		return TaskIsolationPolicy{}, fmt.Errorf("unsupported network access %d", p.Network)
	}

	var err error
	if p.WritableRoots, err = validateRoots("writable", p.WritableRoots); err != nil {
		return TaskIsolationPolicy{}, err
	}
	if p.ReadOnlyRoots, err = validateRoots("read-only", p.ReadOnlyRoots); err != nil {
		return TaskIsolationPolicy{}, err
	}
	if p.SystemRoots, err = validateRoots("system", p.SystemRoots); err != nil {
		return TaskIsolationPolicy{}, err
	}
	if p.ForbiddenRoots, err = validateRoots("forbidden", p.ForbiddenRoots); err != nil {
		return TaskIsolationPolicy{}, err
	}
	allowedRoots := append(append(append([]string(nil), p.WritableRoots...), p.ReadOnlyRoots...), p.SystemRoots...)
	if p.ReadOnlyFiles, err = validateReadOnlyFiles(p.ReadOnlyFiles, p.WritableRoots, p.ForbiddenRoots); err != nil {
		return TaskIsolationPolicy{}, err
	}

	for _, root := range allowedRoots {
		for _, forbidden := range p.ForbiddenRoots {
			if pathsOverlap(root, forbidden) {
				return TaskIsolationPolicy{}, fmt.Errorf("allowed root %q overlaps forbidden root %q", root, forbidden)
			}
		}
	}
	return p, nil
}

func validateReadOnlyFiles(files []ReadOnlyFileMount, writableRoots, forbiddenRoots []string) ([]ReadOnlyFileMount, error) {
	seenTargets := make(map[string]struct{}, len(files))
	validated := make([]ReadOnlyFileMount, 0, len(files))
	for _, mount := range files {
		source, err := validateAbsolutePath("read-only file source", mount.Source)
		if err != nil {
			return nil, err
		}
		target, err := validateAbsolutePath("read-only file target", mount.Target)
		if err != nil {
			return nil, err
		}
		if pathsOverlap(target, "/dev") || pathsOverlap(target, "/proc") {
			return nil, fmt.Errorf("read-only file target %q overlaps an isolated pseudo-filesystem", target)
		}
		if _, exists := seenTargets[target]; exists {
			return nil, fmt.Errorf("duplicate read-only file target %q", target)
		}
		seenTargets[target] = struct{}{}
		for _, writable := range writableRoots {
			if pathWithin(source, writable) {
				return nil, fmt.Errorf("read-only file source %q is inside writable root %q", source, writable)
			}
			if pathsOverlap(target, writable) {
				return nil, fmt.Errorf("read-only file target %q overlaps writable root %q", target, writable)
			}
		}
		for _, forbidden := range forbiddenRoots {
			if pathWithin(source, forbidden) {
				return nil, fmt.Errorf("read-only file source %q is inside forbidden root %q", source, forbidden)
			}
		}
		identity, err := openFileIdentity("read-only file source", source)
		if err != nil {
			return nil, err
		}
		_ = identity.Close()
		validated = append(validated, ReadOnlyFileMount{Source: source, Target: target})
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].Target < validated[j].Target })
	return validated, nil
}

func validateRoots(kind string, roots []string) ([]string, error) {
	seen := make(map[string]struct{}, len(roots))
	validated := make([]string, 0, len(roots))
	for _, root := range roots {
		clean, err := validateAbsolutePath(kind+" root", root)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return nil, fmt.Errorf("resolve %s root %q: %w", kind, clean, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat %s root %q: %w", kind, resolved, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s root %q is not a directory", kind, resolved)
		}
		stableAlias := isStableSystemPathAlias(clean, resolved)
		if resolved != clean && !stableAlias {
			return nil, fmt.Errorf("%s root %q resolves through symlink to %q", kind, clean, resolved)
		}
		if !stableAlias || kind != "system" || runtime.GOOS != "linux" {
			resolved = clean
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		validated = append(validated, resolved)
	}
	sort.Strings(validated)
	return validated, nil
}

func isStableSystemPathAlias(path, resolved string) bool {
	return isStableSystemPathAliasForOS(runtime.GOOS, path, resolved)
}

func resolveStableSystemPathAlias(kind, path string) (string, error) {
	clean, err := validateAbsolutePath(kind, path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "linux" {
		return clean, nil
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve %s %q: %w", kind, clean, err)
	}
	if resolved == clean {
		return clean, nil
	}
	if isStableSystemPathAlias(clean, resolved) {
		return resolved, nil
	}
	if isResolvedStableSystemAliasForOS(runtime.GOOS, clean, resolved) {
		return resolved, nil
	}
	return "", fmt.Errorf("%s %q resolves through symlink to %q", kind, clean, resolved)
}

func isResolvedStableSystemAliasForOS(goos, path, resolved string) bool {
	canonical, ok := stableSystemAliasPathForOS(goos, path)
	return ok && pathWithin(resolved, canonicalRoot(canonical))
}

func stableSystemAliasPathForOS(goos, path string) (string, bool) {
	for alias, canonical := range stableSystemAliasesForOS(goos) {
		if path == alias {
			return canonical, true
		}
		if strings.HasPrefix(path, alias+"/") {
			return canonical + strings.TrimPrefix(path, alias), true
		}
	}
	return "", false
}

func canonicalRoot(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 2 {
		return path
	}
	return "/" + filepath.Join(parts[0], parts[1])
}

func stableSystemAliasesForOS(goos string) map[string]string {
	switch goos {
	case "darwin":
		return map[string]string{
			"/etc": "/private/etc",
			"/tmp": "/private/tmp",
			"/var": "/private/var",
		}
	case "linux":
		return map[string]string{
			"/bin":   "/usr/bin",
			"/lib":   "/usr/lib",
			"/lib64": "/usr/lib64",
		}
	default:
		return nil
	}
}

func isStableSystemPathAliasForOS(goos, path, resolved string) bool {
	for alias, canonical := range stableSystemAliasesForOS(goos) {
		if path == alias && resolved == canonical {
			return true
		}
		if strings.HasPrefix(path, alias+"/") && resolved == canonical+strings.TrimPrefix(path, alias) {
			return true
		}
	}
	return false
}

func validateAbsolutePath(kind, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s %q must be absolute", kind, path)
	}
	clean := filepath.Clean(path)
	if clean != path || containsParentTraversal(path) {
		return "", fmt.Errorf("%s %q must be canonical and contain no parent traversal", kind, path)
	}
	return clean, nil
}

func containsParentTraversal(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func pathWithinAny(path string, roots []string) bool {
	for _, root := range roots {
		if pathWithin(path, root) {
			return true
		}
	}
	return false
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}

func validateProtectedTargetsAgainstCommandPaths(files []boundReadOnlyFileMount, executable, cwd pathIdentity) error {
	for _, file := range files {
		for _, commandPath := range []struct {
			kind string
			path string
		}{
			{kind: "cwd", path: cwd.Path},
			{kind: "executable", path: executable.Path},
		} {
			if pathsOverlap(file.Target, commandPath.path) {
				return fmt.Errorf("read-only file target %q overlaps command %s %q", file.Target, commandPath.kind, commandPath.path)
			}
		}
	}
	return nil
}

type unsupportedIsolation struct {
	goos string
}

func newUnsupportedIsolation(goos string) platformIsolation {
	return &unsupportedIsolation{goos: goos}
}

func (u *unsupportedIsolation) WrapBound(*boundIsolationPolicy, pathIdentity, pathIdentity, []string, int) (string, []string, []*os.File, error) {
	return "", nil, nil, fmt.Errorf("task process isolation is unsupported on %s", u.goos)
}

func isolationLaunchDirectory(platform platformIsolation) string {
	switch platform.(type) {
	case *linuxIsolation:
		return "/"
	default:
		// Darwin seatbelt authorizes by path and has no fd-bound chdir primitive.
		// Callers still re-validate cwd identity immediately before Start.
		return ""
	}
}
