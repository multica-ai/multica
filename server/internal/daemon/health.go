package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// HealthResponse is returned by the daemon's local health endpoint.
type HealthResponse struct {
	Status     string            `json:"status"`
	PID        int               `json:"pid"`
	Uptime     string            `json:"uptime"`
	DaemonID   string            `json:"daemon_id"`
	DeviceName string            `json:"device_name"`
	ServerURL  string            `json:"server_url"`
	Agents     []string          `json:"agents"`
	Workspaces []healthWorkspace `json:"workspaces"`
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
	AgentName   string `json:"agent_name"`
	TaskID      string `json:"task_id"`
}

// watchWorkspaceRequest is the body of a POST /watch request.
type watchWorkspaceRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}

// watchedWorkspaceItem is one entry in the GET /watch response.
type watchedWorkspaceItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Runtime int    `json:"runtime_count"`
}

// serveHealth runs the health HTTP server on the given listener.
// Blocks until ctx is cancelled.
func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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
			Status:     "running",
			PID:        os.Getpid(),
			Uptime:     time.Since(startedAt).Truncate(time.Second).String(),
			DaemonID:   d.cfg.DaemonID,
			DeviceName: d.cfg.DeviceName,
			ServerURL:  d.cfg.ServerBaseURL,
			Agents:     agents,
			Workspaces: wsList,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			d.handleListWatched(w, r)
		case http.MethodPost:
			d.handleWatchWorkspace(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// DELETE /watch/{workspace_id}
	mux.HandleFunc("/watch/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/watch/")
		if id == "" {
			http.Error(w, "workspace_id is required in path", http.StatusBadRequest)
			return
		}
		d.handleUnwatchWorkspace(w, r, id)
	})

	mux.HandleFunc("/repo/checkout", func(w http.ResponseWriter, r *http.Request) {
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
		if req.WorkDir == "" {
			http.Error(w, "workdir is required", http.StatusBadRequest)
			return
		}

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		result, err := d.repoCache.CreateWorktree(repocache.WorktreeParams{
			WorkspaceID: req.WorkspaceID,
			RepoURL:     req.URL,
			WorkDir:     req.WorkDir,
			AgentName:   req.AgentName,
			TaskID:      req.TaskID,
		})
		if err != nil {
			d.logger.Error("repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

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

// handleListWatched returns the daemon's current watched workspaces merged
// with the persisted config so clients can reflect "what is watched" and
// "what has been explicitly opted out".
func (d *Daemon) handleListWatched(w http.ResponseWriter, _ *http.Request) {
	cfg, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
	if err != nil {
		http.Error(w, "load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	d.mu.Lock()
	items := make([]watchedWorkspaceItem, 0, len(cfg.WatchedWorkspaces))
	for _, ws := range cfg.WatchedWorkspaces {
		rc := 0
		if state, ok := d.workspaces[ws.ID]; ok {
			rc = len(state.runtimeIDs)
		}
		items = append(items, watchedWorkspaceItem{
			ID:      ws.ID,
			Name:    ws.Name,
			Runtime: rc,
		})
	}
	d.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"watched":   items,
		"unwatched": cfg.UnwatchedWorkspaces,
	})
}

// handleWatchWorkspace registers a new workspace at runtime. Updates config
// (removes from denylist, adds to watched list), registers runtimes via the
// API, and records the result in daemon state. Idempotent.
func (d *Daemon) handleWatchWorkspace(w http.ResponseWriter, r *http.Request) {
	var req watchWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}

	cfg, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
	if err != nil {
		http.Error(w, "load config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cfg.RemoveUnwatchedWorkspace(req.WorkspaceID)
	cfg.AddWatchedWorkspace(req.WorkspaceID, req.Name)
	if err := cli.SaveCLIConfigForProfile(cfg, d.cfg.Profile); err != nil {
		http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Skip registration if we're already tracking this workspace.
	d.mu.Lock()
	_, already := d.workspaces[req.WorkspaceID]
	d.mu.Unlock()
	if already {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "already_watching"})
		return
	}

	resp, err := d.registerRuntimesForWorkspace(r.Context(), req.WorkspaceID)
	if err != nil {
		d.logger.Error("watch: register failed", "workspace_id", req.WorkspaceID, "error", err)
		http.Error(w, "register runtimes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	runtimeIDs := make([]string, len(resp.Runtimes))
	for i, rt := range resp.Runtimes {
		runtimeIDs[i] = rt.ID
	}
	d.mu.Lock()
	d.workspaces[req.WorkspaceID] = &workspaceState{workspaceID: req.WorkspaceID, runtimeIDs: runtimeIDs}
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
	}
	d.mu.Unlock()

	if d.repoCache != nil && len(resp.Repos) > 0 {
		go func(wsID string, repos []RepoData) {
			if err := d.repoCache.Sync(wsID, repoDataToInfo(repos)); err != nil {
				d.logger.Warn("repo cache sync failed", "workspace_id", wsID, "error", err)
			}
		}(req.WorkspaceID, resp.Repos)
	}

	d.logger.Info("watch: now watching workspace", "workspace_id", req.WorkspaceID, "name", req.Name, "runtimes", len(resp.Runtimes))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":        "watching",
		"workspace_id":  req.WorkspaceID,
		"runtime_count": len(runtimeIDs),
	})
}

// handleUnwatchWorkspace stops tracking a workspace and records the opt-out
// in the denylist so the periodic API sync won't revive it.
func (d *Daemon) handleUnwatchWorkspace(w http.ResponseWriter, _ *http.Request, id string) {
	cfg, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
	if err != nil {
		http.Error(w, "load config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cfg.RemoveWatchedWorkspace(id)
	cfg.AddUnwatchedWorkspace(id)
	if err := cli.SaveCLIConfigForProfile(cfg, d.cfg.Profile); err != nil {
		http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	d.mu.Lock()
	if state, ok := d.workspaces[id]; ok {
		for _, rid := range state.runtimeIDs {
			delete(d.runtimeIndex, rid)
		}
		delete(d.workspaces, id)
	}
	d.mu.Unlock()

	d.logger.Info("watch: stopped watching workspace", "workspace_id", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "unwatched", "workspace_id": id})
}
