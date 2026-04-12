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

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// isLikelyLocalPath heuristically identifies a value that points at a local
// filesystem. Used by /repo/checkout when the caller didn't specify a type.
// The daemon still calls `git rev-parse` inside CreateWorktreeFromLocal to
// reject non-repo paths.
func isLikelyLocalPath(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "file://") {
		return true
	}
	// Absolute POSIX path.
	if strings.HasPrefix(s, "/") {
		return true
	}
	// Tilde-prefixed path.
	if strings.HasPrefix(s, "~") {
		return true
	}
	// Windows drive letter (defensive — daemon primarily runs on macOS/Linux).
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return false
}

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
// The URL field is used as the primary identifier — it may be a GitHub URL,
// an absolute local path, or a repo id — and the daemon resolves which
// workspace repo it refers to. Type can be set explicitly to skip
// auto-detection.
type repoCheckoutRequest struct {
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"`       // "github" | "local" (optional)
	LocalPath   string `json:"local_path,omitempty"` // optional override for local repos
	WorkspaceID string `json:"workspace_id"`
	WorkDir     string `json:"workdir"`
	AgentName   string `json:"agent_name"`
	TaskID      string `json:"task_id"`
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

		// Figure out whether this is a local or remote repo. Explicit type
		// wins; otherwise fall back to daemon-side heuristics (absolute path
		// or file:// → local).
		isLocal := req.Type == "local" || req.LocalPath != "" || isLikelyLocalPath(req.URL)
		localPath := req.LocalPath
		if isLocal && localPath == "" {
			localPath = req.URL
		}

		var result *repocache.WorktreeResult
		var err error
		if isLocal {
			result, err = d.repoCache.CreateWorktreeFromLocal(repocache.LocalWorktreeParams{
				LocalPath: localPath,
				WorkDir:   req.WorkDir,
				AgentName: req.AgentName,
				TaskID:    req.TaskID,
			})
		} else {
			result, err = d.repoCache.CreateWorktree(repocache.WorktreeParams{
				WorkspaceID: req.WorkspaceID,
				RepoURL:     req.URL,
				WorkDir:     req.WorkDir,
				AgentName:   req.AgentName,
				TaskID:      req.TaskID,
			})
		}
		if err != nil {
			d.logger.Error("repo checkout failed", "url", req.URL, "local", isLocal, "error", err)
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
