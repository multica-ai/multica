package repocache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// initiatorRefreshMaxDepth bounds how far below the task workdir a managed
// checkout is searched for. Checkouts land directly in the requested workdir,
// which the checkout endpoint only accepts inside the task root, so a few
// levels cover every layout the endpoint can produce.
const initiatorRefreshMaxDepth = 3

// RefreshInitiatorAuthor re-applies the initiator author override to every
// checkout this cache manages under workDir. A resumed run often commits in a
// checkout an earlier run created without calling checkout again, so the
// override written then would keep naming the earlier run's initiator. Call it
// at the start of every run with that run's settings and initiator; when the
// setting is off or the run has no member initiator it removes a stale
// override. Checkouts not created by this cache are left untouched.
func (c *Cache) RefreshInitiatorAuthor(ctx context.Context, workDir string, params WorktreeParams) error {
	if workDir == "" {
		return nil
	}
	var errs []string
	for _, checkout := range c.managedCheckoutsUnder(ctx, workDir) {
		if err := applyInitiatorAuthorContext(ctx, checkout, params); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", checkout, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("refresh initiator author: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (c *Cache) managedCheckoutsUnder(ctx context.Context, root string) []string {
	var found []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > initiatorRefreshMaxDepth || ctx.Err() != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
				if c.isManagedCheckout(ctx, path) {
					found = append(found, path)
				}
				continue
			}
			walk(path, depth+1)
		}
	}
	walk(root, 1)
	return found
}

// isManagedCheckout accepts a linked worktree of one of this cache's bare
// repositories, or an isolated checkout this cache created.
func (c *Cache) isManagedCheckout(ctx context.Context, path string) bool {
	if isIsolatedCheckoutContext(ctx, path) {
		return true
	}
	if !isGitWorktree(path) {
		return false
	}
	out, err := runGitOutputContext(ctx, "-C", path, "rev-parse", "--git-common-dir")
	if err != nil {
		return false
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(path, commonDir)
	}
	rel, err := filepath.Rel(resolvedPath(c.root), resolvedPath(commonDir))
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func resolvedPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}
