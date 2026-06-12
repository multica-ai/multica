package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// HealthResponse is returned by the daemon's local health endpoint.
type HealthResponse struct {
	Status          string            `json:"status"`
	PID             int               `json:"pid"`
	Uptime          string            `json:"uptime"`
	DaemonID        string            `json:"daemon_id"`
	DeviceName      string            `json:"device_name"`
	ServerURL       string            `json:"server_url"`
	CLIVersion      string            `json:"cli_version"`
	ActiveTaskCount int64             `json:"active_task_count"`
	Agents          []string          `json:"agents"`
	Workspaces      []healthWorkspace `json:"workspaces"`
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
	URL         string `json:"url"`
	WorkspaceID string `json:"workspace_id"`
	WorkDir     string `json:"workdir"`
	Ref         string `json:"ref,omitempty"`
	AgentName   string `json:"agent_name"`
	TaskID      string `json:"task_id"`
}

// repoRefreshRequest is the body of a POST /repo/refresh request.
type repoRefreshRequest struct {
	URL         string `json:"url"`
	WorkspaceID string `json:"workspace_id"`
}

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

		agents := make([]string, 0, len(d.cfg.Agents))
		for name := range d.cfg.Agents {
			agents = append(agents, name)
		}

		resp := HealthResponse{
			Status:          "running",
			PID:             os.Getpid(),
			Uptime:          time.Since(startedAt).Truncate(time.Second).String(),
			DaemonID:        d.cfg.DaemonID,
			DeviceName:      d.cfg.DeviceName,
			ServerURL:       d.cfg.ServerBaseURL,
			CLIVersion:      d.cfg.CLIVersion,
			ActiveTaskCount: d.activeTasks.Load(),
			Agents:          agents,
			Workspaces:      wsList,
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

// repoCheckoutHandler returns the POST /repo/checkout HTTP handler. Extracted
// so both the long-running daemon (serveHealth) and the one-shot run-task
// helper server (single_task.go) can register the same behaviour.
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

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		if err := d.ensureRepoReady(r.Context(), req.WorkspaceID, req.URL); err != nil {
			statusCode := http.StatusInternalServerError
			if errors.Is(err, ErrRepoNotConfigured) {
				statusCode = http.StatusBadRequest
			}
			d.logger.Error("repo checkout readiness failed", "workspace_id", req.WorkspaceID, "url", req.URL, "error", err)
			http.Error(w, err.Error(), statusCode)
			return
		}

		result, err := d.repoCache.CreateWorktree(repocache.WorktreeParams{
			WorkspaceID:         req.WorkspaceID,
			RepoURL:             req.URL,
			WorkDir:             req.WorkDir,
			Ref:                 req.Ref,
			AgentName:           req.AgentName,
			TaskID:              req.TaskID,
			CoAuthoredByEnabled: d.workspaceCoAuthoredByEnabled(req.WorkspaceID),
		})
		if err != nil {
			d.logger.Error("repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// controllerRepoCheckoutHandler is the controller-mode (Plan F.1) variant of
// repoCheckoutHandler. It's registered by SingleTaskRunner when the runner
// detects MULTICA_REPOCACHE_DIR — meaning the bare clones are externally
// managed by the multica-repocache Deployment and mounted ReadOnly into
// this worker pod.
//
// Differences from the daemon-mode handler:
//   - No ensureRepoReady: there's no per-daemon workspaceState, no refresh
//     loop, and no point checking workspaceRepoAllowed because the gitconfig
//     mounted into this pod already constrains which URLs can be rewritten
//     into the cache. The controller validates the workspace's repo list
//     when generating the gitconfig.
//   - Uses Cache.CreateSharedClone instead of CreateWorktree: a `git worktree
//     add` against a RO bare fails because git writes worktree metadata into
//     the bare. The shared-clone path uses alternates + a writable .git in
//     the workdir.
//   - Co-authored-by is resolved by fetching the workspace settings live
//     (fetchCoAuthoredByEnabled). Controller-mode workers have no synced view
//     of the setting, so the gate is read at checkout time; a fetch failure
//     resolves to off so a stale or unreachable settings view can never
//     re-enable attribution.
func (d *Daemon) controllerRepoCheckoutHandler() http.HandlerFunc {
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
		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		result, err := d.repoCache.CreateSharedClone(repocache.WorktreeParams{
			WorkspaceID:         req.WorkspaceID,
			RepoURL:             req.URL,
			WorkDir:             req.WorkDir,
			Ref:                 req.Ref,
			AgentName:           req.AgentName,
			TaskID:              req.TaskID,
			CoAuthoredByEnabled: d.fetchCoAuthoredByEnabled(r.Context(), req.WorkspaceID),
		})
		if err != nil {
			d.logger.Error("controller repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// repoRefreshHandler returns the POST /repo/refresh HTTP handler for daemon
// mode. It looks up the bare clone for (workspace_id, url) and runs
// `git fetch origin` on it. Used by `multica repo refresh` so an agent can
// force the cache to pick up commits that landed within the daemon's sync
// interval window. Returns 404 when the URL is not in the cache, 400 on
// missing fields, 500 on fetch failure.
func (d *Daemon) repoRefreshHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req repoRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			http.Error(w, "workspace_id is required", http.StatusBadRequest)
			return
		}
		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}
		bare := d.repoCache.Lookup(req.WorkspaceID, req.URL)
		if bare == "" {
			http.Error(w, fmt.Sprintf("repo not found in cache: %s (workspace: %s)", req.URL, req.WorkspaceID), http.StatusNotFound)
			return
		}
		if err := d.repoCache.Fetch(bare); err != nil {
			d.logger.Error("repo refresh failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "refreshed"})
	}
}

// controllerRepoRefreshHandler is the controller-mode variant of
// repoRefreshHandler. The bare clone is on a ReadOnly mount owned by the
// multica-repocache Deployment, so the local worker daemon cannot run a fetch
// against it. Instead, this handler proxies to the repocache server's admin
// endpoint at MULTICA_REPOCACHE_URL, which executes the fetch on the writable
// side of the same PVC.
//
// Returns 503 when MULTICA_REPOCACHE_URL is not set (controller-mode is
// indicated by MULTICA_REPOCACHE_DIR being present; if URL is missing too,
// the deployment is misconfigured and the agent's refresh request cannot
// land).
func (d *Daemon) controllerRepoRefreshHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req repoRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			http.Error(w, "workspace_id is required", http.StatusBadRequest)
			return
		}
		adminBase := strings.TrimRight(os.Getenv("MULTICA_REPOCACHE_URL"), "/")
		if adminBase == "" {
			http.Error(w, "MULTICA_REPOCACHE_URL not set; controller is missing the repocache admin endpoint", http.StatusServiceUnavailable)
			return
		}
		// Build the admin /repos/fetch URL. The repocache admin server takes
		// workspace_id and url as query params (see cmd/multica-repocache/server.go).
		q := url.Values{}
		q.Set("workspace_id", req.WorkspaceID)
		q.Set("url", req.URL)
		fetchURL := adminBase + "/repos/fetch?" + q.Encode()
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Post(fetchURL, "application/x-www-form-urlencoded", nil)
		if err != nil {
			d.logger.Error("controller repo refresh: proxy failed", "url", req.URL, "error", err)
			http.Error(w, "proxy to repocache failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			// Mirror the upstream status so the agent sees 404 (not in cache)
			// vs 500 (fetch error) vs 400 (bad input) without translation.
			d.logger.Warn("controller repo refresh: upstream non-OK",
				"url", req.URL,
				"status", resp.StatusCode,
				"body", strings.TrimSpace(string(body)),
			)
			http.Error(w, strings.TrimSpace(string(body)), resp.StatusCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "refreshed"})
	}
}

// serveHealth runs the health HTTP server on the given listener.
// Blocks until ctx is cancelled.
func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(startedAt))
	mux.HandleFunc("/shutdown", d.shutdownHandler())
	mux.HandleFunc("/repo/checkout", d.repoCheckoutHandler())
	mux.HandleFunc("/repo/refresh", d.repoRefreshHandler())

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
