package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/daemon/trace"
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
	AgentName   string `json:"agent_name"`
	TaskID      string `json:"task_id"`
}

type traceResponse struct {
	TaskID string            `json:"task_id"`
	RunID  string            `json:"run_id"`
	Runs   []string          `json:"runs,omitempty"`
	Lines  []trace.TraceLine `json:"lines"`
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

func (d *Daemon) traceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if d.traceStore == nil {
			http.Error(w, "trace store not initialized", http.StatusServiceUnavailable)
			return
		}

		taskID := strings.TrimPrefix(r.URL.Path, "/traces/tasks/")
		stream := strings.HasSuffix(taskID, "/stream")
		if stream {
			taskID = strings.TrimSuffix(taskID, "/stream")
		}
		taskID = strings.Trim(taskID, "/")
		if taskID == "" || strings.Contains(taskID, "/") {
			http.Error(w, "task id is required", http.StatusBadRequest)
			return
		}
		if stream {
			d.streamTrace(w, r, taskID)
			return
		}

		runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
		runs, err := d.traceStore.ListRuns(r.Context(), taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if runID == "" {
			if len(runs) == 0 {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(traceResponse{TaskID: taskID, Lines: []trace.TraceLine{}})
				return
			}
			runID = runs[0]
		}

		afterSeq := int64(0)
		if raw := r.URL.Query().Get("after_seq"); raw != "" {
			afterSeq, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || afterSeq < 0 {
				http.Error(w, "invalid after_seq", http.StatusBadRequest)
				return
			}
		}

		var lines []trace.TraceLine
		if afterSeq > 0 {
			lines, err = d.traceStore.ListSince(r.Context(), taskID, runID, afterSeq)
		} else if tailRaw := r.URL.Query().Get("tail"); tailRaw != "" {
			tail, parseErr := strconv.Atoi(tailRaw)
			if parseErr != nil || tail < 0 {
				http.Error(w, "invalid tail", http.StatusBadRequest)
				return
			}
			lines, err = d.traceStore.Tail(r.Context(), taskID, runID, tail)
		} else {
			lines, err = d.traceStore.ListSince(r.Context(), taskID, runID, 0)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if lines == nil {
			lines = []trace.TraceLine{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(traceResponse{
			TaskID: taskID,
			RunID:  runID,
			Runs:   runs,
			Lines:  lines,
		})
	}
}

func (d *Daemon) streamTrace(w http.ResponseWriter, r *http.Request, taskID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	afterSeq := int64(0)
	if raw := r.URL.Query().Get("after_seq"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid after_seq", http.StatusBadRequest)
			return
		}
		afterSeq = parsed
	}
	tail := 300
	if raw := r.URL.Query().Get("tail"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid tail", http.StatusBadRequest)
			return
		}
		tail = parsed
	}

	sendEvent := func(event string, payload any) bool {
		data, err := json.Marshal(payload)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	sendReady := func(runs []string) bool {
		return sendEvent("ready", traceResponse{
			TaskID: taskID,
			RunID:  runID,
			Runs:   runs,
			Lines:  []trace.TraceLine{},
		})
	}

	sendLines := func(lines []trace.TraceLine) bool {
		for _, line := range lines {
			if !sendEvent("trace", line) {
				return false
			}
			if line.Seq > afterSeq {
				afterSeq = line.Seq
			}
		}
		return true
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	initialized := false
	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		runs, err := d.traceStore.ListRuns(r.Context(), taskID)
		if err != nil {
			_ = sendEvent("error", map[string]string{"error": err.Error()})
			return
		}
		if runID == "" && len(runs) > 0 {
			runID = runs[0]
		}
		if !initialized {
			if !sendReady(runs) {
				return
			}
			initialized = true
		}

		if runID != "" {
			var lines []trace.TraceLine
			if afterSeq == 0 && tail > 0 {
				lines, err = d.traceStore.Tail(r.Context(), taskID, runID, tail)
			} else {
				lines, err = d.traceStore.ListSince(r.Context(), taskID, runID, afterSeq)
			}
			if err != nil {
				_ = sendEvent("error", map[string]string{"error": err.Error()})
				return
			}
			if !sendLines(lines) {
				return
			}
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// serveHealth runs the health HTTP server on the given listener.
// Blocks until ctx is cancelled.
func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(startedAt))
	mux.HandleFunc("/shutdown", d.shutdownHandler())
	mux.HandleFunc("/traces/tasks/", d.traceHandler())

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
