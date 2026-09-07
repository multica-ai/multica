package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// OpenClaw config discovery costs two serial CLI round-trips per task
// preparation in the common case (`config validate --json`, then
// `config get agents.list --json`; see openclawMaxCLIDeadlinesPerPreparation for
// the worst case). On a fast host that is ~1s total; on the Intel Mac in #7112
// it is ~12.7s, and it is paid again for every task — including every chat
// message, because Reuse runs the same preparation path as Prepare. Raising
// the deadline alone would turn a hard failure into a per-message stall, so
// the discovery result is cached.
//
// The cache lives on disk rather than in package memory on purpose:
// Prepare/Reuse run inside a one-shot preparation helper process
// (PrepareIsolated / ReuseIsolated), so an in-process cache would never
// survive to the next task.
//
// What is NOT cached, and no longer exists to cache: the resolved config read
// this flow used to make for a managed mcp_config. Managed MCP is now prepared
// from a reset stage this package writes, so there is no per-agent CLI payload on
// this path at all — one fewer thing whose staleness would have to be reasoned
// about, and one fewer copy of the user's configuration anywhere.
const (
	// openclawDiscoveryCacheFile is the per-profile cache file name. It sits
	// in the profile directory (~/.multica[/profiles/<name>]) so every task on
	// this daemon shares one entry.
	openclawDiscoveryCacheFile = "openclaw-discovery-cache.json"

	// openclawDiscoveryCacheVersion guards the on-disk shape. A daemon that
	// reads an entry written by a different version treats it as a miss.
	openclawDiscoveryCacheVersion = 1

	// openclawDiscoveryCacheTTL bounds how long a hit can be served. The
	// fingerprint catches the changes we can observe cheaply (binary swap,
	// OpenClaw path env, active config rewrite), but not the ones we cannot:
	// a nested `$include` target edited in place, or an agent added through
	// OpenClaw's own SQLite registry, neither of which touches the top-level
	// config file. The TTL is the backstop that keeps those visible, at the
	// cost of re-running discovery at most once a minute.
	openclawDiscoveryCacheTTL = time.Minute

	// openclawDiscoveryCacheFutureSkew is how far ahead of the reader's clock
	// sample an entry may be dated and still count as fresh.
	//
	// Without it, tasks starting together on one daemon evict each other's
	// work. Every caller samples time.Now() once and passes it down, so a peer
	// that sampled later but committed its rename first leaves an entry dated
	// microseconds "in the future" — and the reader, whose own store just
	// succeeded, throws it away and re-runs discovery for nothing. The same
	// interleaving is what made TestOpenclawDiscoveryCacheConcurrentPreparations
	// flake in CI.
	//
	// One second is far above that window and far below what the guard below
	// exists to catch. It also bounds the cost of being wrong: an entry dated
	// into the future outlives its TTL by at most this much.
	openclawDiscoveryCacheFutureSkew = time.Second
)

// openclawDiscoveryFingerprint is the "is this entry still true?" evidence
// captured when discovery ran. Every field must match on lookup; anything
// unequal (or unstattable) is a miss and the CLI runs again.
type openclawDiscoveryFingerprint struct {
	Version int `json:"version"`
	// Binary identity: the resolved absolute path plus size/mtime, so an
	// upgrade in place or a PATH change to a different install invalidates.
	BinPath        string `json:"bin_path"`
	BinSize        int64  `json:"bin_size"`
	BinModTimeNano int64  `json:"bin_mod_time_nano"`
	// Env is the fingerprint of the environment variables that can move
	// OpenClaw's active config out from under us. Values are compared, not
	// hashed: they are paths, not secrets.
	Env string `json:"env"`
	// Active config stat, keyed to the path discovery reported.
	ConfigPath        string `json:"config_path"`
	ConfigSize        int64  `json:"config_size"`
	ConfigModTimeNano int64  `json:"config_mod_time_nano"`
}

// openclawDiscoveryCacheEntry is one cached successful discovery.
type openclawDiscoveryCacheEntry struct {
	Fingerprint      openclawDiscoveryFingerprint `json:"fingerprint"`
	CachedAtNano     int64                        `json:"cached_at_nano"`
	ActiveConfigPath string                       `json:"active_config_path"`
	// AgentsList is the resolved `agents.list` array, stored verbatim so a hit
	// reproduces exactly what the CLI returned.
	AgentsList         json.RawMessage `json:"agents_list"`
	AgentsFromRegistry bool            `json:"agents_from_registry"`
}

// openclawDiscoveryEnvVars are the environment variables that change which
// config OpenClaw considers active. HOME / USERPROFILE are included because
// every default candidate path is derived from them, and the CLAWDBOT_* pair
// because openclawFallbackConfigCandidates still honours those legacy names —
// a cache that ignored them could keep serving the pre-switch config for a
// full TTL after the user pointed the daemon somewhere else.
var openclawDiscoveryEnvVars = []string{
	"OPENCLAW_CONFIG_PATH",
	"OPENCLAW_HOME",
	"OPENCLAW_STATE_DIR",
	"CLAWDBOT_CONFIG_PATH",
	"CLAWDBOT_STATE_DIR",
	"HOME",
	"USERPROFILE",
}

func openclawDiscoveryEnvFingerprint() string {
	parts := make([]string, 0, len(openclawDiscoveryEnvVars))
	for _, key := range openclawDiscoveryEnvVars {
		parts = append(parts, key+"="+os.Getenv(key))
	}
	return strings.Join(parts, "\x00")
}

// resolveOpenclawBinPath expands a bare command name through PATH so the
// fingerprint records the binary that actually ran, not the string "openclaw".
// A lookup failure returns the input unchanged; the stat below then fails and
// the caller takes the miss path.
func resolveOpenclawBinPath(bin string) string {
	if bin == "" {
		bin = "openclaw"
	}
	if strings.ContainsRune(bin, filepath.Separator) {
		return bin
	}
	if resolved, err := exec.LookPath(bin); err == nil {
		return resolved
	}
	return bin
}

// openclawProfileCacheDir resolves the directory this daemon profile shares
// across tasks (~/.multica, or ~/.multica/profiles/<name>). A resolution
// failure only disables caching — preparation still works, it just pays the
// CLI cost every time.
func openclawProfileCacheDir(profile string, logger *slog.Logger) string {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		if logger != nil {
			logger.Warn("execenv: openclaw discovery cache disabled; could not resolve profile dir",
				"profile", profile, "error", err)
		}
		return ""
	}
	return dir
}

// openclawDiscoveryCachePath returns the cache file inside dir, or "" when
// caching is disabled (no directory configured).
func openclawDiscoveryCachePath(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, openclawDiscoveryCacheFile)
}

// buildOpenclawDiscoveryFingerprint captures the current evidence for bin and
// configPath. It returns an error when either file cannot be stat'ed, which
// callers treat as "do not cache / do not trust the cache" rather than as a
// task failure.
func buildOpenclawDiscoveryFingerprint(bin, configPath string) (openclawDiscoveryFingerprint, error) {
	binPath := resolveOpenclawBinPath(bin)
	binInfo, err := os.Stat(binPath)
	if err != nil {
		return openclawDiscoveryFingerprint{}, fmt.Errorf("stat openclaw binary: %w", err)
	}
	cfgInfo, err := os.Stat(configPath)
	if err != nil {
		return openclawDiscoveryFingerprint{}, fmt.Errorf("stat openclaw active config: %w", err)
	}
	return openclawDiscoveryFingerprint{
		Version:           openclawDiscoveryCacheVersion,
		BinPath:           binPath,
		BinSize:           binInfo.Size(),
		BinModTimeNano:    binInfo.ModTime().UnixNano(),
		Env:               openclawDiscoveryEnvFingerprint(),
		ConfigPath:        configPath,
		ConfigSize:        cfgInfo.Size(),
		ConfigModTimeNano: cfgInfo.ModTime().UnixNano(),
	}, nil
}

// loadOpenclawDiscoveryCache returns a cached discovery for bin when one is
// present, structurally valid, still inside the TTL, and fingerprint-equal to
// the current on-disk state. Every other outcome — missing file, unreadable
// file, truncated JSON left by a killed writer, foreign version, moved binary,
// edited config — is a plain miss: the caller re-runs the CLI and stays
// fail-closed. Errors are never propagated, because a bad cache must degrade
// to "slow", never to "task failed".
func loadOpenclawDiscoveryCache(cachePath, bin string, now time.Time) (openclawDiscoveryCacheEntry, bool) {
	if cachePath == "" {
		return openclawDiscoveryCacheEntry{}, false
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return openclawDiscoveryCacheEntry{}, false
	}
	var entry openclawDiscoveryCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return openclawDiscoveryCacheEntry{}, false
	}
	if entry.Fingerprint.Version != openclawDiscoveryCacheVersion {
		return openclawDiscoveryCacheEntry{}, false
	}
	// len 0 means the field was absent or empty — a truncated write, not a
	// stored result. A discovery with no agents is stored as the four bytes
	// "null" and is a legitimate hit.
	if entry.ActiveConfigPath == "" || len(entry.AgentsList) == 0 {
		return openclawDiscoveryCacheEntry{}, false
	}
	if entry.CachedAtNano <= 0 {
		return openclawDiscoveryCacheEntry{}, false
	}
	age := now.Sub(time.Unix(0, entry.CachedAtNano))
	// An age well below zero means the entry was written in the future — a
	// clock jump or a hand-edited file. Treat it as a miss rather than trusting
	// it for a TTL that may never expire. Sub-second future dating is not that:
	// it is the ordinary result of a concurrent writer on this same daemon
	// (see openclawDiscoveryCacheFutureSkew), and rejecting it only buys an
	// unnecessary discovery run.
	if age < -openclawDiscoveryCacheFutureSkew || age > openclawDiscoveryCacheTTL {
		return openclawDiscoveryCacheEntry{}, false
	}
	current, err := buildOpenclawDiscoveryFingerprint(bin, entry.ActiveConfigPath)
	if err != nil {
		return openclawDiscoveryCacheEntry{}, false
	}
	if current != entry.Fingerprint {
		return openclawDiscoveryCacheEntry{}, false
	}
	return entry, true
}

// storeOpenclawDiscoveryCache records a successful discovery. Only complete
// successes reach here: a fresh install with no config on disk, a CLI error,
// and a timeout all skip the write, so a transient failure can never be
// replayed from cache for the rest of the TTL.
//
// A nil agentsList is a success, not an omission: openclawResolvedAgentsList
// returns (nil, false, nil) for a config whose agents.list is null and for an
// empty 2026.6+ registry. Treating that as "incomplete" would leave exactly
// those users paying full discovery on every message, so it is stored as JSON
// null and decodes back to nil.
//
// The write is atomic (temp file + rename) so a concurrently reading task sees
// either the old entry or the new one, never a half-written file. Two tasks
// racing to store simply leave the later winner in place — both entries are
// equally valid.
func storeOpenclawDiscoveryCache(cachePath, bin, activeConfigPath string, agentsList []any, agentsFromRegistry bool, now time.Time) error {
	if cachePath == "" {
		return nil
	}
	if activeConfigPath == "" {
		return nil
	}
	fingerprint, err := buildOpenclawDiscoveryFingerprint(bin, activeConfigPath)
	if err != nil {
		return err
	}
	listBytes, err := json.Marshal(agentsList)
	if err != nil {
		return fmt.Errorf("marshal openclaw agents list: %w", err)
	}
	entry := openclawDiscoveryCacheEntry{
		Fingerprint:        fingerprint,
		CachedAtNano:       now.UnixNano(),
		ActiveConfigPath:   activeConfigPath,
		AgentsList:         listBytes,
		AgentsFromRegistry: agentsFromRegistry,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal openclaw discovery cache: %w", err)
	}
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create openclaw discovery cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, openclawDiscoveryCacheFile+".*")
	if err != nil {
		return fmt.Errorf("create openclaw discovery cache temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup of the temp file when the rename never happened.
		_ = os.Remove(tmpName)
	}()
	// 0o600 — the entry holds config paths and the user's agent ids, which is
	// the same posture the per-task wrapper already uses.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod openclaw discovery cache temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write openclaw discovery cache temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close openclaw discovery cache temp: %w", err)
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		return fmt.Errorf("commit openclaw discovery cache: %w", err)
	}
	return nil
}

// decodeOpenclawCachedAgentsList converts a cached agents.list payload back
// into the []any shape prepareOpenclawConfig works with.
// A stored JSON null decodes to a nil slice, which is exactly the shape
// openclawResolvedAgentsList produces for "no agents", so a hit and a live run
// build the same wrapper.
func decodeOpenclawCachedAgentsList(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty cached agents list")
	}
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}
