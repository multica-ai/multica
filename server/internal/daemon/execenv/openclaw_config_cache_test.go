package execenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// openclawCacheFixture is one host: a real (fake) openclaw binary file, a real
// user config file, a cache directory, and a stubbed CLI that counts calls.
// The binary and config have to exist on disk because the cache fingerprint
// stats both — that is the whole invalidation mechanism.
type openclawCacheFixture struct {
	bin        string
	configPath string
	cacheDir   string
	stub       *openclawCLIStub
}

func newOpenclawCacheFixture(t *testing.T) *openclawCacheFixture {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake openclaw binary: %v", err)
	}
	configPath := filepath.Join(dir, "openclaw.json")
	if err := os.WriteFile(configPath, []byte(`{ "agents": { "list": [] } }`), 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file":                   {stdout: configPath + "\n"},
		"config get agents.list --json": {stdout: `[{"id":"scout"}]`},
	})
	stub.bin = bin
	return &openclawCacheFixture{
		bin:        bin,
		configPath: configPath,
		cacheDir:   t.TempDir(),
		stub:       stub,
	}
}

func (f *openclawCacheFixture) prep() OpenclawConfigPrep {
	return OpenclawConfigPrep{OpenclawBin: f.bin, CacheDir: f.cacheDir}
}

// run executes one full preparation into a throwaway env root and returns how
// many CLI invocations it needed.
func (f *openclawCacheFixture) run(t *testing.T) int {
	t.Helper()
	before := len(f.stub.calls)
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	if _, err := prepareOpenclawConfig(envRoot, workDir, f.prep()); err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	return len(f.stub.calls) - before
}

func (f *openclawCacheFixture) cachePath() string {
	return filepath.Join(f.cacheDir, openclawDiscoveryCacheFile)
}

// TestOpenclawDiscoveryCacheServesSecondPreparation is the headline behaviour.
// Discovery is two serial CLI calls that cost ~12.7s on the host in #7112, and
// every task — including every chat message, which goes through Reuse — used
// to pay them again. Raising the deadline without this would trade a hard
// failure for a per-message stall.
func TestOpenclawDiscoveryCacheServesSecondPreparation(t *testing.T) {
	f := newOpenclawCacheFixture(t)

	if got := f.run(t); got != 2 {
		t.Fatalf("cold preparation made %d CLI calls, want 2 (config file + agents.list)", got)
	}
	if got := f.run(t); got != 0 {
		t.Errorf("warm preparation made %d CLI calls, want 0 (served from cache)", got)
	}
}

// TestOpenclawDiscoveryCacheHitProducesSameWrapper guards the actual contract:
// a cache hit has to produce the byte-identical wrapper a live run would, or
// the speedup silently changes what OpenClaw loads.
func TestOpenclawDiscoveryCacheHitProducesSameWrapper(t *testing.T) {
	f := newOpenclawCacheFixture(t)

	read := func() []byte {
		t.Helper()
		envRoot := t.TempDir()
		workDir := filepath.Join(envRoot, "workdir")
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			t.Fatalf("mkdir workdir: %v", err)
		}
		result, err := prepareOpenclawConfig(envRoot, workDir, f.prep())
		if err != nil {
			t.Fatalf("prepareOpenclawConfig: %v", err)
		}
		raw, err := os.ReadFile(result.ConfigPath)
		if err != nil {
			t.Fatalf("read wrapper: %v", err)
		}
		// The workspace override is env-root specific; normalise it away so the
		// comparison is about the discovered content, not the temp path.
		return []byte(strings.ReplaceAll(string(raw), workDir, "<WORKDIR>"))
	}

	cold := read()
	warm := read()
	if string(cold) != string(warm) {
		t.Errorf("cache hit produced a different wrapper\ncold: %s\nwarm: %s", cold, warm)
	}
}

// TestOpenclawDiscoveryCacheInvalidatesOnConfigChange covers the refresh
// semantics Reuse already promised: a user who adds an agent or rotates a
// provider must not keep getting the stale list for the rest of the TTL.
func TestOpenclawDiscoveryCacheInvalidatesOnConfigChange(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.run(t)

	// Rewrite the active config with different content. Size changes, so this
	// is caught even on filesystems with coarse mtime granularity.
	if err := os.WriteFile(f.configPath, []byte(`{ "agents": { "list": [ { "id": "scout" } ] } }`), 0o600); err != nil {
		t.Fatalf("rewrite user config: %v", err)
	}

	if got := f.run(t); got != 2 {
		t.Errorf("preparation after a config edit made %d CLI calls, want 2 (cache must be invalid)", got)
	}
}

// TestOpenclawDiscoveryCacheInvalidatesOnBinaryChange covers an openclaw
// upgrade in place: same path, new build, potentially a different active
// config or agents schema.
func TestOpenclawDiscoveryCacheInvalidatesOnBinaryChange(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.run(t)

	if err := os.WriteFile(f.bin, []byte("#!/bin/sh\n# upgraded\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("rewrite fake binary: %v", err)
	}

	if got := f.run(t); got != 2 {
		t.Errorf("preparation after a binary upgrade made %d CLI calls, want 2 (cache must be invalid)", got)
	}
}

// TestOpenclawDiscoveryCacheInvalidatesOnEnvChange covers the pointers that
// move the active config without touching any file we stat: OpenClaw's own
// path environment.
func TestOpenclawDiscoveryCacheInvalidatesOnEnvChange(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.run(t)

	t.Setenv("OPENCLAW_STATE_DIR", filepath.Join(t.TempDir(), "state"))

	if got := f.run(t); got != 2 {
		t.Errorf("preparation after an OpenClaw env change made %d CLI calls, want 2 (cache must be invalid)", got)
	}
}

// TestOpenclawDiscoveryCacheExpiresWithTTL covers the drift the fingerprint
// cannot see: a nested `$include` target edited in place, or an agent added
// through OpenClaw's own registry. Neither touches the top-level config, so
// the TTL is the only backstop.
func TestOpenclawDiscoveryCacheExpiresWithTTL(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.run(t)

	raw, err := os.ReadFile(f.cachePath())
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var entry openclawDiscoveryCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	entry.CachedAtNano = time.Now().Add(-openclawDiscoveryCacheTTL - time.Second).UnixNano()
	aged, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("encode aged cache: %v", err)
	}
	if err := os.WriteFile(f.cachePath(), aged, 0o600); err != nil {
		t.Fatalf("write aged cache: %v", err)
	}

	if got := f.run(t); got != 2 {
		t.Errorf("preparation past the TTL made %d CLI calls, want 2 (entry must expire)", got)
	}
}

// TestOpenclawDiscoveryCacheRejectsFutureEntry keeps a clock jump (or a
// hand-edited file) from pinning an entry that never ages out.
func TestOpenclawDiscoveryCacheRejectsFutureEntry(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.run(t)

	raw, err := os.ReadFile(f.cachePath())
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var entry openclawDiscoveryCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	entry.CachedAtNano = time.Now().Add(time.Hour).UnixNano()
	future, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("encode future cache: %v", err)
	}
	if err := os.WriteFile(f.cachePath(), future, 0o600); err != nil {
		t.Fatalf("write future cache: %v", err)
	}

	if got := f.run(t); got != 2 {
		t.Errorf("preparation with a future-dated entry made %d CLI calls, want 2", got)
	}
}

// TestOpenclawDiscoveryCacheIgnoresCorruptEntry covers the file a killed
// writer or a full disk can leave behind. A bad cache must degrade to slow,
// never to a failed task.
func TestOpenclawDiscoveryCacheIgnoresCorruptEntry(t *testing.T) {
	for name, body := range map[string]string{
		"truncated json": `{"fingerprint":{"version":1,"bin_pa`,
		"empty file":     ``,
		"wrong shape":    `["not", "an", "entry"]`,
		"foreign version": `{"fingerprint":{"version":999},"cached_at_nano":1,` +
			`"active_config_path":"/nope","agents_list":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newOpenclawCacheFixture(t)
			if err := os.WriteFile(f.cachePath(), []byte(body), 0o600); err != nil {
				t.Fatalf("write corrupt cache: %v", err)
			}

			if got := f.run(t); got != 2 {
				t.Errorf("preparation with a corrupt cache made %d CLI calls, want 2 (must fall back to the CLI)", got)
			}
			// And the bad entry must be replaced, not left to poison every
			// later task with the same fallback cost.
			if got := f.run(t); got != 0 {
				t.Errorf("preparation after recovery made %d CLI calls, want 0", got)
			}
		})
	}
}

// TestOpenclawDiscoveryCacheSkipsFreshInstall pins the one state that must
// never be cached: "the user has no config yet" flips the moment they run
// OpenClaw's setup, and a cached miss would keep the next tasks running
// without their models and auth profiles.
func TestOpenclawDiscoveryCacheSkipsFreshInstall(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	missingPath := filepath.Join(t.TempDir(), "openclaw.json")
	f.stub.responses["config file"] = openclawResponse{stdout: missingPath + "\n"}

	f.run(t)

	if _, err := os.Stat(f.cachePath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("fresh install wrote a cache entry (stat err = %v); it must stay uncached", err)
	}
}

// TestOpenclawDiscoveryCacheSkipsFailedDiscovery keeps a transient CLI failure
// from being replayed out of cache — including the timeout this change exists
// for, which would otherwise be served instantly for the rest of the TTL.
func TestOpenclawDiscoveryCacheSkipsFailedDiscovery(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.stub.responses["config get agents.list --json"] = openclawResponse{err: errors.New("boom")}

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	if _, err := prepareOpenclawConfig(envRoot, workDir, f.prep()); err == nil {
		t.Fatal("expected the agents.list failure to fail preparation")
	}

	if _, err := os.Stat(f.cachePath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed discovery wrote a cache entry (stat err = %v); failures must stay uncached", err)
	}
}

// TestOpenclawDiscoveryCacheFutureDatedEntry pins both edges of the
// future-dating rule.
//
// Callers sample time.Now() once and pass it down, so on a daemon running
// several tasks at once a peer routinely commits an entry stamped after the
// reader's sample. That entry is completely valid — rejecting it just costs an
// extra discovery run, and it is what made the concurrency test below flake.
// A real clock jump still has to be caught, since an entry dated far ahead
// would otherwise never age out of its TTL.
func TestOpenclawDiscoveryCacheFutureDatedEntry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ahead time.Duration
		want  bool
	}{
		{"concurrent writer, microseconds ahead", 500 * time.Microsecond, true},
		{"just inside the tolerance", openclawDiscoveryCacheFutureSkew - time.Millisecond, true},
		{"clock jump, well past the tolerance", openclawDiscoveryCacheFutureSkew + time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOpenclawCacheFixture(t)
			readerNow := time.Now()
			if err := storeOpenclawDiscoveryCache(f.cachePath(), f.bin, f.configPath, []any{map[string]any{"id": "scout"}}, false, readerNow.Add(tc.ahead)); err != nil {
				t.Fatalf("store: %v", err)
			}
			if _, ok := loadOpenclawDiscoveryCache(f.cachePath(), f.bin, readerNow); ok != tc.want {
				t.Fatalf("entry dated %v ahead of the reader: hit = %t, want %t", tc.ahead, ok, tc.want)
			}
		})
	}
}

// TestOpenclawDiscoveryCacheConcurrentPreparations covers several tasks
// starting at once on the same daemon: the writes are atomic, so every task
// either sees a complete old entry or a complete new one, and none of them
// fails.
func TestOpenclawDiscoveryCacheConcurrentPreparations(t *testing.T) {
	f := newOpenclawCacheFixture(t)

	const workers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			envRoot := t.TempDir()
			workDir := filepath.Join(envRoot, "workdir")
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
				return
			}
			// Each worker gets its own stub-free path into discovery: the
			// shared stub is not goroutine-safe, so drive the cache directly
			// with the same store/load pair preparation uses.
			if err := storeOpenclawDiscoveryCache(f.cachePath(), f.bin, f.configPath, []any{map[string]any{"id": "scout"}}, false, time.Now()); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
				return
			}
			if _, ok := loadOpenclawDiscoveryCache(f.cachePath(), f.bin, time.Now()); !ok {
				mu.Lock()
				failures = append(failures, errors.New("cache load failed under concurrency"))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for _, err := range failures {
		t.Errorf("concurrent cache use: %v", err)
	}

	// No half-written temp files may survive a clean run.
	entries, err := os.ReadDir(f.cacheDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != openclawDiscoveryCacheFile {
			t.Errorf("leftover file in cache dir: %s", entry.Name())
		}
	}
}

// TestOpenclawDiscoveryCacheDisabledWithoutDir pins the opt-out: with no cache
// directory (profile resolution failed), preparation still works and simply
// pays the CLI every time.
func TestOpenclawDiscoveryCacheDisabledWithoutDir(t *testing.T) {
	f := newOpenclawCacheFixture(t)
	f.cacheDir = ""

	if got := f.run(t); got != 2 {
		t.Fatalf("first preparation made %d CLI calls, want 2", got)
	}
	if got := f.run(t); got != 2 {
		t.Errorf("second preparation made %d CLI calls, want 2 (caching must stay off)", got)
	}
}

// TestOpenclawDiscoveryCacheStoresEmptyAgentsList covers Elon's must-fix 2.
// openclawResolvedAgentsList returns (nil, false, nil) for two legitimate
// successes — a config whose agents.list is null, and an empty 2026.6+
// registry. Treating a nil list as "incomplete, do not cache" left exactly
// those users paying full discovery on every message, which is the cost this
// change exists to remove.
func TestOpenclawDiscoveryCacheStoresEmptyAgentsList(t *testing.T) {
	cases := map[string]map[string]openclawResponse{
		"config agents.list is null": {
			"config get agents.list --json": {stdout: "null"},
		},
		"registry is empty": {
			"config get agents.list --json": {err: errors.New("Config path not found: agents.list")},
			"agents list --json":            {stdout: "[]"},
		},
	}
	for name, responses := range cases {
		t.Run(name, func(t *testing.T) {
			f := newOpenclawCacheFixture(t)
			for args, resp := range responses {
				f.stub.responses[args] = resp
			}

			cold := f.run(t)
			if cold == 0 {
				t.Fatalf("cold preparation made no CLI calls, want a live discovery")
			}
			if got := f.run(t); got != 0 {
				t.Errorf("warm preparation made %d CLI calls, want 0: an agent-less host must be cacheable too", got)
			}
		})
	}
}

// TestOpenclawDiscoveryCacheInvalidatesOnLegacyEnvChange covers must-fix 3.
// openclawFallbackConfigCandidates still honours the CLAWDBOT_* names, so a
// user who repoints those and restarts the daemon must not keep getting the
// pre-switch config served out of a cache that never looked at them.
func TestOpenclawDiscoveryCacheInvalidatesOnLegacyEnvChange(t *testing.T) {
	for _, envVar := range []string{"CLAWDBOT_CONFIG_PATH", "CLAWDBOT_STATE_DIR"} {
		t.Run(envVar, func(t *testing.T) {
			f := newOpenclawCacheFixture(t)
			f.run(t)

			t.Setenv(envVar, filepath.Join(t.TempDir(), "legacy"))

			if got := f.run(t); got != 2 {
				t.Errorf("preparation after %s changed made %d CLI calls, want 2 (cache must be invalid)", envVar, got)
			}
		})
	}
}
