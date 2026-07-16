package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// pathIdentity is a validated filesystem object retained by descriptor where
// the host allows it. Path is retained only for policy rendering and error
// messages; authorization decisions recheck the open descriptor.
type pathIdentity struct {
	Path string
	File *os.File
	Info os.FileInfo
}

func (p *pathIdentity) Close() error {
	if p == nil || p.File == nil {
		return nil
	}
	err := p.File.Close()
	p.File = nil
	return err
}

type boundIsolationPolicy struct {
	WritableRoots  []pathIdentity
	ReadOnlyRoots  []pathIdentity
	SystemRoots    []pathIdentity
	ForbiddenRoots []pathIdentity
	Network        NetworkAccess
}

func (p *boundIsolationPolicy) Close() error {
	if p == nil {
		return nil
	}
	var first error
	for _, group := range [][]pathIdentity{
		p.WritableRoots,
		p.ReadOnlyRoots,
		p.SystemRoots,
		p.ForbiddenRoots,
	} {
		for i := range group {
			if err := group[i].Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (p boundIsolationPolicy) policy() TaskIsolationPolicy {
	return TaskIsolationPolicy{
		WritableRoots:  identityPaths(p.WritableRoots),
		ReadOnlyRoots:  identityPaths(p.ReadOnlyRoots),
		SystemRoots:    identityPaths(p.SystemRoots),
		ForbiddenRoots: identityPaths(p.ForbiddenRoots),
		Network:        p.Network,
	}
}

func identityPaths(values []pathIdentity) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.Path)
	}
	return out
}

func bindTaskIsolationPolicy(policy TaskIsolationPolicy) (*boundIsolationPolicy, error) {
	validated, err := policy.Validated()
	if err != nil {
		return nil, err
	}
	bound := &boundIsolationPolicy{Network: validated.Network}
	owned := true
	defer func() {
		if owned {
			_ = bound.Close()
		}
	}()

	if bound.WritableRoots, err = openDirectoryIdentities("writable", validated.WritableRoots); err != nil {
		return nil, err
	}
	if bound.ReadOnlyRoots, err = openDirectoryIdentities("read-only", validated.ReadOnlyRoots); err != nil {
		return nil, err
	}
	if bound.SystemRoots, err = openDirectoryIdentities("system", validated.SystemRoots); err != nil {
		return nil, err
	}
	if bound.ForbiddenRoots, err = openDirectoryIdentities("forbidden", validated.ForbiddenRoots); err != nil {
		return nil, err
	}
	if err := recheckBoundIsolation(bound); err != nil {
		return nil, err
	}
	owned = false
	return bound, nil
}

func openDirectoryIdentities(kind string, roots []string) ([]pathIdentity, error) {
	out := make([]pathIdentity, 0, len(roots))
	for _, root := range roots {
		identity, err := openDirectoryIdentity(kind+" root", root)
		if err != nil {
			for i := range out {
				_ = out[i].Close()
			}
			return nil, err
		}
		out = append(out, identity)
	}
	return out, nil
}

func openDirectoryIdentity(kind, path string) (pathIdentity, error) {
	clean, err := validateAbsolutePath(kind, path)
	if err != nil {
		return pathIdentity{}, err
	}
	file, info, err := openPathIdentity(clean, true)
	if err != nil {
		return pathIdentity{}, fmt.Errorf("open %s %q: %w", kind, clean, err)
	}
	if !info.IsDir() {
		_ = file.Close()
		return pathIdentity{}, fmt.Errorf("%s %q is not a directory", kind, clean)
	}
	return pathIdentity{Path: clean, File: file, Info: info}, nil
}

func openFileIdentity(kind, path string) (pathIdentity, error) {
	clean, err := validateAbsolutePath(kind, path)
	if err != nil {
		return pathIdentity{}, err
	}
	file, info, err := openPathIdentity(clean, false)
	if err != nil {
		return pathIdentity{}, fmt.Errorf("open %s %q: %w", kind, clean, err)
	}
	if info.IsDir() {
		_ = file.Close()
		return pathIdentity{}, fmt.Errorf("%s %q is a directory", kind, clean)
	}
	return pathIdentity{Path: clean, File: file, Info: info}, nil
}

func recheckBoundIsolation(policy *boundIsolationPolicy) error {
	if policy == nil {
		return fmt.Errorf("bound isolation policy is required")
	}
	for _, group := range [][]pathIdentity{
		policy.WritableRoots,
		policy.ReadOnlyRoots,
		policy.SystemRoots,
		policy.ForbiddenRoots,
	} {
		for i := range group {
			if err := recheckPathIdentity(&group[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func recheckPathIdentity(identity *pathIdentity) error {
	if identity == nil || identity.File == nil || identity.Info == nil {
		return fmt.Errorf("path identity is incomplete")
	}
	info, err := identity.File.Stat()
	if err != nil {
		return fmt.Errorf("recheck %q: %w", identity.Path, err)
	}
	if !os.SameFile(identity.Info, info) || info.Mode() != identity.Info.Mode() {
		return fmt.Errorf("path identity changed for %q", identity.Path)
	}
	// Pathname replacement detection for platforms that can only authorize by
	// path (Darwin seatbelt). A replaced directory at the same path fails closed.
	pathInfo, err := os.Lstat(identity.Path)
	if err != nil {
		return fmt.Errorf("recheck path %q: %w", identity.Path, err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path %q became a symlink after validation", identity.Path)
	}
	if !os.SameFile(identity.Info, pathInfo) {
		return fmt.Errorf("path %q was replaced after validation", identity.Path)
	}
	return nil
}

func closeAll(resources ...io.Closer) error {
	var first error
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		if err := resource.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func openPathIdentity(path string, directory bool) (*os.File, os.FileInfo, error) {
	switch runtime.GOOS {
	case "linux":
		return openPathIdentityLinux(path, directory)
	default:
		return openPathIdentityPortable(path, directory)
	}
}

func openPathIdentityPortable(path string, directory bool) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Stable system aliases are already normalized by Validated(). Any
		// remaining symlink is treated as an unsafe alias.
		return nil, nil, fmt.Errorf("path %q must not be a symlink", path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("path %q changed while opening", path)
	}
	if directory && !opened.IsDir() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("path %q is not a directory", path)
	}
	if !directory && opened.IsDir() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("path %q is a directory", path)
	}
	return file, opened, nil
}

func identityWithinAny(path string, roots []pathIdentity) bool {
	for _, root := range roots {
		if pathWithin(path, root.Path) {
			return true
		}
	}
	return false
}

func joinIdentityResources(policy *boundIsolationPolicy, extra ...io.Closer) []io.Closer {
	resources := make([]io.Closer, 0, 8+len(extra))
	if policy != nil {
		resources = append(resources, policy)
	}
	resources = append(resources, extra...)
	return resources
}

func currentWorkingDirectoryIdentity(path string) (pathIdentity, error) {
	return openDirectoryIdentity("cwd", path)
}

func executableIdentity(path string) (pathIdentity, error) {
	identity, err := openFileIdentity("executable", path)
	if err != nil {
		return pathIdentity{}, err
	}
	// Executability is checked from the validated mode bits. The descriptor is
	// retained so launch revalidation can detect replacement.
	if identity.Info.Mode().Perm()&0o111 == 0 && runtime.GOOS != "windows" {
		_ = identity.Close()
		return pathIdentity{}, fmt.Errorf("executable %q is not executable", path)
	}
	return identity, nil
}

func mustAbsClean(path string) string {
	return filepath.Clean(path)
}
