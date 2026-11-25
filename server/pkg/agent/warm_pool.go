package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultWarmPoolIdleTTL bounds how long an idle pooled provider process
	// stays alive between turns before being gracefully closed and reaped.
	DefaultWarmPoolIdleTTL = 15 * time.Minute
	// DefaultWarmPoolMaxIdle caps the number of idle provider processes held
	// across all conversations to bound host memory usage.
	DefaultWarmPoolMaxIdle = 8

	warmPoolEnvEnabled = "MULTICA_WARM_POOL"
	warmPoolEnvIdleTTL = "MULTICA_WARM_POOL_IDLE_TTL"
	warmPoolEnvMaxIdle = "MULTICA_WARM_POOL_MAX_IDLE"
)

type warmPoolKey struct {
	provider  string
	sessionID string
}

type warmPoolProcess interface {
	Fingerprint() string
	Alive() bool
	Close() bool
}

type warmPoolEntry struct {
	key      warmPoolKey
	proc     warmPoolProcess
	parkedAt time.Time
	timer    *time.Timer
}

// WarmSessionPoolConfig configures a WarmSessionPool.
type WarmSessionPoolConfig struct {
	IdleTTL time.Duration
	MaxIdle int
	Logger  *slog.Logger
}

// WarmSessionPool keeps recently completed Codex app-server and Claude Code
// processes warm across turns of the same conversation so follow-up turns avoid
// process spawn, model catalog refresh, MCP initialization, and cold WebSocket
// handshakes.
//
// Security & isolation invariants:
//   - Entries are keyed by (provider, sessionID) and verified against a SHA-256
//     fingerprint of every session-immutable input (executable, launch args,
//     cwd, model, thinking level, service tier, MCP config, settings/config
//     file contents, runtime brief hash, and all non-volatile env vars including
//     workspace/agent identity). Any mismatch evicts and closes the stale
//     process synchronously before spawning fresh.
//   - Volatile per-task credentials (MULTICA_TOKEN, MULTICA_TASK_ID, etc.) are
//     never stored in the pooled process's static environment; they travel via a
//     0o600 per-session overlay file that is atomically cleared before parking.
//   - Unhealthy, cancelled, timed-out, or errored turns are never parked.
type WarmSessionPool struct {
	idleTTL time.Duration
	maxIdle int
	logger  *slog.Logger

	mu      sync.Mutex
	closed  bool
	entries map[warmPoolKey]*warmPoolEntry
}

// NewWarmSessionPool creates a new pool with bounded idle TTL and capacity.
func NewWarmSessionPool(cfg WarmSessionPoolConfig) *WarmSessionPool {
	idleTTL := cfg.IdleTTL
	if idleTTL <= 0 {
		idleTTL = DefaultWarmPoolIdleTTL
	}
	maxIdle := cfg.MaxIdle
	if maxIdle <= 0 {
		maxIdle = DefaultWarmPoolMaxIdle
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &WarmSessionPool{
		idleTTL: idleTTL,
		maxIdle: maxIdle,
		logger:  logger,
		entries: make(map[warmPoolKey]*warmPoolEntry),
	}
}

// NewWarmSessionPoolFromEnv creates a WarmSessionPool configured from daemon
// environment variables, or returns nil if MULTICA_WARM_POOL explicitly
// disables pooling ("0", "false", "off", "no").
func NewWarmSessionPoolFromEnv(logger *slog.Logger) *WarmSessionPool {
	if raw := strings.TrimSpace(strings.ToLower(os.Getenv(warmPoolEnvEnabled))); raw != "" {
		switch raw {
		case "0", "false", "off", "no":
			if logger != nil {
				logger.Info("warm session pool disabled by environment", "env", warmPoolEnvEnabled, "value", raw)
			}
			return nil
		}
	}
	cfg := WarmSessionPoolConfig{
		IdleTTL: DefaultWarmPoolIdleTTL,
		MaxIdle: DefaultWarmPoolMaxIdle,
		Logger:  logger,
	}
	if raw := strings.TrimSpace(os.Getenv(warmPoolEnvIdleTTL)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.IdleTTL = d
		}
	}
	if raw := strings.TrimSpace(os.Getenv(warmPoolEnvMaxIdle)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.MaxIdle = n
		}
	}
	return NewWarmSessionPool(cfg)
}

// Acquire checks out a parked process for (provider, sessionID) if its
// fingerprint matches and it is still alive. If a parked entry exists with a
// mismatched fingerprint or dead process, it is evicted and closed before
// returning nil so the caller can spawn a fresh session cleanly.
func (p *WarmSessionPool) Acquire(provider, sessionID, fingerprint string) warmPoolProcess {
	if p == nil || provider == "" || sessionID == "" {
		return nil
	}
	key := warmPoolKey{provider: provider, sessionID: sessionID}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	entry, ok := p.entries[key]
	if !ok {
		p.mu.Unlock()
		return nil
	}
	delete(p.entries, key)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	p.mu.Unlock()

	if !entry.proc.Alive() {
		p.logger.Debug("warm pool: evicted dead process on acquire", "provider", provider, "session_id", sessionID)
		entry.proc.Close()
		return nil
	}
	if entry.proc.Fingerprint() != fingerprint {
		p.logger.Info("warm pool: config changed; evicting stale process", "provider", provider, "session_id", sessionID)
		entry.proc.Close()
		return nil
	}
	p.logger.Info("warm pool: session hit", "provider", provider, "session_id", sessionID)
	return entry.proc
}

// Park stores a healthy idle process in the pool under (provider, sessionID).
// Returns false (and closes proc) if the pool is closed, proc is dead, or
// sessionID is empty.
func (p *WarmSessionPool) Park(provider, sessionID string, proc warmPoolProcess) bool {
	if proc == nil {
		return false
	}
	if p == nil || provider == "" || sessionID == "" || !proc.Alive() {
		proc.Close()
		return false
	}
	key := warmPoolKey{provider: provider, sessionID: sessionID}

	var toClose []warmPoolProcess

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		proc.Close()
		return false
	}
	if existing, ok := p.entries[key]; ok {
		delete(p.entries, key)
		if existing.timer != nil {
			existing.timer.Stop()
		}
		toClose = append(toClose, existing.proc)
	}
	for len(p.entries) >= p.maxIdle {
		var oldestKey warmPoolKey
		var oldest *warmPoolEntry
		for k, e := range p.entries {
			if oldest == nil || e.parkedAt.Before(oldest.parkedAt) {
				oldestKey = k
				oldest = e
			}
		}
		if oldest == nil {
			break
		}
		delete(p.entries, oldestKey)
		if oldest.timer != nil {
			oldest.timer.Stop()
		}
		toClose = append(toClose, oldest.proc)
	}

	entry := &warmPoolEntry{
		key:      key,
		proc:     proc,
		parkedAt: time.Now(),
	}
	entry.timer = time.AfterFunc(p.idleTTL, func() {
		p.evictExact(key, entry)
	})
	p.entries[key] = entry
	p.mu.Unlock()

	for _, stale := range toClose {
		stale.Close()
	}
	p.logger.Info("warm pool: parked session", "provider", provider, "session_id", sessionID, "idle_ttl", p.idleTTL.String())
	return true
}

func (p *WarmSessionPool) evictExact(key warmPoolKey, target *warmPoolEntry) {
	p.mu.Lock()
	current, ok := p.entries[key]
	if !ok || current != target {
		p.mu.Unlock()
		return
	}
	delete(p.entries, key)
	p.mu.Unlock()

	p.logger.Info("warm pool: evicting idle session", "provider", key.provider, "session_id", key.sessionID)
	target.proc.Close()
}

// Evict removes and closes any parked process for (provider, sessionID).
func (p *WarmSessionPool) Evict(provider, sessionID string) {
	if p == nil || provider == "" || sessionID == "" {
		return
	}
	key := warmPoolKey{provider: provider, sessionID: sessionID}
	p.mu.Lock()
	entry, ok := p.entries[key]
	if ok {
		delete(p.entries, key)
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	p.mu.Unlock()
	if ok {
		entry.proc.Close()
	}
}

// Len returns the number of currently parked idle sessions.
func (p *WarmSessionPool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Close closes the pool and terminates/reaps every parked process in parallel.
// Returns true iff every parked process confirmed clean termination.
func (p *WarmSessionPool) Close() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return true
	}
	p.closed = true
	entries := make([]*warmPoolEntry, 0, len(p.entries))
	for k, e := range p.entries {
		delete(p.entries, k)
		if e.timer != nil {
			e.timer.Stop()
		}
		entries = append(entries, e)
	}
	p.mu.Unlock()

	if len(entries) == 0 {
		return true
	}
	p.logger.Info("warm pool: closing idle sessions", "count", len(entries))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		allClean = true
	)
	for _, entry := range entries {
		wg.Add(1)
		go func(e *warmPoolEntry) {
			defer wg.Done()
			if !e.proc.Close() {
				mu.Lock()
				allClean = false
				mu.Unlock()
			}
		}(entry)
	}
	wg.Wait()
	return allClean
}

// WarmSessionFingerprintInput captures every session-immutable input that
// governs whether a parked process can safely serve the next turn.
type WarmSessionFingerprintInput struct {
	Provider      string
	Executable    string
	LaunchPrefix  []string
	Args          []string
	Cwd           string
	Model         string
	ThinkingLevel string
	ServiceTier   string
	McpConfig     json.RawMessage
	ConfigFiles   []string // optional file paths whose bytes are hashed (e.g. ClaudeSettingsPath, CODEX_HOME/config.toml)
	BriefHash     string
	Env           map[string]string
	ExtraFlags    []string
}

// HashWarmBrief returns a hex SHA-256 digest of a runtime brief string.
func HashWarmBrief(brief string) string {
	if brief == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(brief))
	return hex.EncodeToString(sum[:])
}

func computeWarmSessionFingerprint(in WarmSessionFingerprintInput) string {
	h := sha256.New()
	writeField := func(tag, val string) {
		_, _ = h.Write([]byte(tag))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(val))
		_, _ = h.Write([]byte{0})
	}
	writeSlice := func(tag string, vals []string) {
		_, _ = h.Write([]byte(tag))
		_, _ = h.Write([]byte{0})
		for _, v := range vals {
			_, _ = h.Write([]byte(v))
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte{1})
	}

	writeField("provider", in.Provider)
	writeField("exec", in.Executable)
	writeSlice("prefix", in.LaunchPrefix)
	writeSlice("args", in.Args)
	writeField("cwd", in.Cwd)
	writeField("model", in.Model)
	writeField("thinking", in.ThinkingLevel)
	writeField("tier", in.ServiceTier)
	writeField("mcp", strings.TrimSpace(string(in.McpConfig)))
	writeField("brief", in.BriefHash)
	writeSlice("extra", in.ExtraFlags)

	for _, path := range in.ConfigFiles {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			writeField("cfgfile:"+path, "ERR:"+err.Error())
		} else {
			sum := sha256.Sum256(data)
			writeField("cfgfile:"+path, hex.EncodeToString(sum[:]))
		}
	}

	// Hash all non-volatile environment variables (excluding temp dirs and the
	// dynamic overlay path itself) so workspace, agent, CODEX_HOME, custom_env,
	// proxy URLs, and auth overrides all participate in isolation.
	keys := make([]string, 0, len(in.Env))
	for k := range in.Env {
		if isWarmPoolIgnoredEnvKey(k) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeField("env:"+k, in.Env[k])
	}

	return hex.EncodeToString(h.Sum(nil))
}

func isWarmPoolIgnoredEnvKey(key string) bool {
	if IsTaskEnvVolatileKey(key) || key == TaskEnvFileEnv {
		return true
	}
	upper := key
	if runtime.GOOS == "windows" {
		upper = strings.ToUpper(key)
	}
	switch upper {
	case "TMPDIR", "TMP", "TEMP", "CLAUDE_CODE_TMPDIR":
		return true
	default:
		return false
	}
}
