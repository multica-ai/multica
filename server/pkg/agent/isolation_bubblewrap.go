package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type linuxIsolation struct {
	helper string
}

func newLinuxIsolation(helper string) platformIsolation {
	return &linuxIsolation{helper: helper}
}

func (l *linuxIsolation) WrapBound(policy *boundIsolationPolicy, executable, cwd pathIdentity, args []string, leadingExtraFiles int) (string, []string, []*os.File, error) {
	if err := validateIsolationHelper(l.helper); err != nil {
		return "", nil, nil, err
	}
	if policy == nil {
		return "", nil, nil, fmt.Errorf("bound isolation policy is required")
	}
	if err := recheckBoundIsolation(policy); err != nil {
		return "", nil, nil, err
	}
	if err := recheckPathIdentity(&executable); err != nil {
		return "", nil, nil, err
	}
	if err := recheckPathIdentity(&cwd); err != nil {
		return "", nil, nil, err
	}
	if err := rejectLinuxHostPseudoFilesystemBindings(policy.policy()); err != nil {
		return "", nil, nil, err
	}

	wrapped, extraFiles, err := renderLinuxBubblewrapArgsBound(policy, executable, cwd, args, leadingExtraFiles)
	if err != nil {
		return "", nil, nil, err
	}
	return l.helper, wrapped, extraFiles, nil
}

func renderLinuxBubblewrapArgs(policy TaskIsolationPolicy, executable string, commandArgs []string) ([]string, error) {
	// Compatibility helper for pure path-based unit tests. Production launch
	// uses renderLinuxBubblewrapArgsBound with retained directory descriptors.
	validated, err := policy.Validated()
	if err != nil {
		return nil, err
	}
	if _, err := validateAbsolutePath("executable", executable); err != nil {
		return nil, err
	}
	if err := rejectLinuxHostPseudoFilesystemBindings(validated); err != nil {
		return nil, err
	}
	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
	}
	if validated.Network == NetworkAccessPublicAndLoopback {
		args = append(args, "--share-net")
	}
	args = append(args,
		"--clearenv",
		"--dev", "/dev",
		"--proc", "/proc",
	)
	roots := append(append([]string(nil), validated.ReadOnlyRoots...), validated.SystemRoots...)
	sort.Strings(roots)
	created := make(map[string]struct{})
	for _, root := range append(append([]string(nil), roots...), validated.WritableRoots...) {
		for _, parent := range missingNamespaceParents(root) {
			if _, ok := created[parent]; ok {
				continue
			}
			created[parent] = struct{}{}
			args = append(args, "--dir", parent)
		}
	}
	for _, root := range roots {
		args = append(args, "--ro-bind", root, root)
	}
	for _, root := range validated.WritableRoots {
		args = append(args, "--bind", root, root)
	}
	args = append(args, "--", executable)
	args = append(args, commandArgs...)
	return args, nil
}

func renderLinuxBubblewrapArgsBound(policy *boundIsolationPolicy, executable, cwd pathIdentity, commandArgs []string, leadingExtraFiles int) ([]string, []*os.File, error) {
	if policy == nil {
		return nil, nil, fmt.Errorf("bound isolation policy is required")
	}
	if executable.File == nil || cwd.File == nil {
		return nil, nil, fmt.Errorf("linux isolation requires open executable and cwd descriptors")
	}

	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
	}
	if policy.Network == NetworkAccessPublicAndLoopback {
		args = append(args, "--share-net")
	}
	args = append(args,
		"--clearenv",
		"--dev", "/dev",
		"--proc", "/proc",
	)

	type mount struct {
		identity pathIdentity
		writable bool
	}
	var mounts []mount
	for _, root := range policy.ReadOnlyRoots {
		mounts = append(mounts, mount{identity: root, writable: false})
	}
	for _, root := range policy.SystemRoots {
		mounts = append(mounts, mount{identity: root, writable: false})
	}
	for _, root := range policy.WritableRoots {
		mounts = append(mounts, mount{identity: root, writable: true})
	}
	sort.SliceStable(mounts, func(i, j int) bool {
		if mounts[i].identity.Path == mounts[j].identity.Path {
			return !mounts[i].writable && mounts[j].writable
		}
		return mounts[i].identity.Path < mounts[j].identity.Path
	})

	created := make(map[string]struct{})
	for _, m := range mounts {
		for _, parent := range missingNamespaceParents(m.identity.Path) {
			if _, ok := created[parent]; ok {
				continue
			}
			created[parent] = struct{}{}
			args = append(args, "--dir", parent)
		}
	}

	if leadingExtraFiles < 0 {
		return nil, nil, fmt.Errorf("leading extra files count must not be negative")
	}
	extraFiles := make([]*os.File, 0, len(mounts)+2)
	// exec.Cmd.ExtraFiles starts at child FD 3. Callers may reserve leading
	// descriptors (for example Pi session FD 3) before isolation mounts.
	childFD := 3 + leadingExtraFiles
	for _, m := range mounts {
		if m.identity.File == nil {
			return nil, nil, fmt.Errorf("linux isolation root %q is missing a descriptor", m.identity.Path)
		}
		if m.writable {
			args = append(args, "--bind-fd", fmt.Sprintf("%d", childFD), m.identity.Path)
		} else {
			args = append(args, "--ro-bind-fd", fmt.Sprintf("%d", childFD), m.identity.Path)
		}
		extraFiles = append(extraFiles, m.identity.File)
		childFD++
	}

	// Overmount cwd and executable at their original namespace paths from the
	// retained descriptors. This preserves expected absolute paths while the
	// final chdir and exec resolve to the validated objects, not host pathnames.
	args = append(args, "--bind-fd", fmt.Sprintf("%d", childFD), cwd.Path)
	extraFiles = append(extraFiles, cwd.File)
	childFD++
	args = append(args, "--ro-bind-fd", fmt.Sprintf("%d", childFD), executable.Path)
	extraFiles = append(extraFiles, executable.File)
	args = append(args, "--chdir", cwd.Path)
	args = append(args, "--", executable.Path)
	args = append(args, commandArgs...)
	return args, extraFiles, nil
}

func rejectLinuxHostPseudoFilesystemBindings(policy TaskIsolationPolicy) error {
	roots := append(append(append([]string(nil), policy.WritableRoots...), policy.ReadOnlyRoots...), policy.SystemRoots...)
	for _, root := range roots {
		for _, isolated := range []string{"/dev", "/proc"} {
			if pathsOverlap(root, isolated) {
				return fmt.Errorf("allowed root %q overlaps isolated Linux pseudo-filesystem %q", root, isolated)
			}
		}
	}
	return nil
}

func missingNamespaceParents(path string) []string {
	var reversed []string
	for parent := filepath.Dir(path); parent != "/" && parent != "."; parent = filepath.Dir(parent) {
		reversed = append(reversed, parent)
	}
	parents := make([]string, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		parents = append(parents, reversed[i])
	}
	return parents
}

func (l *linuxIsolation) String() string {
	return fmt.Sprintf("bubblewrap(%s)", l.helper)
}
