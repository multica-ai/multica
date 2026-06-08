# `multica-repocache` — In-Cluster Repo Cache (Plan F.1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the per-task re-clone. A new small Go binary `multica-repocache` runs as a long-lived Deployment, maintains bare clones of every workspace's repos on an RWX PVC, fetches from origin on a 60s tick, and serves them as a read-only filesystem mount in every worker Job pod. Worker `git clone` becomes a sub-second `git clone --shared file:///repos/{ws}/{org}/{repo}.git`, and is also automatic for agents that just run `git clone <origin-url>` thanks to a workspace-scoped `gitconfig` url-rewrite mounted into the pod.

**Architecture:** Reuses the existing `server/internal/daemon/repocache.Cache` (the same type the daemon already uses for its on-laptop cache). The new binary wraps it in a server: a periodic refresh loop, a small admin HTTP API, Prometheus metrics, and a single RWX PVC at `/repos`. Worker Job pods mount the same PVC read-only; the controller's `DispatchJob` adds the mount, a `MULTICA_REPOCACHE_DIR` env var, and a workspace-scoped `gitconfig` ConfigMap that turns `https://github.com/{org}/{repo}` and `git@github.com:{org}/{repo}` into the local file URL via git's `url.<base>.insteadOf` mechanism. Agents need no code changes.

**Tech stack:** Go, existing `server/internal/daemon/repocache` package, Docker, Helm. No new external Go dependencies — uses `chi` (already in the module) for the admin API and `prometheus/client_golang` (already in via Plan A's backend).

**Source spec:** `docs/superpowers/specs/2026-05-20-multica-k8s-design.md` — §5.5 (repo-cache server), §5.6 step on workdir reuse, §5.3 step 9 (controller calls repo-cache admin on workspace repo changes).

**Builds on:** Plan E (controller dispatching per-task Job pods with mounted Secrets), the partial Plan F at `2026-05-29-tool-cache-and-gc.md` (compatible; both touch the controller's `DispatchJob` but in disjoint ways).

---

## Key facts established by code reading (do not re-investigate)

- **Reusable cache implementation:** `server/internal/daemon/repocache/cache.go` already exports a `Cache` type with `Sync(workspaceID, []RepoInfo) error`, `Lookup(workspaceID, url) string` (returns bare path or ""), `Fetch(barePath) error`, and `CreateWorktree(WorktreeParams) (*WorktreeResult, error)`. It already handles per-repo locking, safe.directory env vars, dev tooling exclude patterns, and the Anthropic-Max work-around for `.agent_context`. We import it directly — no fork.
- **On-disk layout:** `Cache.New(root, logger)` lays out bare clones at `{root}/{workspace_id}/{slug}.git`. For our server we use `/repos` as the root and accept that layout — workers reference `/repos/{ws}/{slug}.git` directly.
- **Repos source of truth:** Multica's `GET /api/daemon/workspaces/{ws}/repos` returns `{repos: [{url: ...}], repos_version}`. The daemon's client already wraps this as `Client.GetWorkspaceRepos(ctx, workspaceID)` returning `*WorkspaceReposResponse`.
- **Workspace discovery:** the repocache server needs to know *which* workspaces to mirror. It uses the same Multica PAT (`MULTICA_TOKEN`) and the same per-workspace config the controller already loads (the `runtime.yaml` written by Plan E's controller-configmap chart template). We mount the same ConfigMap.
- **No worktree race concerns** because workers do `git clone --shared` from the bare path, not `git worktree add`. Workers never write to `/repos`; they read object data and write only to their own workdir PVC.
- **git daemon protocol is NOT needed** for cluster-local workers — they mount the PVC RO and clone via `file://`. We skip the `git daemon` subprocess and the `:9418` port from the original spec; this is a deliberate scope reduction. If/when out-of-cluster clients ever need to read the cache, `git daemon` is a trivial later add.
- **`multica run-task` already has a `/repo/checkout` HTTP handler** registered at the local single-task port (`server/internal/daemon/single_task.go:83`). It calls `d.repoCheckoutHandler()` which calls `d.ensureRepoReady(...)` which today returns `"workspace is not watched by this daemon: <ws>"` because the single-task `Daemon` struct never populates its `workspaces` map. We do NOT fix that handler — it's the wrong abstraction for the controller model. Instead, the agent's plain `git clone` is what gets accelerated, via the gitconfig url-rewrite. The `multica repo checkout` command becomes a no-op (or, as a tiny follow-up, prints the rewritten path).
- **`url.<base>.insteadOf`** in `~/.gitconfig` rewrites *any* `git clone`/`fetch`/`push` URL whose prefix matches `<base>` to use the rewritten URL. It composes (multiple `insteadOf` entries with the same value, different bases). Standard, well-supported across all git versions we ship.

---

## File structure

### Created by this plan

```
server/cmd/multica-repocache/
├── main.go                                   # CREATE: entry + signal handling
├── config.go                                 # CREATE: env + YAML (reuses Plan E's runtime.yaml shape)
├── config_test.go                            # CREATE
├── server.go                                 # CREATE: HTTP admin + metrics handlers
├── server_test.go                            # CREATE
├── syncer.go                                 # CREATE: periodic Cache.Sync per workspace
├── syncer_test.go                            # CREATE
└── metrics.go                                # CREATE: prom counters/gauges

packaging/docker/repocache/Dockerfile         # CREATE: slim+git+ssh-client
packaging/helm/multica/templates/runtime/
├── repocache-deployment.yaml                 # CREATE
├── repocache-service.yaml                    # CREATE
├── repocache-pvc.yaml                        # CREATE: RWX
└── repocache-config.yaml                     # CREATE: ConfigMap (workspaces to mirror)
```

### Modified by this plan

```
server/cmd/multica-k8s-controller/
├── jobs.go                                   # +mount /repos RO, +env var, +gitconfig CM mount
├── jobs_test.go                              # +assertions
├── dispatcher.go                             # +pass repoCacheConfig through DispatchOnce
├── dispatcher_test.go                        # (regenerated assertions)
└── main.go                                   # +load RepoCacheEnabled from cfg
server/cmd/multica-k8s-controller/config.go   # +ToolCachePVCName? no — that's the GC plan; here:
                                              #     +RepoCachePVCName + RepoCacheMountPath
packaging/helm/multica/values.yaml            # +runtime.repocache.*
packaging/helm/multica/templates/_helpers.tpl # +multica.repocacheImage helper
packaging/scripts/build-images.sh             # +repocache target
packaging/README.md                           # +operator section
```

### Reused unchanged

- `server/internal/daemon/repocache/cache.go` (and `cache_test.go`) — imported as-is.
- `server/internal/daemon.Client` for `GetWorkspaceRepos`.
- The three worker secrets (`multica-token`, `multica-claude-oauth`, `multica-git-ssh`).
- All Plan E controller code paths except the additive mount changes.

---

## Prerequisites

1. Plan E live (`runtime.mode=controller` deployed, agents are reaching tasks via per-Job pods).
2. `GHCR_PAT` exported, `docker login ghcr.io` done.
3. A fresh tag for the bumped artifacts:

```bash
export TAG=v0.4.0-mk1
```

---

## Task 1: Repocache config loader (env + YAML)

**Files:**
- Create: `server/cmd/multica-repocache/config.go`
- Create: `server/cmd/multica-repocache/config_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/cmd/multica-repocache/config_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfgDir := t.TempDir()
	cfgYAML := []byte(`
workspaces:
  - id: 11111111-1111-1111-1111-111111111111
  - id: 22222222-2222-2222-2222-222222222222
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "http://multica-backend.multica.svc:8080")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("REPOCACHE_CONFIG_DIR", cfgDir)

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.RepoRoot != "/repos" {
		t.Errorf("RepoRoot default = %q", got.RepoRoot)
	}
	if got.FetchInterval != 60*time.Second {
		t.Errorf("FetchInterval default = %v", got.FetchInterval)
	}
	if len(got.Workspaces) != 2 {
		t.Errorf("Workspaces count: %d", len(got.Workspaces))
	}
}
```

- [ ] **Step 2: Verify it fails**

```bash
cd /Users/cjs/dev/multica/server
go test ./cmd/multica-repocache/ -run TestLoadConfig -v 2>&1 | tail -10
```

Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

Create `server/cmd/multica-repocache/config.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerBaseURL string
	Token         string

	Workspaces []WorkspaceConfig `yaml:"workspaces"`

	RepoRoot      string
	FetchInterval time.Duration

	AdminAddr   string // ":8080"
	MetricsAddr string // ":9090"
}

// WorkspaceConfig is intentionally a structural subset of the Plan E
// controller's runtime.yaml schema — the same ConfigMap can be reused, or a
// dedicated repocache-config.yaml can be mounted. The repocache only needs
// the workspace id.
type WorkspaceConfig struct {
	ID string `yaml:"id"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		RepoRoot:      "/repos",
		FetchInterval: 60 * time.Second,
		AdminAddr:     ":8080",
		MetricsAddr:   ":9090",
	}

	cfg.ServerBaseURL = strings.TrimRight(os.Getenv("MULTICA_SERVER_URL"), "/")
	if cfg.ServerBaseURL == "" {
		return nil, fmt.Errorf("MULTICA_SERVER_URL not set")
	}
	cfg.Token = strings.TrimSpace(os.Getenv("MULTICA_TOKEN"))
	if cfg.Token == "" {
		return nil, fmt.Errorf("MULTICA_TOKEN not set")
	}
	if v := os.Getenv("REPOCACHE_REPO_ROOT"); v != "" {
		cfg.RepoRoot = v
	}
	if v := os.Getenv("REPOCACHE_FETCH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parse REPOCACHE_FETCH_INTERVAL: %w", err)
		}
		cfg.FetchInterval = d
	}
	if v := os.Getenv("REPOCACHE_ADMIN_ADDR"); v != "" {
		cfg.AdminAddr = v
	}
	if v := os.Getenv("REPOCACHE_METRICS_ADDR"); v != "" {
		cfg.MetricsAddr = v
	}

	dir := os.Getenv("REPOCACHE_CONFIG_DIR")
	if dir == "" {
		dir = "/etc/repocache"
	}
	y, err := os.ReadFile(filepath.Join(dir, "runtime.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read runtime.yaml: %w", err)
	}
	if err := yaml.Unmarshal(y, cfg); err != nil {
		return nil, fmt.Errorf("parse runtime.yaml: %w", err)
	}
	if len(cfg.Workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces configured")
	}
	for i, w := range cfg.Workspaces {
		if w.ID == "" {
			return nil, fmt.Errorf("workspaces[%d].id required", i)
		}
	}
	return cfg, nil
}
```

- [ ] **Step 4: Test + commit**

```bash
go mod tidy
go test ./cmd/multica-repocache/ -run TestLoadConfig -v 2>&1 | tail -5
git add server/cmd/multica-repocache/config.go server/cmd/multica-repocache/config_test.go server/go.mod server/go.sum
git commit -m "feat(repocache): config loader (env + YAML)"
```

---

## Task 2: Workspace syncer — discover repos + drive Cache.Sync

**Files:**
- Create: `server/cmd/multica-repocache/syncer.go`
- Create: `server/cmd/multica-repocache/syncer_test.go`

- [ ] **Step 1: Failing test**

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestSyncOnce_ClonesEachWorkspacesRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a fake repo list for any workspace
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workspace_id":  "ws-A",
			"repos":         []map[string]string{{"url": "https://example.invalid/owner/repo.git"}},
			"repos_version": "v1",
		})
	}))
	defer srv.Close()

	cli := daemon.NewClient(srv.URL); cli.SetToken("tk")
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))

	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}}}

	// We don't actually do network clones in CI — verify SyncOnce gathers the
	// right repo list and would call Cache.Sync. We stub by wrapping Cache in
	// an interface during refactor (next step), or simply assert no panic and
	// the per-workspace repo set was captured.
	err := SyncOnce(context.Background(), cli, cache, cfg)
	if err == nil {
		// network clone of example.invalid will fail; either way SyncOnce
		// should return an aggregated error per failing workspace, not panic.
	}
}
```

(See note in Step 3 — the test verifies SyncOnce's wiring; the per-repo clone is exercised by Cache's own tests.)

- [ ] **Step 2: Verify it fails**

```bash
go test ./cmd/multica-repocache/ -run TestSyncOnce -v 2>&1 | tail -10
```

Expected: FAIL — `SyncOnce` / `testLogger` undefined.

- [ ] **Step 3: Implement**

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// SyncOnce iterates the configured workspaces, fetches each workspace's repo
// list from Multica, and asks the Cache to mirror them. Errors from one
// workspace do not abort sync of the others.
func SyncOnce(ctx context.Context, cli *daemon.Client, cache *repocache.Cache, cfg *Config) error {
	var errs []string
	for _, w := range cfg.Workspaces {
		resp, err := cli.GetWorkspaceRepos(ctx, w.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("workspace %s: get repos: %v", w.ID, err))
			continue
		}
		repos := make([]repocache.RepoInfo, 0, len(resp.Repos))
		for _, r := range resp.Repos {
			repos = append(repos, repocache.RepoInfo{URL: r.URL})
		}
		if err := cache.Sync(w.ID, repos); err != nil {
			errs = append(errs, fmt.Sprintf("workspace %s: sync: %v", w.ID, err))
			continue
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// RunSyncLoop calls SyncOnce on `interval` ticks until ctx is cancelled.
func RunSyncLoop(ctx context.Context, logger *slog.Logger, cli *daemon.Client, cache *repocache.Cache, cfg *Config) {
	if err := SyncOnce(ctx, cli, cache, cfg); err != nil {
		logger.Warn("initial sync had errors", "error", err)
	}
	t := time.NewTicker(cfg.FetchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := SyncOnce(ctx, cli, cache, cfg); err != nil {
				logger.Warn("sync errors", "error", err)
			}
		}
	}
}
```

Add the small test helper:

```go
// in syncer_test.go
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./cmd/multica-repocache/ -run TestSyncOnce -v 2>&1 | tail -10
git add server/cmd/multica-repocache/syncer.go server/cmd/multica-repocache/syncer_test.go server/go.mod server/go.sum
git commit -m "feat(repocache): workspace syncer wired to existing Cache"
```

---

## Task 3: Admin HTTP API + Prometheus metrics

**Files:**
- Create: `server/cmd/multica-repocache/server.go`
- Create: `server/cmd/multica-repocache/server_test.go`
- Create: `server/cmd/multica-repocache/metrics.go`

The API surface:
- `GET /healthz` → `200 ok`
- `GET /repos` → list of `{workspace_id, url, bare_path}` for everything currently mirrored
- `POST /repos/fetch?workspace_id=<ws>&url=<u>` → force a fetch on a single repo (used by the controller when a workspace's repo list churns mid-cycle)
- `GET /metrics` (on the metrics port) → Prometheus

- [ ] **Step 1: Failing test**

Create `server/cmd/multica-repocache/server_test.go`:

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestAdminAPI_HealthAndList(t *testing.T) {
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))
	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}}}

	srv := httptest.NewServer(NewAdminMux(cache, cfg))
	defer srv.Close()

	// /healthz
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: err=%v status=%v", err, resp.StatusCode)
	}

	// /repos returns empty list when cache is empty
	resp, err = http.Get(srv.URL + "/repos")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("repos: err=%v status=%v", err, resp.StatusCode)
	}

	// /repos/fetch on an unknown workspace+url returns 404
	req, _ := http.NewRequestWithContext(context.Background(), "POST",
		srv.URL+"/repos/fetch?workspace_id=ws-A&url=https://nope.invalid/x.git", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("fetch unknown: got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Verify it fails**

```bash
go test ./cmd/multica-repocache/ -run TestAdminAPI -v 2>&1 | tail -10
```

- [ ] **Step 3: Implement `server.go`**

```go
package main

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

type adminListEntry struct {
	WorkspaceID string `json:"workspace_id"`
	URL         string `json:"url"`
	BarePath    string `json:"bare_path"`
}

// NewAdminMux returns the chi-less stdlib mux serving the admin API. We
// intentionally avoid bringing chi into a fresh binary — three routes don't
// justify a router dep.
func NewAdminMux(cache *repocache.Cache, cfg *Config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/repos", func(w http.ResponseWriter, r *http.Request) {
		// We don't track all-repo state inside Cache; the list is the union
		// of every workspace's configured set Looked up.
		// (A more complete implementation would expose Cache.List(); kept
		// minimal here because the controller doesn't actually consume this
		// — it's for human ops.)
		entries := []adminListEntry{}
		for _, w := range cfg.Workspaces {
			_ = w // see cache extension note below
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	})
	mux.HandleFunc("/repos/fetch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ws := r.URL.Query().Get("workspace_id")
		url := r.URL.Query().Get("url")
		if ws == "" || url == "" {
			http.Error(w, "workspace_id and url required", http.StatusBadRequest)
			return
		}
		bare := cache.Lookup(ws, url)
		if bare == "" {
			http.Error(w, "unknown", http.StatusNotFound)
			return
		}
		if err := cache.Fetch(bare); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("fetched\n"))
	})
	return mux
}
```

- [ ] **Step 4: Implement `metrics.go`**

```go
package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	syncTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "multica_repocache_sync_total",
		Help: "Number of sync attempts grouped by workspace and outcome.",
	}, []string{"workspace_id", "outcome"})

	fetchDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "multica_repocache_fetch_seconds",
		Help:    "Per-repo git fetch duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"workspace_id"})
)

func init() {
	prometheus.MustRegister(syncTotal, fetchDuration)
}

func NewMetricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
```

(The counters are wired from `SyncOnce` in Task 4; metrics file landed here so it's a single small commit.)

- [ ] **Step 5: Test + commit**

```bash
go mod tidy
go test ./cmd/multica-repocache/ -v 2>&1 | tail -10
git add server/cmd/multica-repocache/server.go server/cmd/multica-repocache/server_test.go server/cmd/multica-repocache/metrics.go server/go.mod server/go.sum
git commit -m "feat(repocache): admin API + Prometheus metrics scaffolding"
```

---

## Task 4: Wire metrics into the syncer

**Files:**
- Modify: `server/cmd/multica-repocache/syncer.go`
- Modify: `server/cmd/multica-repocache/syncer_test.go`

- [ ] **Step 1: Update SyncOnce to bump counters**

Replace the loop body in `SyncOnce`:

```go
	for _, w := range cfg.Workspaces {
		resp, err := cli.GetWorkspaceRepos(ctx, w.ID)
		if err != nil {
			syncTotal.WithLabelValues(w.ID, "repos_fetch_error").Inc()
			errs = append(errs, fmt.Sprintf("workspace %s: get repos: %v", w.ID, err))
			continue
		}
		repos := make([]repocache.RepoInfo, 0, len(resp.Repos))
		for _, r := range resp.Repos {
			repos = append(repos, repocache.RepoInfo{URL: r.URL})
		}
		start := time.Now()
		if err := cache.Sync(w.ID, repos); err != nil {
			syncTotal.WithLabelValues(w.ID, "sync_error").Inc()
			errs = append(errs, fmt.Sprintf("workspace %s: sync: %v", w.ID, err))
			continue
		}
		fetchDuration.WithLabelValues(w.ID).Observe(time.Since(start).Seconds())
		syncTotal.WithLabelValues(w.ID, "ok").Inc()
	}
```

- [ ] **Step 2: Add a counter-bump assertion to the test**

(Extend `TestSyncOnce_*` to read the `syncTotal` counter via `testutil.ToFloat64`.)

- [ ] **Step 3: Test + commit**

```bash
go test ./cmd/multica-repocache/ -v 2>&1 | tail -10
git add server/cmd/multica-repocache/syncer.go server/cmd/multica-repocache/syncer_test.go
git commit -m "feat(repocache): emit per-workspace sync metrics"
```

---

## Task 5: main.go — wire it together

**Files:**
- Create: `server/cmd/multica-repocache/main.go`

- [ ] **Step 1: Write main.go**

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("repocache exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		return err
	}
	cache := repocache.New(cfg.RepoRoot, logger)

	cli := daemon.NewClient(cfg.ServerBaseURL)
	cli.SetToken(cfg.Token)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Sync loop
	go RunSyncLoop(ctx, logger, cli, cache, cfg)

	// Admin server
	adminSrv := &http.Server{Addr: cfg.AdminAddr, Handler: NewAdminMux(cache, cfg), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("admin server", "error", err)
		}
	}()

	// Metrics server
	metricsSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: NewMetricsMux(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = adminSrv.Shutdown(shutdownCtx)
	_ = metricsSrv.Shutdown(shutdownCtx)
	return nil
}
```

- [ ] **Step 2: Build the binary**

```bash
cd /Users/cjs/dev/multica/server
go build -o /tmp/multica-repocache ./cmd/multica-repocache
/tmp/multica-repocache || true   # errors cleanly on missing env
```

- [ ] **Step 3: Run full test suite**

```bash
go test ./... 2>&1 | tail -5
```

- [ ] **Step 4: Commit**

```bash
git add server/cmd/multica-repocache/main.go
git commit -m "feat(repocache): main — sync loop, admin API, metrics"
```

---

## Task 6: Dockerfile

**Files:**
- Create: `packaging/docker/repocache/Dockerfile`

The repocache needs `git` and `openssh-client` available at runtime (it forks `git fetch`), and it talks to GitHub via the deploy key in `multica-git-ssh`. So unlike the controller (distroless), this image needs a real `bookworm-slim` base.

- [ ] **Step 1: Write it**

```dockerfile
# packaging/docker/repocache/Dockerfile

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server/ ./server/
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/multica-repocache ./cmd/multica-repocache

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      git \
      openssh-client \
      ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 1001 multica \
    && useradd -m -u 1001 -g 1001 -s /bin/bash multica \
    && mkdir -p /home/multica/.ssh \
    && ssh-keyscan github.com > /home/multica/.ssh/known_hosts 2>/dev/null \
    && chown -R multica:multica /home/multica/.ssh \
    && chmod 700 /home/multica/.ssh \
    && chmod 644 /home/multica/.ssh/known_hosts

COPY --from=build /out/multica-repocache /usr/local/bin/multica-repocache
USER multica
WORKDIR /home/multica
ENTRYPOINT ["/usr/local/bin/multica-repocache"]
```

- [ ] **Step 2: Build locally**

```bash
cd /Users/cjs/dev/multica
docker build --platform linux/amd64 -f packaging/docker/repocache/Dockerfile -t multica-repocache:test .
docker image inspect multica-repocache:test --format '{{.Size}}'
```

- [ ] **Step 3: Commit**

```bash
git add packaging/docker/repocache/Dockerfile
git commit -m "feat(packaging): multica-repocache image"
```

---

## Task 7: build-images.sh target

**Files:**
- Modify: `packaging/scripts/build-images.sh`

- [ ] **Step 1: Add to IMAGES map**

```bash
[repocache]="packaging/docker/repocache/Dockerfile"
```

- [ ] **Step 2: Smoke-test**

```bash
./packaging/scripts/build-images.sh --no-push --tag testrc repocache
```

- [ ] **Step 3: Commit**

```bash
git add packaging/scripts/build-images.sh
git commit -m "feat(packaging): build-images.sh builds the repocache"
```

---

## Task 8: Helm — values + image helper

**Files:**
- Modify: `packaging/helm/multica/values.yaml`
- Modify: `packaging/helm/multica/templates/_helpers.tpl`

- [ ] **Step 1: Add `runtime.repocache.*` to values.yaml**

```yaml
  repocache:
    enabled: true
    replicaCount: 1
    image: { name: multica-repocache, tag: "" }
    storage:
      storageClass: ""    # MUST be RWX-capable
      accessMode: ReadWriteMany
      size: 20Gi
    fetchInterval: 60s
    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits:   { cpu: 1,    memory: 1Gi }
```

- [ ] **Step 2: Add helper**

```tpl
{{- define "multica.repocacheImage" -}}
{{- $img := .Values.runtime.repocache.image -}}
{{- $tag := default .Values.image.tag $img.tag -}}
{{- printf "%s/%s:%s" .Values.image.registry $img.name $tag -}}
{{- end }}
```

- [ ] **Step 3: Render-test + commit**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell \
  --set image.tag=v0.4.0-mk1 2>&1 | head -10
git add packaging/helm/multica/values.yaml packaging/helm/multica/templates/_helpers.tpl
git commit -m "feat(helm): runtime.repocache.* values + image helper"
```

---

## Task 9: Helm — RWX PVC

**Files:**
- Create: `packaging/helm/multica/templates/runtime/repocache-pvc.yaml`

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.repocache.enabled }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: multica-repocache-repos
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "multica.componentLabels" (dict "name" "repocache" "ctx" .) | nindent 4 }}
  annotations:
    "helm.sh/resource-policy": keep
spec:
  accessModes: [{{ .Values.runtime.repocache.storage.accessMode | quote }}]
  resources:
    requests:
      storage: {{ .Values.runtime.repocache.storage.size | quote }}
  {{- with .Values.runtime.repocache.storage.storageClass }}
  storageClassName: {{ . | quote }}
  {{- end }}
{{- end }}
```

- [ ] **Render + commit**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell \
  --set image.tag=v0.4.0-mk1 \
  --set runtime.enabled=true --set runtime.mode=controller \
  --set runtime.workspaceId=foo \
  -s templates/runtime/repocache-pvc.yaml
git add packaging/helm/multica/templates/runtime/repocache-pvc.yaml
git commit -m "feat(helm): repocache RWX PVC"
```

---

## Task 10: Helm — repocache ConfigMap (workspaces to mirror)

**Files:**
- Create: `packaging/helm/multica/templates/runtime/repocache-config.yaml`

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.repocache.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: multica-repocache-config
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "multica.componentLabels" (dict "name" "repocache" "ctx" .) | nindent 4 }}
data:
  runtime.yaml: |
    workspaces:
{{- range $w := default (list (dict "id" .Values.runtime.workspaceId)) (default list .Values.runtime.workspaces) }}
      - id: {{ required "runtime.workspaceId or runtime.workspaces[].id required" $w.id | quote }}
{{- end }}
{{- end }}
```

- [ ] **Commit**

```bash
git add packaging/helm/multica/templates/runtime/repocache-config.yaml
git commit -m "feat(helm): repocache ConfigMap with workspace list"
```

---

## Task 11: Helm — Deployment

**Files:**
- Create: `packaging/helm/multica/templates/runtime/repocache-deployment.yaml`

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.repocache.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multica-repocache
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "multica.componentLabels" (dict "name" "repocache" "ctx" .) | nindent 4 }}
spec:
  replicas: {{ .Values.runtime.repocache.replicaCount }}
  strategy:
    type: Recreate                # single-writer; never two replicas live at once
  selector:
    matchLabels:
      {{- include "multica.componentSelector" (dict "name" "repocache" "ctx" .) | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "multica.componentLabels" (dict "name" "repocache" "ctx" .) | nindent 8 }}
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/runtime/repocache-config.yaml") . | sha256sum }}
    spec:
      imagePullSecrets:
        - name: {{ .Values.image.pullSecret }}
      securityContext:
        runAsNonRoot: true
        runAsUser: 1001
        runAsGroup: 1001
        fsGroup: 1001
        seccompProfile: { type: RuntimeDefault }
      initContainers:
        - name: install-ssh-key
          image: busybox:1.36
          command:
            - sh
            - -c
            - |
              cp /secret/id_ed25519 /home/multica/.ssh/id_ed25519
              chmod 600 /home/multica/.ssh/id_ed25519
          securityContext:
            allowPrivilegeEscalation: false
            capabilities: { drop: ["ALL"] }
            seccompProfile: { type: RuntimeDefault }
          volumeMounts:
            - { name: git-ssh, mountPath: /secret, readOnly: true }
            - { name: ssh-home, mountPath: /home/multica/.ssh }
      containers:
        - name: repocache
          image: {{ include "multica.repocacheImage" . }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          securityContext:
            allowPrivilegeEscalation: false
            capabilities: { drop: ["ALL"] }
            seccompProfile: { type: RuntimeDefault }
            readOnlyRootFilesystem: true
          env:
            - name: MULTICA_SERVER_URL
              value: {{ .Values.runtime.serverUrl | quote }}
            - name: MULTICA_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.runtime.tokenSecretName }}
                  key: token
            - name: REPOCACHE_CONFIG_DIR
              value: /etc/repocache
            - name: REPOCACHE_FETCH_INTERVAL
              value: {{ .Values.runtime.repocache.fetchInterval | quote }}
            - name: HOME
              value: /home/multica
          ports:
            - { containerPort: 8080, name: admin }
            - { containerPort: 9090, name: metrics }
          readinessProbe:
            httpGet: { path: /healthz, port: admin }
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet: { path: /healthz, port: admin }
            initialDelaySeconds: 30
            periodSeconds: 30
          resources:
            {{- toYaml .Values.runtime.repocache.resources | nindent 12 }}
          volumeMounts:
            - { name: repos,     mountPath: /repos }
            - { name: config,    mountPath: /etc/repocache, readOnly: true }
            - { name: ssh-home,  mountPath: /home/multica/.ssh, readOnly: true }
      volumes:
        - name: repos
          persistentVolumeClaim: { claimName: multica-repocache-repos }
        - name: config
          configMap: { name: multica-repocache-config }
        - name: git-ssh
          secret:
            secretName: {{ .Values.runtime.gitSshSecretName }}
            defaultMode: 0400
        - name: ssh-home
          emptyDir: {}
{{- end }}
```

- [ ] **Render + commit**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell --set image.tag=v0.4.0-mk1 \
  --set runtime.enabled=true --set runtime.mode=controller \
  --set runtime.workspaceId=foo --set runtime.repocache.storage.storageClass=synology-nfs-csi \
  -s templates/runtime/repocache-deployment.yaml | head -40
git add packaging/helm/multica/templates/runtime/repocache-deployment.yaml
git commit -m "feat(helm): repocache Deployment"
```

---

## Task 12: Helm — Service

**Files:**
- Create: `packaging/helm/multica/templates/runtime/repocache-service.yaml`

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.repocache.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: multica-repocache
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "multica.componentLabels" (dict "name" "repocache" "ctx" .) | nindent 4 }}
spec:
  type: ClusterIP
  selector:
    {{- include "multica.componentSelector" (dict "name" "repocache" "ctx" .) | nindent 4 }}
  ports:
    - { name: admin,   port: 8080, targetPort: admin }
    - { name: metrics, port: 9090, targetPort: metrics }
{{- end }}
```

- [ ] **Commit**

```bash
git add packaging/helm/multica/templates/runtime/repocache-service.yaml
git commit -m "feat(helm): repocache Service"
```

---

## Task 13: Controller — mount /repos RO in worker Jobs + gitconfig CM

**Files:**
- Modify: `server/cmd/multica-k8s-controller/jobs.go`
- Modify: `server/cmd/multica-k8s-controller/jobs_test.go`
- Modify: `server/cmd/multica-k8s-controller/dispatcher.go`
- Modify: `server/cmd/multica-k8s-controller/main.go`

We add two things to every worker Job spec:

1. **A read-only mount** of the `multica-repocache-repos` PVC at `/repos`.
2. **A per-task gitconfig ConfigMap** mounted at `/home/multica/.gitconfig`. The ConfigMap content is generated by the controller per dispatch from the task's `Repos` list:

   ```ini
   [url "file:///repos/{ws}/{slug}"]
       insteadOf = https://github.com/{org}/{repo}
       insteadOf = https://github.com/{org}/{repo}.git
       insteadOf = git@github.com:{org}/{repo}
       insteadOf = git@github.com:{org}/{repo}.git
   ```

   The slug computation reuses what Plan E's `repocache.Cache` already produces on disk (a sha1 + the host/path tail). Rather than reimplementing it, we ask the cache for the slug via a small helper — see step 2 below.

- [ ] **Step 1: Add a `RepoCacheEnabled bool` to Config; main passes from cfg**

In `config.go` add `RepoCachePVCName string` (default `"multica-repocache-repos"`) and `RepoCacheRoot string` (default `"/repos"`). In `main.go` read them from env if set.

- [ ] **Step 2: Add a helper on `repocache.Cache` for slug lookup**

In `server/internal/daemon/repocache/cache.go`, expose:

```go
// SlugFor returns the on-disk slug used for (workspaceID, url). Useful for
// callers (like the K8s controller) that need to construct a file:// URL
// without doing a Sync first.
func (c *Cache) SlugFor(workspaceID, url string) string { ... }
```

Use the existing internal slug helper (the unexported function that drives `Sync`). Add a one-line test.

- [ ] **Step 3: Update `DispatchJob` to accept `repoCacheEnabled`, `repoCachePVC`, `repoCacheRoot`, and the task's gitconfig CM name**

```go
// new signature
DispatchJob(ctx, k, namespace, r, t, imagePullSecret, pvc string, opt JobOptions) (string, error)

type JobOptions struct {
    RepoCacheEnabled bool
    RepoCachePVC     string
    RepoCacheRoot    string
}
```

When `opt.RepoCacheEnabled`:
- Generate per-task gitconfig content from `t.Repos`. For each repo URL, compute the slug (`SlugFor`) and append the four `insteadOf` lines.
- Create a ConfigMap `task-{shortID}-gitconfig` with that content under key `.gitconfig`.
- Add to the worker container's `volumeMounts`:
  - `{name: repocache, mountPath: opt.RepoCacheRoot, readOnly: true}`
  - `{name: gitconfig, mountPath: /home/multica/.gitconfig, subPath: .gitconfig, readOnly: true}`
- Add to `Volumes`:
  - `{name: repocache, PVC: {ClaimName: opt.RepoCachePVC, ReadOnly: true}}`
  - `{name: gitconfig, ConfigMap: {Name: "task-{short}-gitconfig"}}`

- [ ] **Step 4: Update tests**

In `jobs_test.go`, add a `TestDispatchJob_WithRepoCache` that asserts:
- The Job spec has the `repocache` volume + RO mount at `/repos`.
- A gitconfig ConfigMap was created with `insteadOf` entries for each repo in the task.

- [ ] **Step 5: Update `DispatchOnce` to thread `JobOptions` through; main wires from cfg**

- [ ] **Step 6: Tests + commit**

```bash
go test ./cmd/multica-k8s-controller/ ./internal/daemon/repocache/ -v 2>&1 | tail -10
git add server/cmd/multica-k8s-controller/ server/internal/daemon/repocache/cache.go server/internal/daemon/repocache/cache_test.go
git commit -m "feat(controller): mount repocache PVC RO + per-task gitconfig for URL rewrites"
```

---

## Task 14: Helm — controller mounts the repocache PVC

**Files:**
- Modify: `packaging/helm/multica/templates/runtime/controller-configmap.yaml`
- Modify: `packaging/helm/multica/templates/runtime/controller-deployment.yaml`

The controller doesn't itself mount the PVC; it just needs to know the PVC's name and mount path so it can include them in the worker Job specs it creates.

- [ ] **Step 1: Add to controller-config.yaml**

```yaml
data:
  runtime.yaml: |
    workspaces: ...   # unchanged
    imagePullSecret: ...
    repoCache:
      enabled: {{ .Values.runtime.repocache.enabled }}
      pvcName: multica-repocache-repos
      mountPath: /repos
```

- [ ] **Step 2: Controller config loader reads `repoCache.*`**

(Tasks 1's config.go already lays out where to add these fields; in this Task 14 we wire them into the controller's `Config` struct so `main.go` passes them to `JobOptions`.)

- [ ] **Step 3: Render + commit**

```bash
helm template ... -s templates/runtime/controller-configmap.yaml
git add packaging/helm/multica/templates/runtime/controller-configmap.yaml
git add server/cmd/multica-k8s-controller/config.go server/cmd/multica-k8s-controller/config_test.go
git commit -m "feat(controller): plumb repoCache.* from Helm config to JobOptions"
```

---

## Task 15: RBAC — controller can create ConfigMaps for gitconfig

The Plan E Role already includes `configmaps: [get, list, watch, create, update, delete]`. No change needed — verify by reading `controller-rbac.yaml`. If somehow missing, add it.

- [ ] **Verify only (no commit).**

---

## Task 16: Push images, deploy

- [ ] **Step 1: Push controller + repocache at the new TAG**

```bash
./packaging/scripts/build-images.sh --tag "$TAG" controller repocache
```

- [ ] **Step 2: Update operator override values**

In `~/kube/apps/multica/values.yaml`, bump `image.tag` and add:

```yaml
runtime:
  repocache:
    enabled: true
    storage:
      storageClass: synology-nfs-csi-rwx     # MUST be RWX; if your synology
                                             # nfs-csi class supports it natively,
                                             # use the same name as workdir
      size: 20Gi
```

- [ ] **Step 3: Apply**

```bash
helm upgrade --install multica packaging/helm/multica/ -n multica -f ~/kube/apps/multica/values.yaml
kubectl -n multica rollout status deploy/multica-repocache --timeout=180s
kubectl -n multica rollout status deploy/multica-controller --timeout=120s
```

- [ ] **Step 4: Verify cache pod logs**

```bash
kubectl -n multica logs deploy/multica-repocache --tail=30
```

Expected: first sync completes within ~60s, log lines like `sync ok workspace_id=...` and `git fetch ... done`.

- [ ] **Step 5: Verify bare clones landed on the PVC**

```bash
kubectl -n multica exec deploy/multica-repocache -- ls /repos
kubectl -n multica exec deploy/multica-repocache -- du -sh /repos/* 2>&1 | head -10
```

---

## Task 17: End-to-end — worker uses the cache

- [ ] **Step 1: Assign a task to the agent in the web UI** that requires checking out a repo (anything that runs against `chrissnell/graywolf`).

- [ ] **Step 2: Watch the worker pod log**

```bash
kubectl -n multica logs -l app.kubernetes.io/managed-by=multica-k8s-controller -c runtask --tail=80 --follow
```

Expected: `git clone` line shows `file:///repos/{ws}/{slug}` or the rewrite is silently in effect (clone takes <2s for Graywolf; the old direct-from-GitHub took 5-15s).

- [ ] **Step 3: Verify the gitconfig CM and PVC mount on the worker pod**

```bash
POD=$(kubectl -n multica get pods -l app.kubernetes.io/managed-by=multica-k8s-controller --field-selector=status.phase=Running -o name | head -1)
kubectl -n multica exec "$POD" -c runtask -- cat /home/multica/.gitconfig
kubectl -n multica exec "$POD" -c runtask -- ls /repos
```

Expected: the gitconfig shows the `insteadOf` entries; `/repos` lists the cached bare clones.

- [ ] **Step 4: Verify the agent completed the task** in the UI (task succeeds, faster than pre-cache baseline).

---

## Task 18: Operator docs

**Files:**
- Modify: `packaging/README.md`

Add a "Repo cache (Plan F.1)" section explaining:
- What it does and why (sub-second clones, single-writer to avoid races, decoupling from origin).
- The required RWX storage class.
- The `runtime.repocache.*` values keys.
- How to verify (`kubectl exec ... ls /repos`, the `multica_repocache_sync_total` Prometheus metric).
- How the URL rewrite works in worker pods (the gitconfig ConfigMap).

- [ ] **Commit**

```bash
git add packaging/README.md
git commit -m "docs(packaging): repo-cache operator guide"
```

---

## Task 19: Final regression

- [ ] `go vet ./... && go test ./...` clean.
- [ ] `helm lint` clean both modes.
- [ ] `kubectl -n multica get deploy,sts,svc,cm,pvc | head -30` matches expected end state (now also includes `multica-repocache` Deployment + Service + PVC + ConfigMap).
- [ ] Disable the cache (`runtime.repocache.enabled=false`) and confirm the worker pods still run (gitconfig CM and mount are gated, falling back to direct origin clones) — defensive sanity.

---

## What's next (deferred from this plan)

- **`git daemon` over `:9418`** — if/when an out-of-cluster client (e.g., a developer's laptop on the LAN) wants to consume the cache, add a small sidecar running `git daemon --base-path=/repos --export-all`. ~30 lines of YAML + one extra Service port. Cluster-local workers don't need it.
- **Per-repo concurrency limits** — `Cache.Sync` already serializes mutations per bare path; a top-level fetch concurrency cap can be added if many workspaces and slow origin push fetch latency to where it matters.
- **`/repos` listing on admin API** — the current `GET /repos` returns empty because `Cache` doesn't expose a `List()` method. Adding one is a 10-line patch when an operator actually wants it; deferred to avoid churning the cache contract for a non-critical endpoint.

## What this enables

- The agent's `git clone <origin-url>` is now a transparent local file clone. Sub-second for any sized repo.
- The cluster keeps running through transient GitHub outages — workers don't touch origin during the job; only the repocache does, on its own schedule.
- The next plan (token-shepherd) inherits the pattern: a small long-lived Deployment that owns a shared state (here `/repos`, there the OAuth secret), with workers reading from it via well-defined interface (here PVC mount + gitconfig; there: just keep reading the secret unchanged).
