package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	PreviewVisibilityPrivate = "private"
	PreviewStatusRunning     = "running"
	PreviewStatusStarting    = "starting"
	PreviewStatusStopped     = "stopped"
	PreviewStatusUnhealthy   = "unhealthy"
	PreviewStatusUnknown     = "unknown"

	PreviewStartStatusStarted   = "started"
	PreviewStartStatusReused    = "reused"
	PreviewStartStatusRestarted = "restarted"

	defaultPreviewIdleTTL = 2 * time.Hour
	previewStartupGrace   = 30 * time.Second
	previewGCInterval     = 15 * time.Minute
)

// PreviewStartRequest describes a daemon-managed local preview process.
type PreviewStartRequest struct {
	Scope              string   `json:"scope,omitempty"`
	WorkspaceID        string   `json:"workspace_id"`
	IssueID            string   `json:"issue_id,omitempty"`
	RuntimeOwnerUserID string   `json:"runtime_owner_user_id,omitempty"`
	OwnerAgentID       string   `json:"owner_agent_id,omitempty"`
	CWD                string   `json:"cwd"`
	Command            []string `json:"command"`
	Port               int      `json:"port,omitempty"`
	URL                string   `json:"url,omitempty"`
	HealthURL          string   `json:"health_url,omitempty"`
	Restart            bool     `json:"restart,omitempty"`
}

// PreviewActionRequest targets an existing preview by id.
type PreviewActionRequest struct {
	ID string `json:"id"`
}

// Preview represents daemon-local preview state.
type Preview struct {
	ID                 string    `json:"id"`
	Key                string    `json:"key"`
	Scope              string    `json:"scope,omitempty"`
	WorkspaceID        string    `json:"workspace_id"`
	IssueID            string    `json:"issue_id,omitempty"`
	RuntimeOwnerUserID string    `json:"runtime_owner_user_id,omitempty"`
	OwnerAgentID       string    `json:"owner_agent_id,omitempty"`
	DaemonID           string    `json:"daemon_id,omitempty"`
	Visibility         string    `json:"visibility"`
	CWD                string    `json:"cwd"`
	Command            []string  `json:"command"`
	PID                int       `json:"pid"`
	Port               int       `json:"port,omitempty"`
	URL                string    `json:"url,omitempty"`
	HealthURL          string    `json:"health_url,omitempty"`
	LogPath            string    `json:"log_path"`
	Status             string    `json:"status"`
	StartedAt          time.Time `json:"started_at"`
	LastHealthAt       time.Time `json:"last_health_at,omitempty"`
	LastAccessedAt     time.Time `json:"last_accessed_at,omitempty"`
	ExitError          string    `json:"exit_error,omitempty"`
}

type PreviewStartResponse struct {
	Status  string  `json:"status"`
	Preview Preview `json:"preview"`
}

type PreviewLogsResponse struct {
	ID      string `json:"id"`
	LogPath string `json:"log_path"`
	Logs    string `json:"logs"`
}

// PreviewManager owns detached local preview processes and their state files.
type PreviewManager struct {
	dir      string
	daemonID string
	logger   *slog.Logger
	mu       sync.Mutex
	client   *http.Client
}

func NewPreviewManager(dir, daemonID string, logger *slog.Logger) *PreviewManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &PreviewManager{
		dir:      dir,
		daemonID: daemonID,
		logger:   logger,
		client:   &http.Client{Timeout: 2 * time.Second},
	}
}

func (m *PreviewManager) Start(ctx context.Context, req PreviewStartRequest) (PreviewStartResponse, error) {
	if err := req.validate(); err != nil {
		return PreviewStartResponse{}, err
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return PreviewStartResponse{}, fmt.Errorf("create preview state dir: %w", err)
	}

	cwd, err := filepath.Abs(req.CWD)
	if err != nil {
		return PreviewStartResponse{}, fmt.Errorf("resolve cwd: %w", err)
	}
	req.CWD = cwd
	if req.Scope == "" {
		req.Scope = "issue"
	}
	if req.URL == "" && req.Port > 0 {
		req.URL = fmt.Sprintf("http://127.0.0.1:%d/", req.Port)
	}
	if req.HealthURL == "" {
		req.HealthURL = req.URL
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.gcLocked(ctx, time.Now(), false); err != nil {
		m.logger.Warn("preview gc failed", "error", err)
	}

	key := previewKey(req)
	replacingExisting := false
	if existing, ok := m.findByKeyLocked(key); ok {
		existing = m.refreshStatusLocked(ctx, existing)
		if req.Restart {
			replacingExisting = true
			_ = m.stopLocked(existing)
		} else if isReusablePreviewStatus(existing.Status) {
			existing.LastAccessedAt = time.Now()
			_ = m.saveLocked(existing)
			return PreviewStartResponse{Status: PreviewStartStatusReused, Preview: existing}, nil
		} else {
			replacingExisting = true
			_ = m.stopLocked(existing)
		}
	}

	preview, err := m.startProcessLocked(req, key)
	if err != nil {
		return PreviewStartResponse{}, err
	}
	status := PreviewStartStatusStarted
	if req.Restart || replacingExisting {
		status = PreviewStartStatusRestarted
	}
	return PreviewStartResponse{Status: status, Preview: preview}, nil
}

func (m *PreviewManager) List(ctx context.Context) ([]Preview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.gcLocked(ctx, time.Now(), false); err != nil {
		m.logger.Warn("preview gc failed", "error", err)
	}
	previews, err := m.loadAllLocked()
	if err != nil {
		return nil, err
	}
	for i := range previews {
		previews[i] = m.refreshStatusLocked(ctx, previews[i])
	}
	sort.Slice(previews, func(i, j int) bool {
		return previews[i].StartedAt.After(previews[j].StartedAt)
	})
	return previews, nil
}

func (m *PreviewManager) Status(ctx context.Context, id string) (Preview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	preview, err := m.loadLocked(id)
	if err != nil {
		return Preview{}, err
	}
	preview = m.refreshStatusLocked(ctx, preview)
	preview.LastAccessedAt = time.Now()
	_ = m.saveLocked(preview)
	return preview, nil
}

func (m *PreviewManager) Stop(id string) (Preview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	preview, err := m.loadLocked(id)
	if err != nil {
		return Preview{}, err
	}
	if err := m.stopLocked(preview); err != nil {
		return Preview{}, err
	}
	preview.Status = PreviewStatusStopped
	preview.PID = 0
	_ = m.saveLocked(preview)
	return preview, nil
}

func (m *PreviewManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	previews, err := m.loadAllLocked()
	if err != nil {
		return err
	}

	var errs []error
	for _, preview := range previews {
		select {
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			return errors.Join(errs...)
		default:
		}

		preview = m.refreshStatusLocked(ctx, preview)
		if preview.PID <= 0 {
			continue
		}
		if err := m.stopLocked(preview); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *PreviewManager) Restart(ctx context.Context, id string) (PreviewStartResponse, error) {
	m.mu.Lock()
	preview, err := m.loadLocked(id)
	m.mu.Unlock()
	if err != nil {
		return PreviewStartResponse{}, err
	}
	return m.Start(ctx, PreviewStartRequest{
		Scope:              preview.Scope,
		WorkspaceID:        preview.WorkspaceID,
		IssueID:            preview.IssueID,
		RuntimeOwnerUserID: preview.RuntimeOwnerUserID,
		OwnerAgentID:       preview.OwnerAgentID,
		CWD:                preview.CWD,
		Command:            preview.Command,
		Port:               preview.Port,
		URL:                preview.URL,
		HealthURL:          preview.HealthURL,
		Restart:            true,
	})
}

func (m *PreviewManager) Logs(id string, tailBytes int64) (PreviewLogsResponse, error) {
	m.mu.Lock()
	preview, err := m.loadLocked(id)
	m.mu.Unlock()
	if err != nil {
		return PreviewLogsResponse{}, err
	}
	data, err := readTail(preview.LogPath, tailBytes)
	if err != nil {
		return PreviewLogsResponse{}, err
	}
	return PreviewLogsResponse{ID: preview.ID, LogPath: preview.LogPath, Logs: string(data)}, nil
}

func (m *PreviewManager) GC(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gcLocked(ctx, time.Now(), true)
}

func (m *PreviewManager) RunGC(ctx context.Context) {
	ticker := time.NewTicker(previewGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.GC(ctx); err != nil {
				m.logger.Warn("preview gc failed", "error", err)
			}
		}
	}
}

func (m *PreviewManager) startProcessLocked(req PreviewStartRequest, key string) (Preview, error) {
	logPath := filepath.Join(m.dir, key+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Preview{}, fmt.Errorf("open preview log: %w", err)
	}
	defer logFile.Close()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return Preview{}, fmt.Errorf("open dev null: %w", err)
	}
	defer devNull.Close()

	cmd := exec.Command(req.Command[0], req.Command[1:]...)
	cmd.Dir = req.CWD
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	makePreviewDetached(cmd)

	if err := cmd.Start(); err != nil {
		return Preview{}, fmt.Errorf("start preview command: %w", err)
	}
	pid := cmd.Process.Pid

	now := time.Now()
	preview := Preview{
		ID:                 key,
		Key:                key,
		Scope:              req.Scope,
		WorkspaceID:        req.WorkspaceID,
		IssueID:            req.IssueID,
		RuntimeOwnerUserID: req.RuntimeOwnerUserID,
		OwnerAgentID:       req.OwnerAgentID,
		DaemonID:           m.daemonID,
		Visibility:         PreviewVisibilityPrivate,
		CWD:                req.CWD,
		Command:            append([]string(nil), req.Command...),
		PID:                pid,
		Port:               req.Port,
		URL:                req.URL,
		HealthURL:          req.HealthURL,
		LogPath:            logPath,
		Status:             PreviewStatusRunning,
		StartedAt:          now,
		LastAccessedAt:     now,
	}
	if err := m.saveLocked(preview); err != nil {
		_ = killPreviewProcessGroup(pid)
		return Preview{}, err
	}
	go m.waitForExit(preview.ID, pid, cmd)
	m.logger.Info("preview started", "id", preview.ID, "pid", pid, "cwd", preview.CWD, "url", preview.URL)
	return preview, nil
}

func (m *PreviewManager) waitForExit(id string, pid int, cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	preview, loadErr := m.loadLocked(id)
	if loadErr != nil || preview.PID != pid {
		return
	}
	preview.Status = PreviewStatusStopped
	preview.PID = 0
	if err != nil {
		preview.ExitError = err.Error()
	}
	if saveErr := m.saveLocked(preview); saveErr != nil {
		m.logger.Warn("save exited preview state failed", "id", id, "error", saveErr)
	}
}

func (m *PreviewManager) refreshStatusLocked(ctx context.Context, preview Preview) Preview {
	if preview.PID <= 0 {
		preview.Status = PreviewStatusStopped
		_ = m.saveLocked(preview)
		return preview
	}
	if preview.HealthURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, preview.HealthURL, nil)
		if err == nil {
			resp, err := m.client.Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					preview.Status = PreviewStatusRunning
					preview.LastHealthAt = time.Now()
					_ = m.saveLocked(preview)
					return preview
				}
			}
		}
	}
	if previewProcessExists(preview.PID) {
		if preview.HealthURL != "" && !preview.StartedAt.IsZero() && time.Since(preview.StartedAt) < previewStartupGrace {
			preview.Status = PreviewStatusStarting
		} else if preview.HealthURL != "" {
			preview.Status = PreviewStatusUnhealthy
		} else {
			preview.Status = PreviewStatusRunning
		}
	} else {
		preview.Status = PreviewStatusStopped
		preview.PID = 0
	}
	_ = m.saveLocked(preview)
	return preview
}

func (m *PreviewManager) stopLocked(preview Preview) error {
	if preview.PID > 0 && previewProcessExists(preview.PID) {
		if err := killPreviewProcessGroup(preview.PID); err != nil {
			return fmt.Errorf("stop preview %s pid %d: %w", preview.ID, preview.PID, err)
		}
	}
	preview.Status = PreviewStatusStopped
	preview.PID = 0
	return m.saveLocked(preview)
}

func (m *PreviewManager) gcLocked(ctx context.Context, now time.Time, stopIdle bool) error {
	previews, err := m.loadAllLocked()
	if err != nil {
		return err
	}
	for _, preview := range previews {
		preview = m.refreshStatusLocked(ctx, preview)
		if preview.Status == PreviewStatusStopped || preview.PID == 0 {
			continue
		}
		if stopIdle && !preview.LastAccessedAt.IsZero() && now.Sub(preview.LastAccessedAt) > defaultPreviewIdleTTL {
			if err := m.stopLocked(preview); err != nil {
				m.logger.Warn("stop idle preview failed", "id", preview.ID, "error", err)
			}
		}
	}
	return nil
}

func (m *PreviewManager) findByKeyLocked(key string) (Preview, bool) {
	preview, err := m.loadLocked(key)
	return preview, err == nil
}

func (m *PreviewManager) loadAllLocked() ([]Preview, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read preview state dir: %w", err)
	}
	var previews []Preview
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		preview, err := m.loadLocked(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			m.logger.Warn("skip invalid preview state", "file", entry.Name(), "error", err)
			continue
		}
		previews = append(previews, preview)
	}
	return previews, nil
}

func (m *PreviewManager) loadLocked(id string) (Preview, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) {
		return Preview{}, fmt.Errorf("preview id is required")
	}
	data, err := os.ReadFile(m.statePath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Preview{}, fmt.Errorf("preview %s not found", id)
		}
		return Preview{}, fmt.Errorf("read preview state: %w", err)
	}
	var preview Preview
	if err := json.Unmarshal(data, &preview); err != nil {
		return Preview{}, fmt.Errorf("parse preview state: %w", err)
	}
	return preview, nil
}

func (m *PreviewManager) saveLocked(preview Preview) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("create preview state dir: %w", err)
	}
	data, err := json.MarshalIndent(preview, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preview state: %w", err)
	}
	tmp := m.statePath(preview.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write preview state: %w", err)
	}
	if err := os.Rename(tmp, m.statePath(preview.ID)); err != nil {
		return fmt.Errorf("commit preview state: %w", err)
	}
	return nil
}

func (m *PreviewManager) statePath(id string) string {
	return filepath.Join(m.dir, id+".json")
}

func (req PreviewStartRequest) validate() error {
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if strings.TrimSpace(req.CWD) == "" {
		return fmt.Errorf("cwd is required")
	}
	if len(req.Command) == 0 || strings.TrimSpace(req.Command[0]) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

func previewKey(req PreviewStartRequest) string {
	keyParts := []string{
		req.WorkspaceID,
		req.IssueID,
		req.RuntimeOwnerUserID,
		req.CWD,
		strings.Join(req.Command, "\x00"),
	}
	sum := sha256.Sum256([]byte(strings.Join(keyParts, "\x1f")))
	return hex.EncodeToString(sum[:])[:24]
}

func isReusablePreviewStatus(status string) bool {
	return status == PreviewStatusRunning || status == PreviewStatusStarting
}

func readTail(path string, tailBytes int64) ([]byte, error) {
	if tailBytes <= 0 {
		tailBytes = 64 * 1024
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open preview log: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat preview log: %w", err)
	}
	start := int64(0)
	if info.Size() > tailBytes {
		start = info.Size() - tailBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func previewTailBytes(lines int) int64 {
	if lines <= 0 {
		lines = 200
	}
	// The daemon endpoint tails bytes to stay simple and bounded; this maps
	// CLI line counts to a conservative byte budget.
	return int64(lines * 512)
}

func parsePreviewTail(raw string) int64 {
	if raw == "" {
		return previewTailBytes(200)
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return previewTailBytes(200)
	}
	return previewTailBytes(n)
}
