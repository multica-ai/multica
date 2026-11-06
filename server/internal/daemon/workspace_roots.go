package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ResolveWorkspacesRootForProfile is shared by startup, disk usage and GC.
// Precedence is flag, environment, profile config, then the profile default.
func ResolveWorkspacesRootForProfile(profile, flagValue string) (string, error) {
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	override := strings.TrimSpace(flagValue)
	if override == "" {
		override = strings.TrimSpace(os.Getenv("MULTICA_WORKSPACES_ROOT"))
	}
	if override == "" {
		override = cfg.WorkspacesRoot
	}
	return ResolveWorkspacesRoot(profile, override)
}

// EnumerateWorkspaceRoots includes the default profile and existing named
// roots, honoring each profile's config and scanning shared paths only once.
func EnumerateWorkspaceRoots() ([]DiskUsageRoot, error) {
	var roots []DiskUsageRoot
	if root, err := ResolveWorkspacesRootForProfile("", ""); err == nil {
		roots = append(roots, DiskUsageRoot{Profile: "", Root: root})
	}
	base, err := cli.ProfileDir("")
	if err != nil {
		return roots, nil
	}
	entries, err := os.ReadDir(filepath.Join(base, "profiles"))
	if err != nil {
		return roots, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root, err := ResolveWorkspacesRootForProfile(entry.Name(), "")
		if err != nil || containsWorkspaceRoot(roots, root) {
			continue
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, DiskUsageRoot{Profile: entry.Name(), Root: root})
	}
	return roots, nil
}

func containsWorkspaceRoot(roots []DiskUsageRoot, candidate string) bool {
	for _, root := range roots {
		a, errA := filepath.Abs(root.Root)
		b, errB := filepath.Abs(candidate)
		if errA == nil && errB == nil && a == b {
			return true
		}
		ai, errA := os.Stat(root.Root)
		bi, errB := os.Stat(candidate)
		if errA == nil && errB == nil && os.SameFile(ai, bi) {
			return true
		}
	}
	return false
}
