package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type aoIssueSessionPointer struct {
	SessionID string    `json:"session_id"`
	WorkDir   string    `json:"work_dir,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (d *Daemon) applyAOIssueSessionFallback(task *Task, provider string, logger *slog.Logger) {
	if provider != "ao" || task == nil || task.TriggerCommentID == "" || task.PriorSessionID != "" {
		return
	}
	key := aoIssueSessionKey(*task)
	if key == "" {
		return
	}
	d.aoIssueSessionsMu.RLock()
	ptr, ok := d.aoIssueSessions[key]
	d.aoIssueSessionsMu.RUnlock()
	if !ok || ptr.SessionID == "" {
		return
	}
	task.PriorSessionID = ptr.SessionID
	if task.PriorWorkDir == "" {
		task.PriorWorkDir = ptr.WorkDir
	}
	logger.Info("ao local session fallback applied",
		"issue", task.IssueID,
		"agent", task.AgentID,
		"session_id", task.PriorSessionID,
		"has_workdir", task.PriorWorkDir != "",
	)
}

func (d *Daemon) rememberAOIssueSession(task Task, provider string, result TaskResult, logger *slog.Logger) {
	if provider != "ao" || result.SessionID == "" {
		return
	}
	key := aoIssueSessionKey(task)
	if key == "" {
		return
	}
	ptr := aoIssueSessionPointer{
		SessionID: result.SessionID,
		WorkDir:   result.WorkDir,
		UpdatedAt: time.Now().UTC(),
	}
	d.aoIssueSessionsMu.Lock()
	if d.aoIssueSessions == nil {
		d.aoIssueSessions = make(map[string]aoIssueSessionPointer)
	}
	d.aoIssueSessions[key] = ptr
	snapshot := make(map[string]aoIssueSessionPointer, len(d.aoIssueSessions))
	for k, v := range d.aoIssueSessions {
		snapshot[k] = v
	}
	d.aoIssueSessionsMu.Unlock()
	if err := d.writeAOIssueSessionCache(snapshot); err != nil {
		logger.Warn("ao local session cache persist failed", "error", err)
	}
}

func aoIssueSessionKey(task Task) string {
	if task.WorkspaceID == "" || task.AgentID == "" || task.IssueID == "" {
		return ""
	}
	return task.WorkspaceID + "|" + task.AgentID + "|" + task.IssueID
}

func (d *Daemon) aoIssueSessionCachePath() string {
	if d == nil || d.cfg.WorkspacesRoot == "" {
		return ""
	}
	return filepath.Join(d.cfg.WorkspacesRoot, ".ao-session-cache.json")
}

func (d *Daemon) loadAOIssueSessionCache(logger *slog.Logger) {
	path := d.aoIssueSessionCachePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("ao local session cache read failed", "error", err)
		}
		return
	}
	var cache map[string]aoIssueSessionPointer
	if err := json.Unmarshal(data, &cache); err != nil {
		logger.Warn("ao local session cache decode failed", "error", err)
		return
	}
	d.aoIssueSessionsMu.Lock()
	d.aoIssueSessions = cache
	d.aoIssueSessionsMu.Unlock()
}

func (d *Daemon) writeAOIssueSessionCache(cache map[string]aoIssueSessionPointer) error {
	path := d.aoIssueSessionCachePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ao-session-cache-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
