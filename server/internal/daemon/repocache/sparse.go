package repocache

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
)

const sparseProfilePath = ".multica/sparse-profile"
const maxSparseProfileBytes = 64 << 10

// sparseProfileAtRef reads the profile from the requested revision, not the
// daemon's current branch. Repositories without a profile retain full checkouts.
func sparseProfileAtRef(ctx context.Context, barePath, ref string) (string, error) {
	out, err := runGitOutputContext(ctx, "-C", barePath, "ls-tree", ref, "--", sparseProfilePath)
	if err != nil {
		return "", fmt.Errorf("inspect sparse profile at %s: %w", ref, err)
	}
	if len(out) == 0 {
		return "", nil
	}
	if !strings.HasPrefix(string(out), "100644 blob ") && !strings.HasPrefix(string(out), "100755 blob ") {
		return "", fmt.Errorf("%s must be a regular file", sparseProfilePath)
	}
	profile, err := runGitOutputContext(ctx, "-C", barePath, "show", ref+":"+sparseProfilePath)
	if err != nil {
		return "", fmt.Errorf("read sparse profile at %s: %w", ref, err)
	}
	if len(profile) > maxSparseProfileBytes || strings.IndexByte(string(profile), 0) >= 0 {
		return "", fmt.Errorf("%s must be a text file of at most %d bytes", sparseProfilePath, maxSparseProfileBytes)
	}
	if strings.TrimSpace(string(profile)) == "" {
		return "", nil
	}
	return string(profile), nil
}

func setSparseCheckoutContext(parent context.Context, checkoutPath, profile string) error {
	return setSparseCheckoutModeContext(parent, checkoutPath, profile, false)
}

func setSparseCheckoutModeContext(parent context.Context, checkoutPath, profile string, cone bool) error {
	if profile == "" {
		return disableSparseCheckoutContext(parent, checkoutPath)
	}
	ctx, cancel := context.WithTimeout(parent, repoCacheGitTimeout)
	defer cancel()
	mode := "--no-cone"
	if cone {
		mode = "--cone"
	}
	cmd := newGitCommand("-C", checkoutPath, "sparse-checkout", "set", mode, "--stdin")
	cmd.Stdin = strings.NewReader(profile)
	out, err := processtree.CombinedOutput(ctx, cmd, 5*time.Second)
	if err != nil {
		return fmt.Errorf("set sparse checkout: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Read a linked worktree's current sparse selection before migrating it to
// isolated metadata. The target ref's profile must not silently replace a
// selection the user may have edited in an existing checkout.
func currentSparseCheckoutContext(ctx context.Context, checkoutPath string) (string, bool, error) {
	enabled, err := sparseCheckoutEnabledContext(ctx, checkoutPath)
	if err != nil || !enabled {
		return "", false, err
	}
	patterns, err := runGitOutputContext(ctx, "-C", checkoutPath, "sparse-checkout", "list")
	if err != nil {
		return "", false, fmt.Errorf("read current sparse patterns: %w", err)
	}
	coneOut, err := runGitOutputContext(ctx, "-C", checkoutPath, "config", "--bool", "--get", "core.sparseCheckoutCone")
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", false, fmt.Errorf("read current sparse mode: %w", err)
		}
	}
	return string(patterns), strings.TrimSpace(string(coneOut)) == "true", nil
}

func sparseCheckoutEnabledContext(ctx context.Context, checkoutPath string) (bool, error) {
	out, err := runGitOutputContext(ctx, "-C", checkoutPath, "config", "--bool", "--get", "core.sparseCheckout")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("read current sparse state: %w", err)
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

func disableSparseCheckoutContext(ctx context.Context, checkoutPath string) error {
	out, err := runGitCombinedOutputContext(ctx, "-C", checkoutPath, "sparse-checkout", "disable")
	if err != nil {
		return fmt.Errorf("disable sparse checkout: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
