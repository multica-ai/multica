package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/spf13/cobra"
)

type backendServiceRecord struct {
	ServerURL      string `json:"server_url"`
	OwnerID        string `json:"owner_id"`
	Profile        string `json:"profile"`
	StateProfile   string `json:"state_profile"`
	WorkspacesRoot string `json:"workspaces_root"`
	DaemonID       string `json:"daemon_id"`
	Port           int    `json:"port"`
	PID            int    `json:"pid"`
}

type backendAccountState struct {
	OwnerID         string   `json:"owner_id"`
	StateProfile    string   `json:"state_profile"`
	WorkspacesRoot  string   `json:"workspaces_root"`
	DaemonID        string   `json:"daemon_id"`
	LegacyDaemonIDs []string `json:"legacy_daemon_ids,omitempty"`
}

func backendServiceDir(serverURL string) (string, error) {
	key, err := daemon.BackendServiceKey(serverURL)
	if err != nil {
		return "", err
	}
	root, err := cli.ProfileDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "services", key), nil
}

func writeServiceJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".service-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func backendURLForCommand(cmd *cobra.Command, profile string) (string, error) {
	raw := resolveDaemonServerURL(cmd, profile)
	if raw == "" {
		raw = daemon.DefaultServerURL
	}
	return daemon.BackendServiceURL(raw)
}

func authenticatedDaemonOwner(ctx context.Context, profile, serverURL string) (string, error) {
	owner, _, err := daemonAccountCredentials(ctx, profile, serverURL)
	return owner, err
}

func daemonAccountCredentials(ctx context.Context, profile, serverURL string) (string, string, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return "", "", err
	}
	if cfg.Token == "" {
		return "", "", fmt.Errorf("sign in before connecting to the local execution service")
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := cli.NewAPIClient(serverURL, "", cfg.Token).GetJSON(ctx, "/api/me", &me); err != nil {
		return "", "", fmt.Errorf("verify daemon account: %w", err)
	}
	if me.ID == "" {
		return "", "", fmt.Errorf("backend did not identify the authenticated daemon account")
	}
	return me.ID, cfg.Token, nil
}

func liveBackendService(serverURL string) (*backendServiceRecord, map[string]any, error) {
	dir, err := backendServiceDir(serverURL)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "active.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var record backendServiceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, nil, fmt.Errorf("invalid local service record: %w", err)
	}
	if !validServiceProfile(record.Profile) || record.OwnerID == "" || record.ServerURL != serverURL || record.Port < 1 || record.Port > 65535 || record.PID < 1 {
		return nil, nil, fmt.Errorf("invalid local service identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, record.Port)
	if !daemonAlive(health) {
		return nil, nil, nil
	}
	if health["pid"] != float64(record.PID) || health["profile"] != record.Profile || health["owner_id"] != record.OwnerID || health["daemon_id"] != record.DaemonID {
		return nil, nil, fmt.Errorf("local service identity changed; refusing automatic adoption")
	}
	got, _ := health["server_url"].(string)
	normalized, err := daemon.BackendServiceURL(got)
	if err != nil || normalized != serverURL {
		return nil, nil, fmt.Errorf("local service belongs to another backend")
	}
	return &record, health, nil
}

// Older daemons do not hold a backend lock or attest an account. Never start
// over them, even if they use another profile. Upgrade/restart that profile
// first so its own state is bound before another launcher can adopt it.
func refuseLegacyBackendDaemon(serverURL, requested string) error {
	profiles, err := knownProfiles()
	if err != nil {
		return err
	}
	profiles = append([]string{""}, profiles...)
	for _, profile := range profiles {
		port := resolveHealthPort(profile)
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		health := checkDaemonHealthOnPort(ctx, port)
		cancel()
		if !daemonAlive(health) {
			continue
		}
		if reported, known := health["profile"].(string); known && reported != profile {
			continue
		}
		raw, _ := health["server_url"].(string)
		if raw == "" {
			config, err := cli.LoadCLIConfigForProfile(profile)
			if err != nil {
				return err
			}
			raw = config.ServerURL
			if raw == "" {
				raw = daemon.DefaultServerURL
			}
		}
		normalized, err := daemon.BackendServiceURL(raw)
		if err != nil || normalized != serverURL {
			continue
		}
		return fmt.Errorf("a daemon for this backend is already running under %s; stop it explicitly and restart that profile with the updated CLI before switching launchers (requested %s)", describeProfile(profile), describeProfile(requested))
	}
	return nil
}

// Called while holding the backend lifetime lock. New bindings always use an
// account-scoped namespace: legacy files do not prove their historical owner.
func bindBackendAccount(dir string, cfg *daemon.Config, owner string) error {
	digest := sha256.Sum256([]byte(owner))
	accountKey := hex.EncodeToString(digest[:])
	path := filepath.Join(dir, "accounts", accountKey+".json")
	data, err := os.ReadFile(path)
	var state backendAccountState
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return err
		}
		if state.OwnerID != owner || !filepath.IsAbs(state.WorkspacesRoot) || !validServiceProfile(state.StateProfile) || state.DaemonID == "" {
			return fmt.Errorf("invalid saved execution state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		bindings, err := filepath.Glob(filepath.Join(dir, "accounts", "*.json"))
		if err != nil {
			return err
		}
		if len(bindings) == 0 {
			if err := requireLegacyStateMigration(cfg); err != nil {
				return err
			}
		}
		backendKey, err := daemon.BackendServiceKey(cfg.ServerBaseURL)
		if err != nil {
			return err
		}
		state = backendAccountState{
			OwnerID:        owner,
			StateProfile:   "execution-" + backendKey[:16] + "-" + accountKey[:16],
			WorkspacesRoot: filepath.Join(cfg.WorkspacesRoot, ".multica-accounts", backendKey[:16]+"-"+accountKey[:16]),
			DaemonID:       uuid.NewSHA1(uuid.NameSpaceOID, []byte(cfg.DaemonID+"\x00"+backendKey+"\x00"+owner)).String(),
		}
		if err := writeServiceJSON(path, state); err != nil {
			return err
		}
	}
	cfg.StateProfile = &state.StateProfile
	cfg.WorkspacesRoot = state.WorkspacesRoot
	cfg.LegacyDaemonIDs = append([]string(nil), state.LegacyDaemonIDs...)
	cfg.DaemonID = state.DaemonID
	cfg.OwnerID = owner

	return nil
}

func prepareBackendService(cfg *daemon.Config, expectedOwner string) (*os.File, string, error) {
	if !validServiceProfile(cfg.Profile) {
		return nil, "", fmt.Errorf("invalid local execution profile")
	}
	serverURL, err := daemon.BackendServiceURL(cfg.ServerBaseURL)
	if err != nil {
		return nil, "", err
	}
	dir, err := backendServiceDir(serverURL)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, "", err
	}
	lock, acquired, err := tryLockDaemonProfile(filepath.Join(dir, "service.lock"))
	if os.Getenv("MULTICA_DAEMON_HANDOFF") == "1" {
		deadline := time.Now().Add(35 * time.Second)
		for err == nil && !acquired && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			lock, acquired, err = tryLockDaemonProfile(filepath.Join(dir, "service.lock"))
		}
	}
	if err != nil {
		return nil, "", err
	}
	if !acquired {
		return nil, "", fmt.Errorf("another daemon is already serving this backend; connect to it or use 'daemon takeover' explicitly")
	}
	fail := func(err error) (*os.File, string, error) { lock.Close(); return nil, "", err }
	if err := refuseLegacyBackendDaemon(serverURL, cfg.Profile); err != nil {
		return fail(err)
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	owner, token, err := daemonAccountCredentials(ctx, cfg.Profile, serverURL)
	if err != nil {
		return fail(err)
	}
	if expectedOwner != "" && expectedOwner != owner {
		return fail(fmt.Errorf("profile account changed during startup; refusing to replace the confirmed account"))
	}
	_ = os.Unsetenv("MULTICA_DAEMON_HANDOFF_OWNER")
	cfg.ServerBaseURL = serverURL
	cfg.ServiceToken = token
	if err := bindBackendAccount(dir, cfg, owner); err != nil {
		return fail(err)
	}
	return lock, dir, nil
}

type daemonOperationBackendKey struct{}

func daemonOperationBackend(cmd *cobra.Command) string {
	if ctx := cmd.Context(); ctx != nil {
		serverURL, _ := ctx.Value(daemonOperationBackendKey{}).(string)
		return serverURL
	}
	return ""
}

// Every lifecycle transition has the same lock order: operation, backend,
// profile. A background child inherits a parent PID marker while its starter
// holds operation.lock; arbitrary direct foreground starts do not bypass it.
func acquireBackendOperation(cmd *cobra.Command) (*os.File, error) {
	serverURL, err := backendURLForCommand(cmd, resolveProfile(cmd))
	if err != nil {
		return nil, err
	}
	dir, err := backendServiceDir(serverURL)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, acquired, err := tryLockDaemonProfile(filepath.Join(dir, "operation.lock"))
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("another local service transition is in progress; retry after it finishes")
	}
	// Keep the lock, service lookup, and replacement on the same backend even
	// if another command rewrites the profile configuration during this operation.
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, daemonOperationBackendKey{}, serverURL))
	return lock, nil
}

func runBackendTakeover(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("daemon takeover"); err != nil {
		return err
	}
	operation, err := acquireBackendOperation(cmd)
	if err != nil {
		return err
	}
	defer operation.Close()
	return runBackendTransitionLocked(cmd, true)
}

// The operation lock must cover identity checks, stopping, credential commit and
// replacement readiness. A normal restart can never authorize an account switch.
func runBackendTransitionLocked(cmd *cobra.Command, allowAccountSwitch bool) error {
	profile := resolveProfile(cmd)
	serverURL, err := backendURLForCommand(cmd, profile)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var credentials *desktopHandoffCredentials
	owner := ""
	if input, _ := cmd.Flags().GetBool("desktop-credentials-stdin"); input {
		credentials, err = readDesktopHandoffCredentials(cmd, profile, serverURL)
		if err != nil {
			return err
		}
		owner = credentials.OwnerID
	} else {
		owner, err = authenticatedDaemonOwner(ctx, profile, serverURL)
		if err != nil {
			return err
		}
	}

	record, health, err := liveBackendService(serverURL)
	if err != nil {
		return err
	}
	if expected, _ := cmd.Flags().GetInt("expected-pid"); expected > 0 && (record == nil || record.PID != expected) {
		return fmt.Errorf("local execution service changed after confirmation; retry the handoff")
	}
	if record != nil && !allowAccountSwitch && record.OwnerID != owner {
		return fmt.Errorf("another account owns the service; use an explicit takeover")
	}
	if record != nil {
		force, _ := cmd.Flags().GetBool("force")
		if err := drainAndStopBackend(record, health, force); err != nil {
			return err
		}
		dir, err := backendServiceDir(serverURL)
		if err != nil {
			return err
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			lock, free, err := tryLockDaemonProfile(filepath.Join(dir, "service.lock"))
			if lock != nil {
				lock.Close()
			}
			if err != nil {
				return err
			}
			if free {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("previous backend service has not released its lock")
			}
			time.Sleep(20 * time.Millisecond)
		}

	}
	if record == nil {
		if err := refuseLegacyBackendDaemon(serverURL, profile); err != nil {
			return err
		}
	}
	if credentials != nil {
		config, err := cli.LoadCLIConfigForProfile(profile)
		if err != nil {
			return err
		}
		dir, err := cli.ProfileDir(profile)
		if err != nil {
			return err
		}
		// Invalidate the cache-owner hint before changing the token. A partial
		// write must never label a new account's token with the old owner.
		if err := os.Remove(filepath.Join(dir, ".desktop-user-id")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		config.Token, config.ServerURL = credentials.Token, serverURL
		if err := cli.SaveCLIConfigForProfile(config, profile); err != nil {
			return err
		}
		// This explicit Desktop operation commits the cache-owner sidecar too.
		// Ordinary CLI config rewrites still leave it alone.
		if err := os.WriteFile(filepath.Join(dir, ".desktop-user-id"), []byte(owner), 0600); err != nil {
			return err
		}
	}
	transitionContext := cmd.Context()
	if transitionContext == nil {
		transitionContext = context.Background()
	}
	cmd.SetContext(context.WithValue(transitionContext, daemonTransitionOwnerKey{}, owner))
	if foreground, _ := cmd.Flags().GetBool("foreground"); foreground {
		return runTransitionForeground(cmd)
	}
	return runDaemonBackgroundUnlocked(cmd)
}

// The caller retains operation.lock until the foreground process has published
// its identity. runDaemonForeground then releases it for normal stop commands.
var runTransitionForeground = func(cmd *cobra.Command) error {
	return runDaemonForeground(cmd)
}

// Secrets travel only over stdin, never process arguments or diagnostics.
type desktopHandoffCredentials struct {
	Token   string `json:"token"`
	OwnerID string `json:"owner_id"`
}

func readDesktopHandoffCredentials(cmd *cobra.Command, profile, serverURL string) (*desktopHandoffCredentials, error) {
	if profile == "" || !strings.HasPrefix(profile, "desktop-") || !validServiceProfile(profile) {
		return nil, fmt.Errorf("credential handoff requires a Desktop authentication profile")
	}
	data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 65537))
	if err != nil || len(data) > 65536 {
		return nil, fmt.Errorf("invalid handoff credentials")
	}
	var credentials desktopHandoffCredentials
	if json.Unmarshal(data, &credentials) != nil || credentials.Token == "" || credentials.OwnerID == "" {
		return nil, fmt.Errorf("invalid handoff credentials")
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var me struct {
		ID string `json:"id"`
	}
	if err := cli.NewAPIClient(serverURL, "", credentials.Token).GetJSON(ctx, "/api/me", &me); err != nil || me.ID != credentials.OwnerID {
		return nil, fmt.Errorf("could not verify replacement account; running service left untouched")
	}
	return &credentials, nil
}

var daemonTakeoverCmd = &cobra.Command{
	Use: "takeover", Short: "Explicitly hand the backend's execution service to this profile", RunE: runBackendTakeover,
}

func init() {
	daemonTakeoverCmd.Flags().Bool("desktop-credentials-stdin", false, "Read replacement Desktop credentials from stdin")
	_ = daemonTakeoverCmd.Flags().MarkHidden("desktop-credentials-stdin")
	daemonRestartCmd.Flags().Int("expected-pid", 0, "Refuse restart if the observed service changed")
	daemonRestartCmd.Flags().Bool("force", false, "Interrupt active tasks; cleanup must still be confirmed")
	daemonTakeoverCmd.Flags().Int("expected-pid", 0, "Refuse handoff if the observed service process changed")
	daemonTakeoverCmd.Flags().Bool("force", false, "Interrupt active tasks instead of waiting; never restart if cleanup is unconfirmed")
	daemonCmd.AddCommand(daemonTakeoverCmd)
}

func drainAndStopBackend(record *backendServiceRecord, health map[string]any, force bool) error {
	if !force {
		if err := setServiceDrain(record.Port, http.MethodPost); err != nil {
			return err
		}
		defer setServiceDrain(record.Port, http.MethodDelete)
		deadline := time.Now().Add(daemonShutdownLockWait)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			current := checkDaemonHealthOnPort(ctx, record.Port)
			cancel()
			if !daemonAlive(current) || current["pid"] != float64(record.PID) || current["profile"] != record.Profile || current["draining"] != true {
				return fmt.Errorf("daemon changed while waiting for handoff; retry")
			}
			count, known := current["active_task_count"].(float64)
			claims, knownClaims := current["claims_in_flight"].(float64)
			if known && knownClaims && count == 0 && claims == 0 {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("tasks are still active; handoff cancelled, retry later or explicitly use --force")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Existing stop confirmation waits for the profile lock AND task cleanup.
	return stopDaemonAndWait(record.Profile, record.Port, health, daemonShutdownLockWait, 2*time.Second)
}

func setServiceDrain(port int, method string) error {
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d/drain", port), nil)
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon cannot drain safely (HTTP %d)", res.StatusCode)
	}
	return nil
}

// Same-account background starts attach; a different account requires an
// explicit takeover. The foreground lifetime lock is the final race guard.
func reuseBackendService(cmd *cobra.Command) (bool, error) {
	profile := resolveProfile(cmd)
	serverURL, err := backendURLForCommand(cmd, profile)
	if err != nil {
		return false, err
	}
	record, _, err := liveBackendService(serverURL)
	if err != nil || record == nil {
		return false, err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	owner, err := authenticatedDaemonOwner(ctx, profile, serverURL)
	if err != nil {
		return false, err
	}
	if owner != record.OwnerID {
		return false, fmt.Errorf("this backend already has a daemon owned by another account; use 'daemon takeover' only after confirming the account switch")
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Using the backend's existing daemon (pid %d, profile %s).\n", record.PID, describeProfile(record.Profile))
	return true, nil
}

func validServiceProfile(profile string) bool {
	if profile == "" {
		return true
	}
	if !filepath.IsLocal(profile) || (len(profile) > 1 && profile[1] == ':') {
		return false
	}
	for _, part := range strings.Split(strings.ReplaceAll(profile, "\\", "/"), "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	return true
}
