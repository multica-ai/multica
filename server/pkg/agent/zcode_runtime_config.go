package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	zcodePersonalConfigEnv   = "ZCODE_PERSONAL_PROVIDER_CONFIG_FILE"
	zcodeProvidersTempPrefix = "multica-zcode-providers-"
	// zcodeSnapshotOwnerFile is locked by the process using a snapshot for
	// the snapshot's whole lifetime (lockZcodeProviderSnapshot).
	zcodeSnapshotOwnerFile = "owner"
	// zcodeSnapshotMinAge keeps the startup sweep away from snapshots still
	// being written: a directory exists briefly before its owner lock does.
	zcodeSnapshotMinAge = time.Minute
	// zcodeUnownedSnapshotMaxAge retires snapshots that carry no owner file
	// (written before the lock existed, or where locking failed). Their
	// writer cannot be checked, so they must outlive any plausible run.
	zcodeUnownedSnapshotMaxAge = 24 * time.Hour
)

type zcodeDesktopProvider struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Enabled *bool             `json:"enabled"`
	Headers map[string]string `json:"headers"`
	Options struct {
		APIKey  string            `json:"apiKey"`
		BaseURL string            `json:"baseURL"`
		Headers map[string]string `json:"headers"`
	} `json:"options"`
	Models map[string]struct {
		Limit struct {
			Context int64 `json:"context"`
		} `json:"limit"`
	} `json:"models"`
}

func zcodeEnvValue(env []string, key string) string {
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			return strings.TrimPrefix(entry, key+"=")
		}
	}
	return ""
}

func readZcodeConfig(path string, into any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return fmt.Errorf("ZCode provider config exceeds 2 MiB")
	}
	return json.Unmarshal(data, into)
}

// prepareZcodeRuntimeEnv adapts Desktop's enabled API-key connections into a
// private, process-owned native provider repository snapshot under the process
// temp dir (multica-zcode-providers-*). It reads Desktop's enabled connections
// to build that snapshot, but never edits Desktop's repositories or converts
// Start Plan identity tokens into API keys. Explicit native repositories remain
// authoritative. The snapshot carries an owner lock held until cleanup, so
// CleanupStaleZcodeProviderSnapshots on daemon startup removes crash leftovers
// without touching snapshots other daemons on the machine are still using.
func prepareZcodeRuntimeEnv(executable string, env []string) ([]string, func(), error) {
	noop := func() {}
	if zcodeEnvValue(env, zcodePersonalConfigEnv) != "" {
		return zcodeRuntimeEnv(executable, env), noop, nil
	}
	builtin := filepath.Join(filepath.Dir(executable), "..", "config", "provider", "zcode-builtin.json")
	if !isNodeScriptPath(executable) {
		return env, noop, nil
	}
	if info, err := os.Stat(builtin); err != nil || !info.Mode().IsRegular() {
		return env, noop, nil
	}
	base := zcodeEnvValue(env, "ZCODE_DATA_BASE_DIR")
	if base == "" {
		var err error
		base, err = os.UserHomeDir()
		if err != nil {
			return nil, noop, err
		}
	}
	root := filepath.Join(base, ".zcode", "v2")
	var desktop struct {
		Providers map[string]zcodeDesktopProvider `json:"provider"`
	}
	err := readZcodeConfig(filepath.Join(root, "config.json"), &desktop)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, noop, fmt.Errorf("read ZCode Desktop connections: %w", err)
	}
	native := map[string]any{"schemaVersion": 1, "config": map[string]any{}}
	err = readZcodeConfig(filepath.Join(root, "provider_config.json"), &native)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, noop, fmt.Errorf("read ZCode native provider repository: %w", err)
	}
	config, ok := native["config"].(map[string]any)
	if !ok {
		return nil, noop, fmt.Errorf("ZCode native provider repository has no config object")
	}
	group := func(key string) map[string]any {
		if value, ok := config[key].(map[string]any); ok {
			return value
		}
		value := map[string]any{}
		config[key] = value
		return value
	}
	providers, models := group("providerConfigRules"), group("modelConfigRules")
	providerRules, _ := providers["providerRules"].([]any)
	modelRules, _ := models["providerModelRules"].([]any)
	order, _ := config["providerOrder"].([]any)
	if providerRules == nil {
		providerRules = []any{}
	}
	if modelRules == nil {
		modelRules = []any{}
	}
	if order == nil {
		order = []any{}
	}
	sourceIDs := make([]string, 0, len(desktop.Providers))
	for id := range desktop.Providers {
		sourceIDs = append(sourceIDs, id)
	}
	sort.Strings(sourceIDs)
	for _, sourceID := range sourceIDs {
		provider := desktop.Providers[sourceID]
		if provider.Enabled == nil || !*provider.Enabled || strings.TrimSpace(provider.Options.APIKey) == "" || len(provider.Models) == 0 {
			continue
		}
		if strings.HasPrefix(sourceID, "builtin:") && strings.Contains(sourceID, "start-plan") {
			continue
		}
		providerID := "multica-desktop:" + sourceID
		ids := make([]string, 0, len(provider.Models))
		for id := range provider.Models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		apiType := "openai-chat-completions"
		switch provider.Kind {
		case "anthropic":
			apiType = "anthropic-messages"
		case "openai":
			apiType = "openai-responses"
		}
		api := map[string]any{"type": apiType}
		if provider.Options.BaseURL != "" {
			api["baseUrl"] = provider.Options.BaseURL
		}
		headers := map[string]string{}
		for key, value := range provider.Headers {
			headers[key] = value
		}
		for key, value := range provider.Options.Headers {
			headers[key] = value
		}
		if len(headers) > 0 {
			api["headers"] = headers
		}
		rule := map[string]any{"providerId": providerID, "config": map[string]any{
			"group": "standard-personal", "access": map[string]any{"type": "api-key", "apiKey": provider.Options.APIKey},
			"api": api, "personalModelIds": ids, "modelOrder": ids,
		}}
		if provider.Name != "" {
			rule["providerName"] = provider.Name
		}
		providerRules = append(providerRules, rule)
		order = append(order, providerID)
		for _, id := range ids {
			modelConfig := map[string]any{"enabled": true}
			if contextWindow := provider.Models[id].Limit.Context; contextWindow > 0 {
				modelConfig["properties"] = map[string]any{"contextWindow": contextWindow}
			}
			modelRules = append(modelRules, map[string]any{"providerId": providerID, "modelId": id, "config": modelConfig})
		}
	}
	providers["providerRules"], models["providerModelRules"], config["providerOrder"] = providerRules, modelRules, order
	if _, present := models["manualProviderModelRules"]; !present {
		models["manualProviderModelRules"] = []any{}
	}
	data, err := json.Marshal(native)
	if err != nil {
		return nil, noop, fmt.Errorf("encode process-owned ZCode provider repository: %w", err)
	}
	temp, err := os.MkdirTemp("", zcodeProvidersTempPrefix)
	if err != nil {
		return nil, noop, err
	}
	owner := lockZcodeProviderSnapshot(temp)
	cleanup := func() {
		releasePiSessionFileLock(owner)
		_ = os.RemoveAll(temp)
	}
	file := filepath.Join(temp, "providers.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		cleanup()
		return nil, noop, err
	}
	env = zcodeRuntimeEnv(executable, env)
	return append(env, zcodePersonalConfigEnv+"="+file), cleanup, nil
}

// lockZcodeProviderSnapshot marks a snapshot directory as in use by this
// process: it creates the owner file and holds an exclusive lock on it until
// the snapshot's cleanup releases it. The kernel drops the lock when the
// process dies, which is how CleanupStaleZcodeProviderSnapshots tells a crash
// leftover from a snapshot another daemon on this machine is still using.
//
// The lock reuses the Pi transcript lock (flock on Unix, a LockFileEx sidecar
// on Windows). When the filesystem refuses the lock, the owner file is
// removed again, so the snapshot counts as unowned and the sweep retires it
// only by age; the run itself goes ahead as before.
func lockZcodeProviderSnapshot(dir string) *os.File {
	path := filepath.Join(dir, zcodeSnapshotOwnerFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	_ = file.Close()
	lock, locked, err := tryLockPiSessionFile(path)
	if err != nil || !locked {
		_ = os.Remove(path)
		return nil
	}
	return lock
}

// CleanupStaleZcodeProviderSnapshots removes orphaned process-private provider
// repository directories left under the process temp dir when the process that
// wrote them exited without running the per-session cleanup (crash / SIGKILL).
//
// Several daemons (Desktop's profile and a CLI profile, say) can share one
// temp dir, so a directory is only removed once its writer is gone: its owner
// lock can be taken. A directory without an owner file, written by a daemon
// that predates the lock, cannot be checked and is removed only after
// zcodeUnownedSnapshotMaxAge. Nothing younger than zcodeSnapshotMinAge is
// touched, which covers the moment between creating a directory and locking
// it.
//
// Best-effort: failures to remove individual matches are ignored; the count of
// removed directories is returned for logging.
func CleanupStaleZcodeProviderSnapshots() int {
	return cleanupStaleZcodeProviderSnapshots(os.TempDir(), time.Now())
}

func cleanupStaleZcodeProviderSnapshots(tempDir string, now time.Time) int {
	matches, err := filepath.Glob(filepath.Join(tempDir, zcodeProvidersTempPrefix+"*"))
	if err != nil {
		return 0
	}
	removed := 0
	for _, dir := range matches {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if !strings.HasPrefix(filepath.Base(dir), zcodeProvidersTempPrefix) {
			continue
		}
		if !zcodeProviderSnapshotStale(dir, now.Sub(info.ModTime())) {
			continue
		}
		if err := os.RemoveAll(dir); err == nil {
			removed++
		}
	}
	return removed
}

// zcodeProviderSnapshotStale reports whether no live process owns the
// snapshot in dir, last modified age ago.
func zcodeProviderSnapshotStale(dir string, age time.Duration) bool {
	if age < zcodeSnapshotMinAge {
		return false
	}
	owner := filepath.Join(dir, zcodeSnapshotOwnerFile)
	if _, err := os.Lstat(owner); err != nil {
		return errors.Is(err, os.ErrNotExist) && age >= zcodeUnownedSnapshotMaxAge
	}
	lock, locked, err := tryLockPiSessionFile(owner)
	if err != nil || !locked {
		return false
	}
	// Released before removal: Windows cannot remove a directory while a
	// handle inside it is open. No process adopts an existing snapshot, so
	// nothing can take the lock in between.
	releasePiSessionFileLock(lock)
	return true
}
