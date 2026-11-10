package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/localenv"
)

const (
	environmentTickInterval = 15 * time.Second
	// Detection shells out to lsof on macOS, so it runs at half the tick rate.
	environmentDetectEvery = 2
	// A live snapshot is re-sent at least this often so the server can tell a
	// daemon that went away from one that has nothing new to say.
	environmentHeartbeat    = time.Minute
	environmentReportedFile = "reported.json"
	environmentStartTimeout = 10 * time.Minute
)

type environmentReporter interface {
	ReportTaskEnvironments(ctx context.Context, taskID string, envs []TaskEnvironmentReport) error
}

// environmentService supervises the environments tasks start with
// `multica env start`, detects servers agents started on their own, and
// reports both to the issue each task belongs to.
type environmentService struct {
	manager        *localenv.Manager
	detector       *localenv.Detector
	client         environmentReporter
	logger         *slog.Logger
	workspacesRoot string
	stateDir       string
	wake           chan struct{}

	mu sync.Mutex
	// tasks maps the env root of each running task to its identity, so a
	// server started inside it is attributed before .gc_meta.json exists.
	tasks    map[string]localenv.Task
	detected []localenv.Environment
	// sticky keeps a detected server on the task that first reported it when
	// a later task reuses the same directory.
	sticky map[string]string
	// reported is owned by the report loop: task ID → the last snapshot sent.
	reported map[string]reportedSnapshot
}

type reportedSnapshot struct {
	fingerprint string
	sentAt      time.Time
}

func newEnvironmentService(cfg Config, client environmentReporter, ignoreCommands []string, logger *slog.Logger) *environmentService {
	if strings.TrimSpace(cfg.WorkspacesRoot) == "" {
		return nil
	}
	root := cfg.WorkspacesRoot
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	stateDir := filepath.Join(cfg.WorkspacesRoot, ".environments", "v1")
	return &environmentService{
		manager:        localenv.NewManager(stateDir, logger),
		detector:       localenv.NewDetector(ignoreCommands),
		client:         client,
		logger:         logger,
		workspacesRoot: root,
		stateDir:       stateDir,
		wake:           make(chan struct{}, 1),
		tasks:          make(map[string]localenv.Task),
		sticky:         make(map[string]string),
		reported:       make(map[string]reportedSnapshot),
	}
}

// environmentIgnoreCommands names the processes detection never reports:
// the agent CLIs, whose own helper servers are not the task's environment.
func environmentIgnoreCommands(agents map[string]AgentEntry) []string {
	out := make([]string, 0, len(agents)*2)
	for name, entry := range agents {
		out = append(out, name)
		if entry.Path != "" {
			out = append(out, strings.TrimSuffix(filepath.Base(entry.Path), filepath.Ext(entry.Path)))
		}
		if entry.Command != "" {
			out = append(out, filepath.Base(entry.Command))
		}
	}
	return out
}

func (s *environmentService) trackTask(envRoot string, task localenv.Task) func() {
	if s == nil || envRoot == "" {
		return func() {}
	}
	if resolved, err := filepath.EvalSymlinks(envRoot); err == nil {
		envRoot = resolved
	}
	task.RootDir = envRoot
	s.mu.Lock()
	s.tasks[envRoot] = task
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		if s.tasks[envRoot].TaskID == task.TaskID {
			delete(s.tasks, envRoot)
		}
		s.mu.Unlock()
	}
}

// servesFrom reports whether a live environment runs from inside dir, so GC
// leaves the directory alone until it stops.
func (s *environmentService) servesFrom(dir string) bool {
	if s == nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for _, env := range s.manager.Snapshot() {
		if (env.State == localenv.StateStarting || env.State == localenv.StateRunning) && pathWithin(env.Dir, dir) {
			return true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, env := range s.detected {
		if pathWithin(env.Dir, dir) {
			return true
		}
	}
	return false
}

// focus brings forward the desktop app last detected under key.
func (s *environmentService) focus(ctx context.Context, key string) error {
	s.mu.Lock()
	var target *localenv.Environment
	for i := range s.detected {
		if s.detected[i].Key == key && s.detected[i].Kind == localenv.KindApp {
			env := s.detected[i]
			target = &env
			break
		}
	}
	s.mu.Unlock()
	if target == nil {
		return localenv.ErrAppGone
	}
	return target.Focus(ctx)
}

func (s *environmentService) nudge() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *environmentService) run(ctx context.Context) {
	if s == nil {
		return
	}
	if err := s.manager.Load(); err != nil {
		s.logger.Warn("environments: load state failed", "error", err)
	}
	s.loadReported()
	ticker := time.NewTicker(environmentTickInterval)
	defer ticker.Stop()
	for tick := 0; ; tick++ {
		s.manager.Tick()
		if tick%environmentDetectEvery == 0 {
			s.detect(ctx)
		}
		s.report(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
			// A start or stop just happened; report it without detecting again.
			tick--
		}
	}
}

func (s *environmentService) detect(ctx context.Context) {
	found, err := s.detector.Detect(ctx, s.resolve, s.manager.Owns)
	if err != nil {
		s.logger.Debug("environments: detection failed", "error", err)
		return
	}
	s.mu.Lock()
	s.detected = found
	s.mu.Unlock()
}

// resolve attributes a working directory to the issue task whose env root
// contains it: a running task from the registry, else a finished one from
// its .gc_meta.json.
func (s *environmentService) resolve(cwd string) (localenv.Task, bool) {
	cwd = filepath.Clean(cwd)
	s.mu.Lock()
	for root, task := range s.tasks {
		if pathWithin(cwd, root) {
			s.mu.Unlock()
			return task, task.IssueID != ""
		}
	}
	s.mu.Unlock()

	rel, err := filepath.Rel(s.workspacesRoot, cwd)
	if err != nil || !filepath.IsLocal(rel) {
		return localenv.Task{}, false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || strings.HasPrefix(parts[0], ".") {
		return localenv.Task{}, false
	}
	envRoot := filepath.Join(s.workspacesRoot, parts[0], parts[1])
	meta, err := execenv.ReadGCMeta(envRoot)
	if err != nil || meta.Kind != execenv.GCKindIssue || meta.IssueID == "" || meta.TaskID == "" {
		return localenv.Task{}, false
	}
	return localenv.Task{TaskID: meta.TaskID, IssueID: meta.IssueID, WorkspaceID: meta.WorkspaceID, RootDir: envRoot}, true
}

// report sends each task's snapshot when it changed, as a heartbeat while it
// is not empty, and once more as empty after its last environment is gone.
func (s *environmentService) report(ctx context.Context, now time.Time) {
	groups := make(map[string][]localenv.Environment)
	for _, env := range s.manager.Snapshot() {
		if env.IssueID != "" {
			groups[env.TaskID] = append(groups[env.TaskID], env)
		}
	}
	s.mu.Lock()
	live := make(map[string]bool, len(s.detected))
	for _, env := range s.detected {
		live[env.Key] = true
		if owner, ok := s.sticky[env.Key]; ok {
			env.TaskID = owner
		} else {
			s.sticky[env.Key] = env.TaskID
		}
		groups[env.TaskID] = append(groups[env.TaskID], env)
	}
	for key := range s.sticky {
		if !live[key] {
			delete(s.sticky, key)
		}
	}
	s.mu.Unlock()
	for taskID := range s.reported {
		if _, ok := groups[taskID]; !ok {
			groups[taskID] = nil
		}
	}

	taskIDs := make([]string, 0, len(groups))
	for taskID := range groups {
		taskIDs = append(taskIDs, taskID)
	}
	sort.Strings(taskIDs)
	changed := false
	for _, taskID := range taskIDs {
		envs := groups[taskID]
		fingerprint := environmentFingerprint(envs)
		prev, had := s.reported[taskID]
		if !had && len(envs) == 0 {
			continue
		}
		if had && prev.fingerprint == fingerprint && (len(envs) == 0 || now.Sub(prev.sentAt) < environmentHeartbeat) {
			continue
		}
		err := s.client.ReportTaskEnvironments(ctx, taskID, toEnvironmentReports(envs))
		var reqErr *requestError
		switch {
		case err == nil:
		case errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusNotFound || reqErr.StatusCode == http.StatusForbidden):
			// The task is gone or belongs to a workspace this daemon left;
			// nothing will ever accept its snapshot.
			s.logger.Debug("environments: snapshot refused", "task_id", taskID, "status", reqErr.StatusCode)
			envs = nil
		default:
			if ctx.Err() == nil {
				s.logger.Warn("environments: report failed", "task_id", taskID, "error", err)
			}
			continue
		}
		if len(envs) == 0 {
			delete(s.reported, taskID)
		} else {
			s.reported[taskID] = reportedSnapshot{fingerprint: fingerprint, sentAt: now}
		}
		changed = changed || had != (len(envs) > 0)
	}
	if changed {
		s.saveReported()
	}
}

// The reported task set survives a restart, so the environments a stopped
// daemon reported are closed out instead of turning unreachable forever.
func (s *environmentService) loadReported() {
	data, err := os.ReadFile(filepath.Join(s.stateDir, environmentReportedFile))
	if err != nil {
		return
	}
	var taskIDs []string
	if json.Unmarshal(data, &taskIDs) != nil {
		return
	}
	for _, id := range taskIDs {
		s.reported[id] = reportedSnapshot{fingerprint: "restarted"}
	}
}

func (s *environmentService) saveReported() {
	taskIDs := make([]string, 0, len(s.reported))
	for id := range s.reported {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	data, _ := json.Marshal(taskIDs)
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return
	}
	path := filepath.Join(s.stateDir, environmentReportedFile)
	if err := os.WriteFile(path+".tmp", data, 0o600); err == nil {
		os.Rename(path+".tmp", path)
	}
}

func environmentFingerprint(envs []localenv.Environment) string {
	parts := make([]string, 0, len(envs))
	for _, env := range envs {
		expires := ""
		if env.ExpiresAt != nil {
			expires = env.ExpiresAt.UTC().Format(time.RFC3339)
		}
		parts = append(parts, strings.Join([]string{env.Key, env.Kind, env.State, env.URL, env.Label, env.Detail, expires}, "\x1f"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x1e")
}

func toEnvironmentReports(envs []localenv.Environment) []TaskEnvironmentReport {
	out := make([]TaskEnvironmentReport, 0, len(envs))
	for _, env := range envs {
		out = append(out, TaskEnvironmentReport{
			Key: env.Key, Source: env.Source, Kind: env.Kind, Name: env.Name, Label: env.Label, URL: env.URL,
			Port: env.Port, State: env.State, Detail: env.Detail, StartedAt: env.StartedAt, ExpiresAt: env.ExpiresAt,
		})
	}
	return out
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// Local HTTP API used by `multica environment`. Identity comes from the
// task-scoped bearer token, exactly as for /repo/checkout.

type environmentStartRequest struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	Path string `json:"path"`
	Wait bool   `json:"wait"`
}

type environmentStopRequest struct {
	Name string `json:"name"`
}

type environmentResponse struct {
	Environment  *localenv.Environment  `json:"environment,omitempty"`
	Environments []localenv.Environment `json:"environments,omitempty"`
}

func (d *Daemon) environmentTask(w http.ResponseWriter, r *http.Request) (localenv.Task, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return localenv.Task{}, false
	}
	if d.environments == nil {
		http.Error(w, "environments are not available on this daemon", http.StatusServiceUnavailable)
		return localenv.Task{}, false
	}
	active, result := d.activeRepoCheckoutTask(r)
	if result != repoCheckoutAuthOK {
		http.Error(w, "environment commands need the credential of a task running in this daemon; "+
			"run them from inside the task, while it is running", http.StatusUnauthorized)
		return localenv.Task{}, false
	}
	return localenv.Task{TaskID: active.TaskID, IssueID: active.IssueID, WorkspaceID: active.WorkspaceID, RootDir: active.WorkDir}, true
}

func (d *Daemon) environmentStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		task, ok := d.environmentTask(w, r)
		if !ok {
			return
		}
		var req environmentStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Dir == "" {
			req.Dir = task.RootDir
		}
		env, err := d.environments.manager.Start(task, req.Dir, strings.TrimSpace(req.Name), req.Path)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, localenv.ErrUnsupported) {
				status = http.StatusNotImplemented
			}
			http.Error(w, err.Error(), status)
			return
		}
		d.environments.nudge()
		if req.Wait {
			ctx, cancel := context.WithTimeout(r.Context(), environmentStartTimeout)
			env = d.environments.manager.WaitReady(ctx, env.Key)
			cancel()
			d.environments.nudge()
		}
		writeEnvironmentResponse(w, environmentResponse{Environment: &env})
	}
}

func (d *Daemon) environmentStopHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		task, ok := d.environmentTask(w, r)
		if !ok {
			return
		}
		var req environmentStopRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		env, err := d.environments.manager.Stop(task, strings.TrimSpace(req.Name))
		if errors.Is(err, localenv.ErrNotFound) {
			http.Error(w, fmt.Sprintf("no running environment named %q on this issue", req.Name), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.environments.nudge()
		writeEnvironmentResponse(w, environmentResponse{Environment: &env})
	}
}

func (d *Daemon) environmentListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		task, ok := d.environmentTask(w, r)
		if !ok {
			return
		}
		envs := d.environments.manager.List(task)
		if envs == nil {
			envs = []localenv.Environment{}
		}
		writeEnvironmentResponse(w, environmentResponse{Environments: envs})
	}
}

type environmentFocusRequest struct {
	Key string `json:"key"`
}

// environmentFocusHandler brings a detected desktop app's window forward.
// Desktop calls it on behalf of whoever sits at this machine, so unlike its
// siblings there is no task credential to check. A request can only raise a
// window this daemon already found in a task directory. Browsers cannot send
// it: a page's POST always carries an Origin header, and a JSON body needs a
// CORS preflight this server never answers.
func (d *Daemon) environmentFocusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Header.Get("Origin") != "" || mediaType != "application/json" {
			writeEnvironmentFocusError(w, http.StatusForbidden, "forbidden", "focus requests come from Desktop, as JSON, without an Origin")
			return
		}
		if d.environments == nil {
			writeEnvironmentFocusError(w, http.StatusServiceUnavailable, "unavailable", "environments are not available on this daemon")
			return
		}
		var req environmentFocusRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			writeEnvironmentFocusError(w, http.StatusBadRequest, "invalid", "invalid request body: "+err.Error())
			return
		}
		err := d.environments.focus(r.Context(), strings.TrimSpace(req.Key))
		switch {
		case err == nil:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"focused": true})
		case errors.Is(err, localenv.ErrAppGone):
			writeEnvironmentFocusError(w, http.StatusNotFound, "gone", err.Error())
		case errors.Is(err, localenv.ErrFocusUnsupported):
			writeEnvironmentFocusError(w, http.StatusNotImplemented, "unsupported", err.Error())
		default:
			d.logger.Warn("environments: focus failed", "error", err)
			writeEnvironmentFocusError(w, http.StatusInternalServerError, "failed", err.Error())
		}
	}
}

// The reason lets Desktop tell the user what happened; a daemon too old for
// this endpoint answers the mux's plain-text 404 instead.
func writeEnvironmentFocusError(w http.ResponseWriter, status int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"reason": reason, "error": message})
}

func writeEnvironmentResponse(w http.ResponseWriter, resp environmentResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
