package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// SingleTaskRunner is a stripped-down Daemon that executes exactly one Task
// and exits. It deliberately skips runtime registration, the daemonws WS
// subscription, the poll loop, the GC loop, and auto-updates. It DOES start
// the local helper HTTP server (127.0.0.1:HealthPort) so spawned agent
// processes can call `multica repo checkout` and friends.
//
// Used by the `multica run-task` subcommand in containerized worker pods
// (K8s controller creates the pods, this runs in them).
type SingleTaskRunner struct {
	*Daemon
	healthSrv  *http.Server
	healthLn   net.Listener
	healthPort int
	closed     atomic.Bool
}

// NewSingleTaskRunner constructs a runner. It resolves auth via MULTICA_TOKEN
// env (and falls back to the CLI config file, matching daemon.resolveAuth),
// and binds the local helper HTTP server on cfg.HealthPort (or an OS-picked
// port if HealthPort == 0).
func NewSingleTaskRunner(cfg Config, logger *slog.Logger) (*SingleTaskRunner, error) {
	if cfg.ServerBaseURL == "" {
		return nil, fmt.Errorf("ServerBaseURL is required")
	}
	if cfg.WorkspacesRoot == "" {
		return nil, fmt.Errorf("WorkspacesRoot is required")
	}

	cacheRoot := filepath.Join(cfg.WorkspacesRoot, ".repos")
	client := NewClient(cfg.ServerBaseURL)
	client.SetVersion(cfg.CLIVersion)

	d := &Daemon{
		cfg:                       cfg,
		client:                    client,
		repoCache:                 repocache.New(cacheRoot, logger),
		logger:                    logger,
		workspaces:                make(map[string]*workspaceState),
		runtimeIndex:              make(map[string]Runtime),
		runtimeSet:                newRuntimeSetWatcher(),
		agentVersions:             make(map[string]string),
		wsHBLastAck:               make(map[string]time.Time),
		activeEnvRoots:            make(map[string]int),
		runtimeGoneInflight:       make(map[string]struct{}),
		reregisterNextAttempt:     make(map[string]time.Time),
		reregisterLastCompletedAt: make(map[string]time.Time),
		cancelPollInterval:        5 * time.Second,
	}
	d.runner = taskRunnerFunc(d.runTask)

	// resolveAuth honours MULTICA_TOKEN env first and falls back to the CLI
	// config file. Single-task mode wants the same precedence so operators can
	// drive the subcommand from an env-var-only container.
	if err := d.resolveAuth(); err != nil {
		return nil, fmt.Errorf("no auth: set MULTICA_TOKEN env or run multica login: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.HealthPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("bind helper server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	d.cfg.HealthPort = port

	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(time.Now()))
	mux.HandleFunc("/repo/checkout", d.repoCheckoutHandler())
	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Warn("single-task helper server stopped", "error", err)
		}
	}()

	return &SingleTaskRunner{
		Daemon:     d,
		healthSrv:  srv,
		healthLn:   ln,
		healthPort: port,
	}, nil
}

// Close shuts down the local helper server. Idempotent.
func (r *SingleTaskRunner) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.healthSrv.Shutdown(ctx)
}

// HealthPort returns the bound port of the local helper HTTP server.
func (r *SingleTaskRunner) HealthPort() int { return r.healthPort }

// SeedRuntime pre-populates runtimeIndex so handleTask's provider lookup works
// without the controller-side registration loop. Call this before RunOneTask.
func (r *SingleTaskRunner) SeedRuntime(runtimeID, provider string) {
	r.mu.Lock()
	r.runtimeIndex[runtimeID] = Runtime{ID: runtimeID, Provider: provider}
	r.mu.Unlock()
}

// RunOneTask runs exactly one task end-to-end (start → run → report) and
// returns once handleTask has reported the final disposition. The error
// return is reserved for setup failures (missing IDs, missing runtime
// seeding); per-task failures during execution are reported to the server
// via FailTask and return nil here.
//
// Concurrency: single-task — the daemon's slot pool is bypassed (slot=0).
func (r *SingleTaskRunner) RunOneTask(ctx context.Context, task Task) error {
	if task.ID == "" {
		return fmt.Errorf("task.ID required")
	}
	if task.WorkspaceID == "" {
		return fmt.Errorf("task.WorkspaceID required")
	}
	if task.RuntimeID == "" {
		return fmt.Errorf("task.RuntimeID required")
	}

	// handleTask looks up the provider via runtimeIndex[task.RuntimeID]. The
	// regular daemon's registration loop populates this; single-task mode
	// requires the caller to call SeedRuntime(task.RuntimeID, provider) first,
	// because the task payload itself does not carry the provider (AgentData
	// has no Provider field — provider lives on the Runtime row).
	r.mu.Lock()
	_, ok := r.runtimeIndex[task.RuntimeID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("runtime %q not seeded; call SeedRuntime(runtimeID, provider) before RunOneTask", task.RuntimeID)
	}

	r.handleTask(ctx, task, 0)
	return nil
}
