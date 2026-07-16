package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

const (
	repoCheckoutMaxBodyBytes = 16 << 10
	shutdownCredentialBytes  = 32

	// ShutdownCredentialHeader carries the operator-only credential accepted
	// by POST /shutdown. It is intentionally separate from task checkout
	// capabilities and is never exposed by /health or task environments.
	ShutdownCredentialHeader  = "X-Multica-Shutdown-Credential"
	ShutdownRequireIdleHeader = "X-Multica-Shutdown-Require-Idle"
	// ShutdownCredentialFileName is the profile-local state file read by the
	// lifecycle CLI. Its contents are generated afresh by each daemon process.
	ShutdownCredentialFileName = "daemon.shutdown-token"
)

// HealthResponse contains only public liveness/readiness metadata. The health
// port is reachable from task sandboxes, so management metadata belongs on the
// operator-authenticated /diagnostics endpoint instead.
type HealthResponse struct {
	Status string `json:"status"`
	// OS is the daemon's runtime.GOOS. The desktop app compares it against its
	// own host OS to detect a daemon it cannot manage — e.g. a Windows desktop
	// reaching a Linux daemon inside WSL2 over localhost forwarding. The
	// lifecycle CLI (`daemon start/stop`) acts on the host process namespace,
	// so a foreign-OS daemon can't be started/stopped by the app even though
	// /health is reachable. See #3916.
	OS string `json:"os"`
}

// DiagnosticsResponse is returned only to an operator that presents the
// daemon's per-process credential.
type DiagnosticsResponse struct {
	Status          string            `json:"status"`
	PID             int               `json:"pid"`
	OS              string            `json:"os"`
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

func createShutdownCredential(profile string) (string, error) {
	raw := make([]byte, shutdownCredentialBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate credential: %w", err)
	}
	credential := base64.RawURLEncoding.EncodeToString(raw)
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		return "", fmt.Errorf("resolve profile directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create profile directory: %w", err)
	}
	if err := writeShutdownCredential(filepath.Join(dir, ShutdownCredentialFileName), credential); err != nil {
		return "", err
	}
	return credential, nil
}

func writeShutdownCredential(path, credential string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon-shutdown-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary credential file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("protect temporary credential file: %w", err)
	}
	if _, err := tmp.WriteString(credential + "\n"); err != nil {
		cleanup()
		return fmt.Errorf("write temporary credential file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary credential file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temporary credential file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("publish shutdown credential: %w", err)
	}
	return nil
}

func removeShutdownCredential(path, credential string) {
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) != credential {
		return
	}
	_ = os.Remove(path)
}

// repoCheckoutRequest is the body of a POST /repo/checkout request.
type repoCheckoutRequest struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

// healthHandler returns the /health HTTP handler. Extracted from serveHealth
// so tests can exercise it without spinning up a listener.
func (d *Daemon) healthHandler(startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := HealthResponse{
			Status: d.healthStatus(),
			OS:     runtime.GOOS,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func (d *Daemon) healthStatus() string {
	if d.ready.Load() {
		return "running"
	}
	return "starting"
}

func validOperatorCredential(expected, supplied string) bool {
	supplied = strings.TrimSpace(supplied)
	if expected == "" || supplied == "" {
		return false
	}
	expectedDigest := sha256.Sum256([]byte(expected))
	suppliedDigest := sha256.Sum256([]byte(supplied))
	return subtle.ConstantTimeCompare(expectedDigest[:], suppliedDigest[:]) == 1
}

func (d *Daemon) diagnosticsHandler(startedAt time.Time, credential string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if credential == "" {
			http.Error(w, "operator credential unavailable", http.StatusServiceUnavailable)
			return
		}
		if !validOperatorCredential(credential, r.Header.Get(ShutdownCredentialHeader)) {
			http.Error(w, "invalid operator credential", http.StatusUnauthorized)
			return
		}

		d.mu.Lock()
		wsList := make([]healthWorkspace, 0, len(d.workspaces))
		for id, ws := range d.workspaces {
			runtimes := append([]string(nil), ws.runtimeIDs...)
			sort.Strings(runtimes)
			wsList = append(wsList, healthWorkspace{ID: id, Runtimes: runtimes})
		}
		d.mu.Unlock()
		sort.Slice(wsList, func(i, j int) bool { return wsList[i].ID < wsList[j].ID })

		agents := make([]string, 0, len(d.cfg.Agents))
		for name := range d.cfg.Agents {
			agents = append(agents, name)
		}
		sort.Strings(agents)

		resp := DiagnosticsResponse{
			Status:          d.healthStatus(),
			PID:             os.Getpid(),
			OS:              runtime.GOOS,
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
// Loopback reachability alone does not grant shutdown authority: callers must
// also present the per-process operator credential generated at daemon start.
func (d *Daemon) shutdownHandler(credential string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if credential == "" {
			http.Error(w, "shutdown credential unavailable", http.StatusServiceUnavailable)
			return
		}
		if !validOperatorCredential(credential, r.Header.Get(ShutdownCredentialHeader)) {
			http.Error(w, "invalid shutdown credential", http.StatusUnauthorized)
			return
		}
		requireIdle := r.Header.Get(ShutdownRequireIdleHeader) == "true"
		if requireIdle && !d.trySetClaimBarrier() {
			http.Error(w, "daemon is busy", http.StatusConflict)
			return
		}
		if requireIdle && d.cancelFunc == nil {
			d.releaseClaimBarrier()
			http.Error(w, "daemon shutdown unavailable", http.StatusServiceUnavailable)
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
	credential, err := createShutdownCredential(d.cfg.Profile)
	cleanupCredential := func() {}
	if err != nil {
		d.logger.Error("operator shutdown credential unavailable; /shutdown will fail closed", "error", err)
	} else {
		dir, pathErr := cli.ProfileDir(d.cfg.Profile)
		if pathErr != nil {
			d.logger.Error("operator shutdown credential path unavailable; /shutdown will fail closed", "error", pathErr)
			credential = ""
		} else {
			credentialPath := filepath.Join(dir, ShutdownCredentialFileName)
			cleanupCredential = func() { removeShutdownCredential(credentialPath, credential) }
			defer cleanupCredential()
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(startedAt))
	mux.HandleFunc("/diagnostics", d.diagnosticsHandler(startedAt, credential))
	mux.HandleFunc("/shutdown", d.shutdownHandler(credential))
	mux.HandleFunc("/repo/checkout", d.repoCheckoutHandler())

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Remove this process's credential before releasing the port. A new
		// daemon cannot publish its successor credential until this listener
		// has closed, so normal shutdown cannot delete the successor's file.
		cleanupCredential()
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

		token := strings.TrimSpace(r.Header.Get(repoCheckoutCapabilityHeader))
		binding, release, ok := d.acquireRepoCheckoutCapability(token)
		if !ok {
			http.Error(w, "invalid or expired repo checkout capability", http.StatusUnauthorized)
			return
		}
		defer release()

		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
			return
		}

		var req repoCheckoutRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, repoCheckoutMaxBodyBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid request body: expected one JSON object", http.StatusBadRequest)
			return
		}
		req.URL = strings.TrimSpace(req.URL)
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		boundRef, allowed := binding.repoRefs[req.URL]
		if !allowed {
			http.Error(w, "repo is not assigned to this task", http.StatusForbidden)
			return
		}
		checkoutRef := strings.TrimSpace(req.Ref)
		if checkoutRef != "" && checkoutRef != boundRef {
			http.Error(w, "ref is not assigned to this task", http.StatusForbidden)
			return
		}
		if checkoutRef == "" {
			checkoutRef = boundRef
		}
		if binding.claimRepo == nil || !binding.claimRepo(req.URL) {
			http.Error(w, "repo checkout capability already used for this repo", http.StatusConflict)
			return
		}

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		if d.repoCache.LookupContext(r.Context(), binding.workspaceID, req.URL) == "" {
			http.Error(w, "assigned repo cache is not ready", http.StatusConflict)
			return
		}

		result, err := d.repoCache.CreateWorktreeContext(r.Context(), repocache.WorktreeParams{
			WorkspaceID:         binding.workspaceID,
			RepoURL:             req.URL,
			WorkDir:             binding.workDir,
			Ref:                 checkoutRef,
			AgentName:           binding.agentName,
			TaskID:              binding.taskID,
			CoAuthoredByEnabled: binding.coAuthoredByEnabled,
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
