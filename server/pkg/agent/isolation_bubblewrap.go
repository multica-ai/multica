package agent

import (
	"fmt"
	"path/filepath"
	"sort"
)

type linuxIsolation struct {
	helper string
}

func newLinuxIsolation(helper string) platformIsolation {
	return &linuxIsolation{helper: helper}
}

func (l *linuxIsolation) Wrap(policy TaskIsolationPolicy, executable string, args []string) (string, []string, error) {
	if err := validateIsolationHelper(l.helper); err != nil {
		return "", nil, err
	}
	wrapped, err := renderLinuxBubblewrapArgs(policy, executable, args)
	if err != nil {
		return "", nil, err
	}
	return l.helper, wrapped, nil
}

func renderLinuxBubblewrapArgs(policy TaskIsolationPolicy, executable string, commandArgs []string) ([]string, error) {
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
