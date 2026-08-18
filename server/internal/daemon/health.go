package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// HealthResponse is returned by the daemon's local health endpoint.
type HealthResponse struct {
	Status string `json:"status"`
	PID    int    `json:"pid"`
	// OS is the daemon's runtime.GOOS. The desktop app compares it against its
	// own host OS to detect a daemon it cannot manage — e.g. a Windows desktop
	// reaching a Linux daemon inside WSL2 over localhost forwarding. The
	// lifecycle CLI (`daemon start/stop`) acts on the host process namespace,
	// so a foreign-OS daemon can't be started/stopped by the app even though
	// /health is reachable. See #3916.
	OS     string `json:"os"`
	Uptime string `json:"uptime"`
	// Profile names the CLI profile this daemon was started with, empty for
	// the default profile. Health ports are derived by hashing the profile
	// name into a 1000-port range, so two names can collide and a caller has
	// no other way to tell whose daemon answered: `--profile a daemon stop`
	// would happily kill profile b's daemon (#6694).
	//
	// Deliberately NOT omitempty. The empty string is a real answer — "I am
	// the default profile's daemon" — and must stay distinguishable from a
	// pre-#6694 daemon that cannot identify itself at all. Callers key off
	// the field's presence, so collapsing the two would make every default
	// daemon look unidentifiable.
	Profile    string `json:"profile"`
	DaemonID   string `json:"daemon_id"`
	DeviceName string `json:"device_name"`
	ServerURL  string `json:"server_url"`
	CLIVersion string `json:"cli_version"`
	// LaunchedBy is "desktop" when the Electron app spawned this daemon, empty
	// for a standalone one. Already reported to the server on registration;
	// surfaced here so `daemon status` can say who manages the daemon instead
	// of leaving the user to guess why a daemon they never started is running.
	LaunchedBy string `json:"launched_by,omitempty"`
	// ActiveTaskCount remains the compatibility/safety count of every claimed
	// handleTask lifecycle. The additive counters split actual provider
	// execution from local-directory parking for throughput and diagnostics.
	ActiveTaskCount       int64 `json:"active_task_count"`
	RunningTaskCount      int64 `json:"running_task_count"`
	ResourceWaitTaskCount int64 `json:"resource_wait_task_count"`
	// Repo maintenance stays a liveness-safe background activity, so health
	// remains HTTP 200/running. These additive counters explain degraded repo
	// checkout capacity to operators without exposing local cache paths.
	RepoMaintenanceActive int      `json:"repo_maintenance_active,omitempty"`
	RepoCheckoutWaiters   int      `json:"repo_checkout_waiters,omitempty"`
	Agents                []string `json:"agents"`
	// SkippedAgents maps a provider that WAS discovered on this machine to the
	// reason the last registration round dropped it (version undetectable,
	// below the minimum supported version). Purely diagnostic, and omitted when
	// empty so older consumers see no change.
	//
	// Without it, "CLI not installed" and "CLI installed but rejected" both
	// render as an absent runtime, which is what made GH #6077 unactionable for
	// the reporter (MUL-5439).
	SkippedAgents map[string]string `json:"skipped_agents,omitempty"`
	// ReloadPendingReason explains why the daemon has confirmed a multica
	// version change on disk but hasn't restarted into it yet — it was busy at
	// the last barrier check and will retry when idle. Omitted when empty, so
	// older consumers see no change. Diagnostic only: nothing keys off it.
	ReloadPendingReason string            `json:"reload_pending_reason,omitempty"`
	Workspaces          []healthWorkspace `json:"workspaces"`
}

type healthWorkspace struct {
	ID       string   `json:"id"`
	Runtimes []string `json:"runtimes"`
}

// listenHealth binds the health port. Returns the listener or an error if
// another daemon is already running (port taken).
func (d *Daemon) listenHealth() (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", d.cfg.HealthPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("another daemon is already running on %s: %w", addr, err)
	}
	return ln, nil
}

// repoCheckoutRequest is the body of a POST /repo/checkout request.
type repoCheckoutRequest struct {
	URL          string `json:"url"`
	WorkspaceID  string `json:"workspace_id"`
	WorkDir      string `json:"workdir"`
	Ref          string `json:"ref,omitempty"`
	AgentName    string `json:"agent_name"`
	TaskID       string `json:"task_id"`
	CheckoutMode string `json:"checkout_mode,omitempty"`
	// RetryBusy is sent by clients that understand 503 + Retry-After. Older
	// clients omit it and retain their historical unbounded lock-wait behavior.
	RetryBusy bool `json:"retry_busy,omitempty"`
}

const (
	repoCheckoutLockWaitTimeout = 10 * time.Second
	repoCheckoutRetryAfter      = 2 * time.Second
	repoCheckoutRetryHeader     = "X-Multica-Retryable"
	repoCheckoutRetryValueBusy  = "repo-busy"
)

// healthHandler returns the /health HTTP handler. Extracted from serveHealth
// so tests can exercise it without spinning up a listener.
func (d *Daemon) healthHandler(startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		var wsList []healthWorkspace
		for id, ws := range d.workspaces {
			wsList = append(wsList, healthWorkspace{
				ID:       id,
				Runtimes: ws.runtimeIDs,
			})
		}
		d.mu.Unlock()

		agents := make([]string, 0, len(d.agents()))
		for name := range d.agents() {
			agents = append(agents, name)
		}

		// "starting" until preflight (PAT renew + initial workspace sync +
		// runtime registration) completes; "running" once the daemon can
		// actually claim tasks. The health port is bound before preflight for
		// liveness/diagnostics, so callers must not treat a reachable endpoint
		// as ready — they gate on this status. Consumers that only know
		// "running" (older CLI/desktop) safely treat "starting" as not-ready.
		status := "starting"
		if d.ready.Load() {
			status = "running"
		}

		resp := HealthResponse{
			Status:                status,
			PID:                   os.Getpid(),
			OS:                    runtime.GOOS,
			Uptime:                time.Since(startedAt).Truncate(time.Second).String(),
			Profile:               d.cfg.Profile,
			LaunchedBy:            d.cfg.LaunchedBy,
			DaemonID:              d.cfg.DaemonID,
			DeviceName:            d.cfg.DeviceName,
			ServerURL:             d.cfg.ServerBaseURL,
			CLIVersion:            d.cfg.CLIVersion,
			ActiveTaskCount:       d.activeTasks.Load(),
			RunningTaskCount:      d.runningTasks.Load(),
			ResourceWaitTaskCount: d.resourceWaitTasks.Load(),
			Agents:                agents,
			SkippedAgents:         d.skippedAgentsSnapshot(),

			ReloadPendingReason: d.reloadPending(),
			Workspaces:          wsList,
		}
		if reporter, ok := d.repoCache.(interface{ Activity() repocache.Activity }); ok {
			activity := reporter.Activity()
			resp.RepoMaintenanceActive = activity.MaintenanceActive
			resp.RepoCheckoutWaiters = activity.ForegroundWaiters
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// shutdownHandler triggers a graceful daemon shutdown by cancelling the
// top-level context. Used by `multica daemon stop` so we don't depend on
// OS-signal delivery, which is unreliable on Windows once the daemon is
// spawned with DETACHED_PROCESS (no shared console with the stop caller).
// The listener is bound to 127.0.0.1 only, so only local processes can hit
// this endpoint.
func (d *Daemon) shutdownHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "shutting down"})
		if d.cancelFunc != nil {
			// Cancel asynchronously so the response flushes first; otherwise
			// srv.Close() races with the writer.
			go d.cancelFunc()
		}
	}
}

// serveHealth runs the health HTTP server on the given listener.
// Blocks until ctx is cancelled.
func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(startedAt))
	mux.HandleFunc("/shutdown", d.shutdownHandler())
	mux.HandleFunc("/repo/checkout", d.repoCheckoutHandler())

	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	d.logger.Info("health server listening", "addr", ln.Addr().String())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		d.logger.Warn("health server error", "error", err)
	}
}

func (d *Daemon) repoCheckoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req repoCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		req.URL = strings.TrimSpace(req.URL)
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			http.Error(w, "workspace_id is required", http.StatusBadRequest)
			return
		}
		if req.WorkDir == "" {
			http.Error(w, "workdir is required", http.StatusBadRequest)
			return
		}
		if req.CheckoutMode != "" && req.CheckoutMode != repoCheckoutModeIsolated {
			http.Error(w, "invalid checkout_mode", http.StatusBadRequest)
			return
		}

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		if err := d.ensureRepoReady(r.Context(), req.WorkspaceID, req.URL); err != nil {
			if r.Context().Err() != nil {
				d.logger.Debug("repo checkout readiness cancelled", "url", req.URL, "error", err)
				return
			}
			statusCode := http.StatusInternalServerError
			if errors.Is(err, ErrRepoNotConfigured) {
				statusCode = http.StatusBadRequest
			}
			d.logger.Error("repo checkout readiness failed", "workspace_id", req.WorkspaceID, "url", req.URL, "error", err)
			http.Error(w, err.Error(), statusCode)
			return
		}

		checkoutRef := strings.TrimSpace(req.Ref)
		if checkoutRef == "" {
			checkoutRef = d.taskRepoDefaultRef(req.WorkspaceID, req.TaskID, req.URL)
		}

		params := repocache.WorktreeParams{
			WorkspaceID:         req.WorkspaceID,
			RepoURL:             req.URL,
			WorkDir:             req.WorkDir,
			Ref:                 checkoutRef,
			AgentName:           req.AgentName,
			TaskID:              req.TaskID,
			CoAuthoredByEnabled: d.workspaceCoAuthoredByEnabled(req.WorkspaceID),
			IsolatedGitMetadata: req.CheckoutMode == repoCheckoutModeIsolated,
		}
		if req.RetryBusy {
			params.LockWaitTimeout = repoCheckoutLockWaitTimeout
		}
		var result *repocache.WorktreeResult
		var err error
		if cache, ok := d.repoCache.(interface {
			CreateWorktreeContext(context.Context, repocache.WorktreeParams) (*repocache.WorktreeResult, error)
		}); ok {
			result, err = cache.CreateWorktreeContext(r.Context(), params)
		} else {
			result, err = d.repoCache.CreateWorktree(params)
		}
		if err != nil {
			if errors.Is(err, repocache.ErrRepoBusy) && req.RetryBusy {
				w.Header().Set(repoCheckoutRetryHeader, repoCheckoutRetryValueBusy)
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", repoCheckoutRetryAfter.Seconds()))
				http.Error(w, "repository is busy with another operation; retry later", http.StatusServiceUnavailable)
				return
			}
			if r.Context().Err() != nil {
				d.logger.Debug("repo checkout cancelled", "url", req.URL, "error", err)
				return
			}
			d.logger.Error("repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
