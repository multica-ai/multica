package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	ReloadPendingReason string           `json:"reload_pending_reason,omitempty"`
	GCPolicy            GCPolicySnapshot `json:"gc_policy"`
	// WorkspacesRoot is the normalized root this running daemon actually owns.
	// Local diagnostics compare it with the directory they scanned before
	// applying this daemon's GC policy; profile config and process environment
	// can change independently after the daemon starts.
	WorkspacesRoot string            `json:"workspaces_root"`
	Workspaces     []healthWorkspace `json:"workspaces"`
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

type activeRepoCheckoutTask struct {
	WorkspaceID string
	TaskID      string
	AgentID     string
	AgentName   string
	WorkDir     string
}

// registerActiveRepoCheckoutTask binds checkout identity to the active task.
// The token prevents unauthenticated localhost callers from choosing another
// task's identity or workdir. It is not an OS-user isolation boundary: another
// process that can steal the child's environment token can authenticate as
// that task and already holds its API credential.
func (d *Daemon) registerActiveRepoCheckoutTask(token string, task activeRepoCheckoutTask) {
	d.repoCheckoutTasksMu.Lock()
	defer d.repoCheckoutTasksMu.Unlock()
	if d.repoCheckoutTasks == nil {
		d.repoCheckoutTasks = make(map[string]activeRepoCheckoutTask)
	}
	d.repoCheckoutTasks[token] = task
}

func (d *Daemon) clearActiveRepoCheckoutTask(token string) {
	d.repoCheckoutTasksMu.Lock()
	delete(d.repoCheckoutTasks, token)
	d.repoCheckoutTasksMu.Unlock()
}

// repoCheckoutAuthResult distinguishes the two ways /repo/checkout auth fails.
// They look identical to a caller but have completely different fixes, and
// collapsing them into one 401 is what made #7520 take 13 hours to diagnose.
type repoCheckoutAuthResult int

const (
	repoCheckoutAuthOK repoCheckoutAuthResult = iota
	// repoCheckoutAuthNoCredential: the request carried no usable Authorization
	// header. The current CLI always sends one, so in practice this is a
	// `multica` binary older than repoCheckoutMinCLIVersion — a permanent
	// failure that no retry fixes.
	repoCheckoutAuthNoCredential
	// repoCheckoutAuthUnknownCredential: a token was presented but is not bound
	// to a task running in THIS daemon — the task already finished, or the
	// daemon restarted while the agent process kept running.
	repoCheckoutAuthUnknownCredential
)

// repoCheckoutMinCLIVersion is the first release whose `multica repo checkout`
// sends the Authorization header this endpoint requires. The header and the
// check landed in the same commit (#7205), so every older CLI is rejected here
// no matter how healthy the task is.
const repoCheckoutMinCLIVersion = "v0.4.30"

func (d *Daemon) activeRepoCheckoutTask(r *http.Request) (activeRepoCheckoutTask, repoCheckoutAuthResult) {
	const bearer = "Bearer "
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, bearer) {
		return activeRepoCheckoutTask{}, repoCheckoutAuthNoCredential
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, bearer))
	if token == "" {
		return activeRepoCheckoutTask{}, repoCheckoutAuthNoCredential
	}
	d.repoCheckoutTasksMu.RLock()
	task, ok := d.repoCheckoutTasks[token]
	d.repoCheckoutTasksMu.RUnlock()
	if !ok {
		return activeRepoCheckoutTask{}, repoCheckoutAuthUnknownCredential
	}
	return task, repoCheckoutAuthOK
}

// repoCheckoutListBinariesCommand names the platform's "show every copy on
// PATH" command. The whole point of the rejection is that the caller ran the
// wrong binary, so handing them a command their shell does not have just moves
// the dead end.
func repoCheckoutListBinariesCommand() string {
	if runtime.GOOS == "windows" {
		return "where.exe multica"
	}
	return "which -a multica"
}

// repoCheckoutAuthErrorMessage builds the rejection body. It has to be
// self-explanatory: the caller is an agent inside a task, and the CLI prints
// this string as the whole failure. Anything it does not say has to be
// reconstructed from daemon logs the agent cannot read.
//
// Compatibility advice deliberately lives HERE rather than in the CLI. The
// population that hits repoCheckoutAuthNoCredential is, by definition, running
// a CLI too old to have received any client-side fix; the daemon is the only
// component on this path guaranteed to be current.
//
// Every claim here has to survive Windows and Linux as well as macOS, and has
// to be true rather than merely likely — advice that is wrong on the reader's
// platform, or that asserts a version match nobody verified, recreates exactly
// the misdirection this message exists to end.
func (d *Daemon) repoCheckoutAuthErrorMessage(result repoCheckoutAuthResult) string {
	if result == repoCheckoutAuthUnknownCredential {
		return "repo checkout credential is not bound to a task running in this daemon: " +
			"the task it belongs to has already finished, or the daemon restarted after this agent started. " +
			"Repo checkout is only available while the task that owns this workdir is still running."
	}

	var b strings.Builder
	b.WriteString("repo checkout requires an active task credential, and this request carried none. ")
	b.WriteString("The multica CLI has sent that credential since ")
	b.WriteString(repoCheckoutMinCLIVersion)
	b.WriteString(", so this request came from an older `multica` binary")
	if daemonVersion := strings.TrimSpace(d.cfg.CLIVersion); daemonVersion != "" {
		b.WriteString(" (this daemon runs ")
		b.WriteString(daemonVersion)
		b.WriteString(")")
	}
	b.WriteString(" and will fail every time, not intermittently. List every copy on PATH with `")
	b.WriteString(repoCheckoutListBinariesCommand())
	b.WriteString("`, check each one's version, then upgrade the stale one with `multica update`")
	b.WriteString(" — it handles Homebrew and direct installs on every platform.")
	// os.Executable() reports where this process was STARTED from, which is not
	// a promise about the bytes on disk now: the daemon deliberately supports a
	// binary being replaced out of band while a restart is deferred (see
	// trySelfReload). Claiming a version match here could hand a version-skew
	// victim a second stale binary, so state the provenance and let the reader
	// verify the version.
	if selfBin, err := resolveSelfExecutable(); err == nil {
		b.WriteString(" This daemon started from ")
		b.WriteString(selfBin)
		if pending := d.reloadPending(); pending != "" {
			b.WriteString(", whose on-disk copy has since changed (")
			b.WriteString(pending)
			b.WriteString(")")
		}
		b.WriteString("; check that copy's version before running it directly.")
	}
	return b.String()
}

// writeRepoCheckoutAuthError rejects the request and leaves a trace in
// daemon.log. Before this, the 401 branch logged nothing at all, so a failure
// the agent saw on stderr was invisible to whoever went looking for it (#7520).
// The token is never logged: it is a live task credential.
func (d *Daemon) writeRepoCheckoutAuthError(w http.ResponseWriter, result repoCheckoutAuthResult) {
	message := d.repoCheckoutAuthErrorMessage(result)
	reason := "no_credential"
	if result == repoCheckoutAuthUnknownCredential {
		reason = "unknown_credential"
	}
	d.logger.Warn("repo checkout rejected",
		"reason", reason,
		"min_cli_version", repoCheckoutMinCLIVersion,
		"daemon_version", d.cfg.CLIVersion,
	)
	http.Error(w, message, http.StatusUnauthorized)
}

func authorizeRepoCheckoutWorkDir(activeRoot, requested string) (string, error) {
	root, err := filepath.Abs(activeRoot)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	workdir, err := filepath.Abs(requested)
	if err != nil {
		return "", err
	}
	workdir, err = filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, workdir)
	if err != nil || !filepath.IsLocal(rel) {
		return "", errors.New("workdir is outside the active task workdir")
	}
	return workdir, nil
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
			WorkspacesRoot:      normalizedHealthWorkspacesRoot(d.cfg.WorkspacesRoot),
			GCPolicy: GCPolicySnapshot{
				Enabled:                 d.cfg.GCEnabled,
				TTLSeconds:              int64(d.cfg.GCTTL / time.Second),
				CompletedTaskTTLSeconds: int64(d.cfg.GCCompletedTaskTTL / time.Second),
				OrphanTTLSeconds:        int64(d.cfg.GCOrphanTTL / time.Second),
				ArtifactTTLSeconds:      int64(d.cfg.GCArtifactTTL / time.Second),
			},
			Workspaces: wsList,
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

func normalizedHealthWorkspacesRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
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
		activeTask, authResult := d.activeRepoCheckoutTask(r)
		if authResult != repoCheckoutAuthOK {
			d.writeRepoCheckoutAuthError(w, authResult)
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
		if req.WorkspaceID != activeTask.WorkspaceID || req.TaskID != activeTask.TaskID {
			http.Error(w, "repo checkout task context does not match the active task", http.StatusForbidden)
			return
		}
		authorizedWorkDir, authErr := authorizeRepoCheckoutWorkDir(activeTask.WorkDir, req.WorkDir)
		if authErr != nil {
			http.Error(w, "repo checkout workdir is not owned by the active task", http.StatusForbidden)
			return
		}
		// Identity is derived from the token-bound active task. AgentName and the
		// other caller-supplied fields are compatibility inputs only and never
		// decide branch ownership.
		req.WorkspaceID = activeTask.WorkspaceID
		req.TaskID = activeTask.TaskID
		req.AgentName = activeTask.AgentName
		req.WorkDir = authorizedWorkDir

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
