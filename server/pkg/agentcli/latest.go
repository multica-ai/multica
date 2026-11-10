package agentcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// npmPackages is the install table for one-click upgrade. A managed CLI is
// installable only when it has a row here. Names absent from the table are
// still reported by status, with latest empty and upgradable false.
var npmPackages = map[string]string{
	CLICodex:  "@openai/codex",
	CLIClaude: "@anthropic-ai/claude-code",
}

// DefaultNPMRegistries is the official npm registry. Callers may prepend
// another registry for tests; production lookups stay on registry.npmjs.org.
var DefaultNPMRegistries = []string{
	"https://registry.npmjs.org",
}

// npmVersionPattern is the version field of a registry /latest document.
// Prerelease and build metadata are accepted; comparison uses the numeric
// triple only (agent.CompareVersions).
var npmVersionPattern = regexp.MustCompile(`^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:[-+][0-9A-Za-z.-]+)?$`)

// NPMPackage returns the registry package for cli. ok is false when the
// install table has no package for that name.
func NPMPackage(cli string) (pkg string, ok bool) {
	pkg, ok = npmPackages[strings.ToLower(strings.TrimSpace(cli))]
	return pkg, ok
}

// LatestLookup caches published versions. A nil lookup reports every CLI
// unknown. Successes are cached longer than failures so a registry blip
// does not stick, and a refresh does not hit the network on every GET.
type LatestLookup struct {
	Client     *http.Client
	Registries []string
	Now        func() time.Time
	SuccessTTL time.Duration
	FailureTTL time.Duration

	mu    sync.Mutex
	items map[string]latestCacheItem
}

type latestCacheItem struct {
	version string
	found   bool
	expires time.Time
}

// NewLatestLookup builds a lookup. A nil client gets a 2s timeout. An empty
// registry list uses DefaultNPMRegistries.
func NewLatestLookup(client *http.Client) *LatestLookup {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &LatestLookup{
		Client:     client,
		Registries: append([]string(nil), DefaultNPMRegistries...),
		Now:        time.Now,
		SuccessTTL: time.Hour,
		FailureTTL: time.Minute,
		items:      map[string]latestCacheItem{},
	}
}

var (
	defaultLatestOnce sync.Once
	defaultLatest     *LatestLookup
)

// DefaultLatestLookup is the process-wide cache used when a handler has no
// lookup of its own.
func DefaultLatestLookup() *LatestLookup {
	defaultLatestOnce.Do(func() {
		defaultLatest = NewLatestLookup(nil)
	})
	return defaultLatest
}

// Version returns the published version for cli. ok is false when cli has no
// registry package or every registry lookup failed.
func (l *LatestLookup) Version(ctx context.Context, cli string) (string, bool) {
	if l == nil {
		return "", false
	}
	cli = strings.ToLower(strings.TrimSpace(cli))
	pkg, ok := NPMPackage(cli)
	if !ok {
		return "", false
	}
	if item, hit := l.cached(cli); hit {
		return item.version, item.found
	}
	version, found := l.fetch(ctx, pkg)
	l.remember(cli, version, found)
	return version, found
}

func (l *LatestLookup) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *LatestLookup) cached(cli string) (latestCacheItem, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, ok := l.items[cli]
	if !ok || !l.now().Before(item.expires) {
		return latestCacheItem{}, false
	}
	return item, true
}

func (l *LatestLookup) remember(cli, version string, found bool) {
	ttl := l.FailureTTL
	if found {
		ttl = l.SuccessTTL
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.items == nil {
		l.items = map[string]latestCacheItem{}
	}
	l.items[cli] = latestCacheItem{
		version: version,
		found:   found,
		expires: l.now().Add(ttl),
	}
}

func (l *LatestLookup) fetch(ctx context.Context, pkg string) (string, bool) {
	registries := l.Registries
	if len(registries) == 0 {
		registries = DefaultNPMRegistries
	}
	for _, registry := range registries {
		if ctx.Err() != nil {
			return "", false
		}
		version, err := l.fetchOne(ctx, registry, pkg)
		if err == nil && npmVersionPattern.MatchString(version) {
			return version, true
		}
	}
	return "", false
}

func (l *LatestLookup) fetchOne(ctx context.Context, registry, pkg string) (string, error) {
	endpoint := npmLatestURL(registry, pkg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "multica-agent-cli-status")
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm latest %s: status %d", pkg, resp.StatusCode)
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	version := strings.TrimSpace(doc.Version)
	if !npmVersionPattern.MatchString(version) {
		return "", fmt.Errorf("npm latest %s: version %q", pkg, version)
	}
	return version, nil
}

// npmLatestURL is {registry}/{package}/latest with only the slash inside a
// scoped name escaped. url.PathEscape would also escape '@', which the npm
// registry does not accept.
func npmLatestURL(registry, pkg string) string {
	registry = strings.TrimRight(strings.TrimSpace(registry), "/")
	escaped := strings.ReplaceAll(strings.TrimSpace(pkg), "/", "%2F")
	return registry + "/" + escaped + "/latest"
}
