package localenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	StateStarting = "starting"
	StateRunning  = "running"
	StateExited   = "exited"
	StateStopped  = "stopped"

	SourceConfig   = "config"
	SourceDetected = "detected"

	// Each environment gets ten consecutive ports, MULTICA_PORT through
	// MULTICA_PORT+9, so a web app and its API can run side by side and
	// parallel tasks never collide.
	portBlock  = 10
	portLow    = 41000
	portHigh   = 48990
	stateFile  = "state.json"
	outputFile = "output.log"

	// An ended environment stays in the snapshot long enough to be reported
	// as ended, then only its log is kept.
	endedRetention = time.Hour
	logRetention   = 7 * 24 * time.Hour
	readyTimeout   = 10 * time.Minute
	stopGrace      = 10 * time.Second
	stopCmdTimeout = time.Minute
)

// ErrUnsupported is returned on platforms without process groups.
var ErrUnsupported = errors.New("local environments are not supported on this platform yet")

// ErrNotFound means no live environment of that name belongs to the task.
var ErrNotFound = errors.New("no running environment with that name")

// Task identifies who asks for an environment. RootDir is the task's own
// directory; an environment can only be started from inside it.
type Task struct {
	TaskID      string
	IssueID     string
	WorkspaceID string
	RootDir     string
}

// Environment is one environment as the daemon reports and the CLI prints it.
type Environment struct {
	Key       string     `json:"key"`
	TaskID    string     `json:"task_id"`
	IssueID   string     `json:"issue_id"`
	Source    string     `json:"source"`
	Kind      string     `json:"kind,omitempty"`
	Name      string     `json:"name"`
	Label     string     `json:"label"`
	URL       string     `json:"url"`
	Port      int        `json:"port"`
	State     string     `json:"state"`
	Detail    string     `json:"detail"`
	StartedAt time.Time  `json:"started_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	LogPath   string     `json:"log_path,omitempty"`
	// Dir is the directory the environment runs from, kept local so GC can
	// leave it in place; it is never reported.
	Dir  string `json:"-"`
	pgid int
	// pid and bundle name a detected app's process for Focus; like Dir they
	// never leave this machine.
	pid    int
	bundle string
}

// record is what survives a daemon restart, in <dir>/<id>/state.json.
type record struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	IssueID     string     `json:"issue_id"`
	WorkspaceID string     `json:"workspace_id"`
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	RepoRoot    string     `json:"repo_root"`
	URL         string     `json:"url"`
	Ready       string     `json:"ready"`
	Stop        string     `json:"stop"`
	Port        int        `json:"port"`
	PID         int        `json:"pid"`
	Path        string     `json:"path"`
	State       string     `json:"state"`
	Detail      string     `json:"detail"`
	StartedAt   time.Time  `json:"started_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	// Adopted lists the sessions of environments of the same checkout that
	// ended while this one ran. What is left of them, such as an API both
	// used, is still in use and still declared.
	Adopted []int `json:"adopted,omitempty"`
}

type entry struct {
	rec      record
	stopping bool
	// leaderDone is closed once the start command's own process has exited.
	// It is nil for an environment adopted after a daemon restart, whose
	// liveness is checked on its process group instead.
	leaderDone chan struct{}
}

func (e *entry) live() bool { return e.rec.EndedAt == nil }

// Manager owns the environments this daemon started.
type Manager struct {
	dir    string
	logger *slog.Logger
	now    func() time.Time
	client *http.Client

	mu          sync.Mutex
	envs        map[string]*entry
	lastLogSwep time.Time
}

func NewManager(dir string, logger *slog.Logger) *Manager {
	return &Manager{
		dir:    dir,
		logger: logger,
		now:    time.Now,
		client: &http.Client{Timeout: 2 * time.Second},
		envs:   make(map[string]*entry),
	}
}

// Load adopts the environments a previous daemon process left running. They
// keep running across a daemon restart; the ones whose process group is gone
// are recorded as exited.
func (m *Manager) Load() error {
	dirs, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, d.Name(), stateFile))
		if err != nil {
			continue
		}
		var rec record
		if err := json.Unmarshal(data, &rec); err != nil || rec.ID != d.Name() {
			continue
		}
		m.envs[rec.ID] = &entry{rec: rec}
	}
	// Every record is loaded before any is ended, so a dead environment's
	// sessions can pass to a live one of the same checkout.
	for _, e := range m.envs {
		if e.live() && !groupAlive(e.rec.PID) {
			m.endLocked(e, StateExited, "stopped while the daemon was not running")
		}
	}
	for _, e := range m.envs {
		if e.live() && e.rec.State == StateStarting {
			go m.probe(e)
		}
	}
	return nil
}

// Start starts the named environment from the repository config found at or
// above dir. Starting one that is already running returns it unchanged.
func (m *Manager) Start(task Task, dir, name, pathEnv string) (Environment, error) {
	if !supported {
		return Environment{}, ErrUnsupported
	}
	root, err := filepath.EvalSymlinks(task.RootDir)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve task directory: %w", err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve directory: %w", err)
	}
	cfg, err := FindConfig(dir, root)
	if err != nil {
		return Environment{}, err
	}
	def, ok := cfg.Environments[name]
	if !ok {
		return Environment{}, fmt.Errorf("%s declares no %q environment (declared: %s)",
			ConfigPath, name, strings.Join(cfg.Names(), ", "))
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.envs {
		if e.live() && !e.stopping && sameOwner(e.rec, task) && e.rec.Name == name && e.rec.RepoRoot == cfg.RepoRoot {
			return m.viewLocked(e), nil
		}
	}
	port, err := m.allocatePortLocked()
	if err != nil {
		return Environment{}, err
	}
	id, err := randomID()
	if err != nil {
		return Environment{}, err
	}
	envDir := filepath.Join(m.dir, id)
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		return Environment{}, fmt.Errorf("create environment directory: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(envDir, outputFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Environment{}, fmt.Errorf("open environment log: %w", err)
	}
	now := m.now()
	rec := record{
		ID: id, TaskID: task.TaskID, IssueID: task.IssueID, WorkspaceID: task.WorkspaceID,
		Name: name, Label: def.Label, RepoRoot: cfg.RepoRoot,
		URL: expandPort(def.URL, port), Ready: expandPort(def.Ready, port), Stop: def.Stop,
		Port: port, Path: pathEnv, State: StateStarting,
		StartedAt: now, ExpiresAt: now.Add(def.TTL),
	}
	cmd := shellCommand(def.Start)
	cmd.Dir = cfg.RepoRoot
	cmd.Env = childEnv(pathEnv, port, name)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		os.RemoveAll(envDir)
		return Environment{}, fmt.Errorf("start %q: %w", name, err)
	}
	rec.PID = cmd.Process.Pid
	e := &entry{rec: rec, leaderDone: make(chan struct{})}
	m.envs[id] = e
	m.persistLocked(e)
	m.logger.Info("environment started", "name", name, "id", id, "port", port, "task_id", task.TaskID)

	go m.wait(e, cmd, logFile)
	go m.probe(e)
	return m.viewLocked(e), nil
}

// WaitReady blocks until the environment stops starting or ctx ends, and
// returns its latest view.
func (m *Manager) WaitReady(ctx context.Context, key string) Environment {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		var view Environment
		for _, e := range m.envs {
			if "config:"+e.rec.ID == key {
				view = m.viewLocked(e)
			}
		}
		m.mu.Unlock()
		if view.State != StateStarting {
			return view
		}
		select {
		case <-ctx.Done():
			return view
		case <-ticker.C:
		}
	}
}

// Stop stops the task's live environment with this name.
func (m *Manager) Stop(task Task, name string) (Environment, error) {
	m.mu.Lock()
	var target *entry
	for _, e := range m.envs {
		if e.live() && sameOwner(e.rec, task) && e.rec.Name == name {
			target = e
			break
		}
	}
	m.mu.Unlock()
	if target == nil {
		return Environment{}, ErrNotFound
	}
	m.stop(target, "")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.viewLocked(target), nil
}

// List returns the task's environments, including recently ended ones.
func (m *Manager) List(task Task) []Environment {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Environment
	for _, e := range m.envs {
		if sameOwner(e.rec, task) {
			out = append(out, m.viewLocked(e))
		}
	}
	sortEnvironments(out)
	return out
}

// Snapshot returns every environment the manager still reports.
func (m *Manager) Snapshot() []Environment {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Environment, 0, len(m.envs))
	for _, e := range m.envs {
		out = append(out, m.viewLocked(e))
	}
	sortEnvironments(out)
	return out
}

// Tick expires environments past their TTL, notices adopted ones that died,
// and forgets ended ones once they have been reported.
func (m *Manager) Tick() {
	now := m.now()
	var expired []*entry
	m.mu.Lock()
	for id, e := range m.envs {
		switch {
		case e.live() && !e.stopping && now.After(e.rec.ExpiresAt):
			e.stopping = true
			expired = append(expired, e)
		case e.live() && e.leaderGone() && !groupAlive(e.rec.PID):
			m.endLocked(e, StateExited, "")
		case !e.live() && now.Sub(*e.rec.EndedAt) > endedRetention:
			delete(m.envs, id)
			os.Remove(filepath.Join(m.dir, id, stateFile))
		}
	}
	sweepLogs := now.Sub(m.lastLogSwep) > time.Hour
	if sweepLogs {
		m.lastLogSwep = now
	}
	m.mu.Unlock()
	for _, e := range expired {
		go m.stop(e, "expired")
	}
	if sweepLogs {
		m.sweepLogs(now)
	}
}

// Owner returns the key of the live environment that pid belongs to, or ""
// when it belongs to none, so detection does not list an environment's own
// servers a second time. A process belongs to an environment through its
// process group or, once a script has detached it into a group of its own,
// through its session.
func (m *Manager) Owner(pid int) string {
	pgid, sid := processGroup(pid), processSession(pid)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.envs {
		if !e.live() {
			continue
		}
		for _, leader := range append([]int{e.rec.PID}, e.rec.Adopted...) {
			if leader > 0 && (leader == pgid || leader == sid) {
				return "config:" + e.rec.ID
			}
		}
	}
	return ""
}

func (e *entry) leaderGone() bool {
	if e.leaderDone == nil {
		return true
	}
	select {
	case <-e.leaderDone:
		return true
	default:
		return false
	}
}

func (m *Manager) wait(e *entry, cmd *exec.Cmd, logFile *os.File) {
	err := cmd.Wait()
	logFile.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	close(e.leaderDone)
	// A start command that hands its servers off to children and returns
	// leaves them in its process group; the environment lives as long as the
	// group does, which Tick checks.
	if e.live() && !e.stopping && groupAlive(e.rec.PID) {
		return
	}
	if e.live() {
		state, detail := StateExited, exitDetail(err)
		if e.stopping {
			state, detail = StateStopped, e.rec.Detail
		}
		m.endLocked(e, state, detail)
	}
}

// probe marks the environment running once its ready URL answers with a
// status below 500. A 404 still counts, since a server with no route at "/"
// is up; a 5xx is a server still warming up or failing its health check. An
// environment with no URL is running as soon as it starts.
func (m *Manager) probe(e *entry) {
	m.mu.Lock()
	ready := e.rec.Ready
	m.mu.Unlock()
	if ready == "" {
		m.setRunning(e)
		return
	}
	detail := "not answering at " + ready
	deadline := m.now().Add(readyTimeout)
	for m.now().Before(deadline) {
		m.mu.Lock()
		live := e.live() && !e.stopping
		m.mu.Unlock()
		if !live {
			return
		}
		if resp, err := m.client.Get(ready); err == nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode < http.StatusInternalServerError {
				m.setRunning(e)
				return
			}
			detail = fmt.Sprintf("%s answered %d", ready, resp.StatusCode)
		}
		time.Sleep(time.Second)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.live() && e.rec.State == StateStarting {
		e.rec.Detail = detail
		m.persistLocked(e)
	}
}

func (m *Manager) setRunning(e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.live() && e.rec.State == StateStarting {
		e.rec.State = StateRunning
		e.rec.Detail = ""
		m.persistLocked(e)
	}
}

// stop runs the environment's stop command, then signals whatever is left
// of its process group.
func (m *Manager) stop(e *entry, detail string) {
	m.mu.Lock()
	if !e.live() {
		m.mu.Unlock()
		return
	}
	e.stopping = true
	if detail != "" {
		e.rec.Detail = detail
	}
	rec := e.rec
	m.mu.Unlock()

	if rec.Stop != "" {
		ctx, cancel := context.WithTimeout(context.Background(), stopCmdTimeout)
		cmd := shellCommand(rec.Stop)
		cmd.Dir = rec.RepoRoot
		cmd.Env = childEnv(rec.Path, rec.Port, rec.Name)
		setProcessGroup(cmd)
		if out, err := runWithContext(ctx, cmd); err != nil {
			m.logger.Warn("environment stop command failed", "name", rec.Name, "id", rec.ID, "error", err, "output", strings.TrimSpace(string(out)))
		}
		cancel()
	}
	if groupAlive(rec.PID) {
		_ = signalGroup(rec.PID, syscall.SIGTERM)
		deadline := time.Now().Add(stopGrace)
		for groupAlive(rec.PID) && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
		if groupAlive(rec.PID) {
			_ = signalGroup(rec.PID, syscall.SIGKILL)
		}
	}
	if e.leaderDone != nil {
		select {
		case <-e.leaderDone:
		case <-time.After(2 * time.Second):
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.live() {
		m.endLocked(e, StateStopped, e.rec.Detail)
	}
	m.logger.Info("environment stopped", "name", rec.Name, "id", rec.ID)
}

func (m *Manager) endLocked(e *entry, state, detail string) {
	now := m.now()
	e.rec.State = state
	e.rec.Detail = detail
	e.rec.EndedAt = &now
	m.persistLocked(e)
	if heir := m.heirLocked(e); heir != nil {
		heir.rec.Adopted = append(heir.rec.Adopted, e.rec.PID)
		heir.rec.Adopted = append(heir.rec.Adopted, e.rec.Adopted...)
		m.persistLocked(heir)
	}
}

// heirLocked picks the live environment that takes over an ended one's
// sessions: the earliest started from the same checkout, for the same owner.
func (m *Manager) heirLocked(ended *entry) *entry {
	owner := Task{TaskID: ended.rec.TaskID, IssueID: ended.rec.IssueID, WorkspaceID: ended.rec.WorkspaceID}
	var heir *entry
	for _, e := range m.envs {
		if e == ended || !e.live() || e.rec.RepoRoot != ended.rec.RepoRoot || !sameOwner(e.rec, owner) {
			continue
		}
		if heir == nil || e.rec.StartedAt.Before(heir.rec.StartedAt) {
			heir = e
		}
	}
	return heir
}

func (m *Manager) persistLocked(e *entry) {
	data, err := json.MarshalIndent(e.rec, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(m.dir, e.rec.ID, stateFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		m.logger.Warn("persist environment state failed", "id", e.rec.ID, "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		m.logger.Warn("persist environment state failed", "id", e.rec.ID, "error", err)
	}
}

func (m *Manager) viewLocked(e *entry) Environment {
	rec := e.rec
	view := Environment{
		Key: "config:" + rec.ID, TaskID: rec.TaskID, IssueID: rec.IssueID,
		Source: SourceConfig, Kind: KindServer, Name: rec.Name, Label: rec.Label, URL: rec.URL, Port: rec.Port,
		State: rec.State, Detail: rec.Detail, StartedAt: rec.StartedAt,
		LogPath: filepath.Join(m.dir, rec.ID, outputFile), Dir: rec.RepoRoot, pgid: rec.PID,
	}
	if e.live() {
		expires := rec.ExpiresAt
		view.ExpiresAt = &expires
	}
	return view
}

func (m *Manager) allocatePortLocked() (int, error) {
	taken := make(map[int]bool)
	for _, e := range m.envs {
		if e.live() {
			taken[e.rec.Port] = true
		}
	}
	for base := portLow; base <= portHigh; base += portBlock {
		if taken[base] {
			continue
		}
		free := true
		for p := base; p < base+portBlock && free; p++ {
			free = portFree(p)
		}
		if free {
			return base, nil
		}
	}
	return 0, errors.New("no free port block for the environment")
}

func (m *Manager) sweepLogs(now time.Time) {
	dirs, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	m.mu.Lock()
	known := make(map[string]bool, len(m.envs))
	for id := range m.envs {
		known[id] = true
	}
	m.mu.Unlock()
	for _, d := range dirs {
		if !d.IsDir() || known[d.Name()] {
			continue
		}
		info, err := os.Stat(filepath.Join(m.dir, d.Name(), outputFile))
		if err == nil && now.Sub(info.ModTime()) < logRetention {
			continue
		}
		os.RemoveAll(filepath.Join(m.dir, d.Name()))
	}
}

// sameOwner scopes an environment to its issue, so a later task on the same
// issue can see and stop what an earlier one started. A task with no issue
// owns only its own.
func sameOwner(rec record, task Task) bool {
	if task.IssueID != "" {
		return rec.IssueID == task.IssueID && rec.WorkspaceID == task.WorkspaceID
	}
	return rec.TaskID == task.TaskID
}

func portFree(port int) bool {
	for _, host := range []string{"127.0.0.1", "", "::1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
		if err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				return false
			}
			continue
		}
		ln.Close()
	}
	return true
}

// childEnv is the daemon's environment without the Multica identity a task
// carries, plus the caller's PATH so the repository's toolchain resolves.
func childEnv(pathEnv string, port int, name string) []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "MULTICA_") || (pathEnv != "" && key == "PATH") {
			continue
		}
		env = append(env, kv)
	}
	if pathEnv != "" {
		env = append(env, "PATH="+pathEnv)
	}
	return append(env, fmt.Sprintf("MULTICA_PORT=%d", port), "MULTICA_ENVIRONMENT="+name)
}

// exitDetail says how the start command ended and nothing it printed: the
// detail is reported to the server, and output can carry secrets such as a
// connection string. The output stays in the local log.
func exitDetail(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Error()
	}
	return ""
}

func runWithContext(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return []byte(out.String()), err
	case <-ctx.Done():
		_ = signalGroup(cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return []byte(out.String()), ctx.Err()
	}
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sortEnvironments(envs []Environment) {
	sort.Slice(envs, func(i, j int) bool {
		if !envs[i].StartedAt.Equal(envs[j].StartedAt) {
			return envs[i].StartedAt.Before(envs[j].StartedAt)
		}
		return envs[i].Key < envs[j].Key
	})
}
