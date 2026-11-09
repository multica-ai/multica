package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/spf13/cobra"
)

func accountStatePath(dir, owner string) string {
	digest := sha256.Sum256([]byte(owner))
	return filepath.Join(dir, "accounts", hex.EncodeToString(digest[:])+".json")
}

func readAccountState(path string) (backendAccountState, error) {
	var state backendAccountState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.OwnerID == "" || state.DaemonID == "" || !filepath.IsAbs(state.WorkspacesRoot) || !validServiceProfile(state.StateProfile) {
		return state, fmt.Errorf("invalid saved execution state: %s", path)
	}
	return state, nil
}

func savedBackendAccountStates() ([]backendAccountState, error) {
	root, err := cli.ProfileDir("")
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(root, "services", "*", "accounts", "*.json"))
	if err != nil {
		return nil, err
	}
	var states []backendAccountState
	for _, path := range paths {
		state, err := readAccountState(path)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

// Automatic diagnostics select only the authenticated account for the command's
// backend. Offline inspection requires an explicit root or --all-profiles;
// neither a live process nor a single saved binding proves the caller's owner.
func boundExecutionRoot(cmd *cobra.Command, profile string) (string, error) {
	serverURL, err := backendURLForCommand(cmd, profile)
	if err != nil {
		return "", err
	}
	record, _, err := liveBackendService(serverURL)
	if err != nil {
		return "", err
	}
	dir, err := backendServiceDir(serverURL)
	if err != nil {
		return "", err
	}
	paths, err := filepath.Glob(filepath.Join(dir, "accounts", "*.json"))
	if err != nil {
		return "", err
	}
	if record == nil && len(paths) == 0 {
		return "", nil
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	owner, err := authenticatedDaemonOwner(ctx, profile, serverURL)
	if err != nil {
		return "", cli.WithUserMessage("Could not verify the execution account. Sign in and retry, or use --all-profiles or --workspaces-root <path> for offline inspection.", err)
	}
	if record != nil && record.OwnerID == owner {
		return record.WorkspacesRoot, nil
	}
	state, err := readAccountState(accountStatePath(dir, owner))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no saved execution state for the authenticated account; use --all-profiles or --workspaces-root to inspect an explicit scope")
	}
	if err != nil {
		return "", err
	}
	if state.OwnerID != owner {
		return "", fmt.Errorf("saved execution state belongs to another account")
	}
	return state.WorkspacesRoot, nil
}

// A launcher with a fresh profile must not create the backend's first binding
// before another profile's legacy state has been explicitly considered.
func requireLegacyStateMigration(cfg *daemon.Config) error {
	profiles, err := knownProfiles()
	if err != nil {
		return err
	}
	profiles = append([]string{cfg.Profile, ""}, profiles...)
	seen := make(map[string]bool)
	for _, profile := range profiles {
		if seen[profile] {
			continue
		}
		seen[profile] = true
		candidate := *cfg
		if profile != cfg.Profile {
			serverURL, err := backendURLForCommand(&cobra.Command{}, profile)
			if err != nil {
				return err
			}
			if serverURL != cfg.ServerBaseURL {
				continue
			}
			root, err := resolveWorkspacesRootForProfile(profile, "")
			if err != nil {
				return err
			}
			candidate.Profile, candidate.WorkspacesRoot = profile, root
		}
		present, err := legacyExecutionStatePresent(&candidate)
		if err != nil {
			return err
		}
		if present {
			return fmt.Errorf("legacy execution state exists for profile %q; stop its old daemon, then run 'multica --profile %q daemon migrate-state' to explicitly bind its runtime identity, workdirs and sessions to this account", profile, profile)
		}
	}
	return nil
}

func legacyExecutionStatePresent(cfg *daemon.Config) (bool, error) {
	if _, err := os.Stat(daemonShutdownResultPath(cfg.Profile)); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	entries, err := os.ReadDir(cfg.WorkspacesRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			return true, nil
		}
	}
	return false, nil
}

// Explicit adoption preserves the old namespace rather than moving live
// worktrees or inventing session aliases. Never reassign an already bound tree.
func migrateLegacyExecutionState(dir string, cfg daemon.Config, owner string) error {
	path := accountStatePath(dir, owner)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("this account already has an execution binding; refusing to replace it")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	states, err := savedBackendAccountStates()
	if err != nil {
		return err
	}
	root, err := util.ResolveSymlinksBestEffort(cfg.WorkspacesRoot)
	if err != nil {
		return fmt.Errorf("resolve legacy execution root: %w", err)
	}
	for _, state := range states {
		boundRoot, err := util.ResolveSymlinksBestEffort(state.WorkspacesRoot)
		if err != nil {
			return fmt.Errorf("resolve saved execution root: %w", err)
		}
		rel, relErr := filepath.Rel(root, boundRoot)
		reverse, reverseErr := filepath.Rel(boundRoot, root)
		if state.StateProfile == cfg.Profile || (relErr == nil && filepath.IsLocal(rel)) || (reverseErr == nil && filepath.IsLocal(reverse)) {
			return fmt.Errorf("legacy execution namespace is already bound; refusing to share account state")
		}
	}
	cfg.WorkspacesRoot = root
	return writeServiceJSON(path, backendAccountState{
		OwnerID: owner, StateProfile: cfg.Profile, WorkspacesRoot: cfg.WorkspacesRoot,
		DaemonID: cfg.DaemonID, LegacyDaemonIDs: cfg.LegacyDaemonIDs,
	})
}

func runDaemonMigrateState(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon migrate-state"); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	if err := requireKnownProfile(profile); err != nil {
		return err
	}
	operation, err := acquireBackendOperation(cmd)
	if err != nil {
		return err
	}
	defer operation.Close()
	serverURL, err := backendURLForCommand(cmd, profile)
	if err != nil {
		return err
	}
	dir, err := backendServiceDir(serverURL)
	if err != nil {
		return err
	}
	lock, acquired, err := tryLockDaemonProfile(filepath.Join(dir, "service.lock"))
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("stop the backend's execution service before migrating")
	}
	defer lock.Close()
	if err := refuseLegacyBackendDaemon(serverURL, profile); err != nil {
		return err
	}
	profileLock, err := acquireDaemonProfileLock(profile, false)
	if err != nil {
		return err
	}
	defer profileLock.Close()
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	owner, err := authenticatedDaemonOwner(ctx, profile, serverURL)
	if err != nil {
		return err
	}
	root, err := resolveWorkspacesRootForProfile(profile, flagString(cmd, "workspaces-root"))
	if err != nil {
		return err
	}
	cfg, err := daemon.LoadConfig(daemon.Overrides{Profile: profile, ServerURL: serverURL, WorkspacesRoot: root, DaemonID: flagString(cmd, "daemon-id")})
	if err != nil {
		return err
	}
	if err := migrateLegacyExecutionState(dir, cfg, owner); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Bound legacy profile %q to account %s; runtime identity, workdirs and provider sessions are preserved. Start the daemon with this profile.\n", profile, owner)
	return nil
}

func init() {
	cmd := &cobra.Command{
		Use: "migrate-state", Short: "Explicitly bind this profile's legacy runtime and sessions to the signed-in account",
		Long: "Stop all daemons for this backend first. This explicitly declares that the selected profile's historical workdirs and provider sessions belong to the authenticated account. No files are moved; existing runtime ownership is still enforced by the server.",
		Args: cobra.NoArgs, RunE: runDaemonMigrateState,
	}
	cmd.Flags().String("workspaces-root", "", "Existing legacy workspaces root")
	cmd.Flags().String("daemon-id", "", "Existing legacy daemon identity override")
	daemonCmd.AddCommand(cmd)
}
