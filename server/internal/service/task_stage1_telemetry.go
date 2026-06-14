package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultStage1LogPath  = "agent-improvement-loop/stage1-events.jsonl"
	defaultEmitCategories = "agent_event,attempt_event,failure_event"
	stage1ConfigEnvKey    = "MULTICA_AIL_STAGE1_CONFIG"
)

type stage1ConfigFile struct {
	Stage1 *struct {
		Enabled        *bool    `json:"enabled"`
		EventsPath     string   `json:"events_path"`
		EmitCategories []string `json:"emit_categories"`
	} `json:"stage1"`
}

type stage1SinkConfig struct {
	Enabled        *bool
	EventsPath     string
	EmitCategories []string
}

// Stage1EventSink receives compact, machine-readable lifecycle records for
// Stage 1 telemetry.
type Stage1EventSink interface {
	EmitTaskLifecycleEvent(context.Context, Stage1LifecycleEvent) error
}

// Stage1LifecycleEvent is the deterministic event schema used by the agent
// improvement loop ingestion pipeline.
type Stage1LifecycleEvent struct {
	TS              string   `json:"ts"`
	EventType       string   `json:"event_type"`
	WorkspaceID     string   `json:"workspace_id"`
	AgentID         string   `json:"agent_id"`
	IssueID         string   `json:"issue_id,omitempty"`
	TaskID          string   `json:"task_id"`
	RuntimeID       string   `json:"runtime_id,omitempty"`
	Status          string   `json:"status"`
	Attempt         int32    `json:"attempt"`
	MaxAttempts     int32    `json:"max_attempts"`
	RetryCount      int32    `json:"retry_count"`
	RunDurationMs   int64    `json:"run_duration_ms,omitempty"`
	FailureReason   string   `json:"failure_reason,omitempty"`
	ErrorMessage    string   `json:"error_message,omitempty"`
	ErrorSignature  string   `json:"error_signature,omitempty"`
	LoopSignature   string   `json:"loop_signature,omitempty"`
	DettoolsUsed    []string `json:"dettools_used,omitempty"`
	Source          string   `json:"source"`
	Model           string   `json:"model,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	RuntimeMode     string   `json:"runtime_mode,omitempty"`
	RawRef          string   `json:"raw_ref,omitempty"`
	ResultSHA256    string   `json:"result_sha256,omitempty"`
	ResultBytes     int      `json:"result_bytes,omitempty"`
	NextRetryAt     string   `json:"next_retry_at,omitempty"`
	WorkspaceUserID string   `json:"user_id,omitempty"`
}

type noopStage1EventSink struct{}

func (noopStage1EventSink) EmitTaskLifecycleEvent(context.Context, Stage1LifecycleEvent) error {
	return nil
}

type stage1JSONLWriter struct {
	path             string
	emitCategoryOnly map[string]struct{}
	mu               sync.Mutex
}

// NewStage1EventSinkFromEnv constructs the default Stage1 sink.
//
// Env vars:
// - MULTICA_AIL_STAGE1_ENABLED: set to "false", "0", "no" to disable.
// - MULTICA_AIL_STAGE1_EVENTS_PATH: override JSONL output path.
// - MULTICA_AIL_STAGE1_EMIT_CATEGORIES: comma/space-separated event types to emit.
// - MULTICA_AIL_STAGE1_CONFIG: optional JSON config file with stage1 settings.
func NewStage1EventSinkFromEnv() Stage1EventSink {
	cfg := resolveStage1SinkConfigFromEnv()
	if !cfg.EnabledOrDefault(true) {
		return noopStage1EventSink{}
	}

	path := resolveEventsPath(cfg.EventsPath)
	if path == "" {
		path = filepath.Join(homeFallback(), ".multica", defaultStage1LogPath)
	}

	emitCategories := parseEmitCategoriesFromConfigOrEnv(cfg)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("stage1 telemetry: unable to create log directory", "path", dir, "error", err)
		return noopStage1EventSink{}
	}

	return &stage1JSONLWriter{path: path, emitCategoryOnly: emitCategories}
}

func resolveStage1SinkConfigFromEnv() stage1SinkConfig {
	cfg := stage1SinkConfig{}
	if filePath := strings.TrimSpace(os.Getenv(stage1ConfigEnvKey)); filePath != "" {
		loaded, err := stage1ConfigFromFile(filePath)
		if err != nil {
			slog.Warn("stage1 telemetry: invalid config file, using env defaults", "path", filePath, "error", err)
		} else {
			cfg = loaded
		}
	}

	if v := strings.TrimSpace(os.Getenv("MULTICA_AIL_STAGE1_ENABLED")); v != "" {
		enabled := boolEnv("MULTICA_AIL_STAGE1_ENABLED", true)
		cfg.Enabled = &enabled
	}
	if v := strings.TrimSpace(os.Getenv("MULTICA_AIL_STAGE1_EVENTS_PATH")); v != "" {
		cfg.EventsPath = v
	}
	if v := strings.TrimSpace(os.Getenv("MULTICA_AIL_STAGE1_EMIT_CATEGORIES")); v != "" {
		cfg.EmitCategories = parseEmitList(v)
	}

	return cfg
}

func stage1ConfigFromFile(path string) (stage1SinkConfig, error) {
	var out stage1SinkConfig
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	var cfg stage1ConfigFile
	if err := json.Unmarshal(fileBytes, &cfg); err != nil {
		return out, err
	}
	if cfg.Stage1 == nil {
		return out, nil
	}
	out.Enabled = cfg.Stage1.Enabled
	out.EventsPath = cfg.Stage1.EventsPath
	out.EmitCategories = append([]string(nil), cfg.Stage1.EmitCategories...)
	return out, nil
}

func (c stage1SinkConfig) EnabledOrDefault(def bool) bool {
	if c.Enabled == nil {
		return def
	}
	return *c.Enabled
}

func parseEmitCategoriesFromConfigOrEnv(cfg stage1SinkConfig) map[string]struct{} {
	if len(cfg.EmitCategories) > 0 {
		res := make(map[string]struct{}, len(cfg.EmitCategories))
		for _, c := range cfg.EmitCategories {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			res[c] = struct{}{}
		}
		if len(res) > 0 {
			return res
		}
	}
	return parseEmitCategories("")
}

func resolveEventsPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if path == "~" {
		path = homeFallback()
	} else if strings.HasPrefix(path, "~") {
		if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
			home := homeFallback()
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(filepath.Separator)))
			return path
		}
	}
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}
	return path
}

func homeFallback() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "/"
}

func parseEmitList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '	' || r == '\n' || r == ';'
	})
	if len(parts) == 0 {
		return nil
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func parseEmitCategories(v string) map[string]struct{} {
	selected := strings.TrimSpace(v)
	if selected == "" {
		selected = defaultEmitCategories
	}

	parts := strings.FieldsFunc(selected, func(r rune) bool {
		return r == ',' || r == ' ' || r == '	' || r == '\n' || r == ';'
	})
	if len(parts) == 0 {
		return map[string]struct{}{}
	}

	res := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		res[p] = struct{}{}
	}
	return res
}

func (w *stage1JSONLWriter) EmitTaskLifecycleEvent(_ context.Context, evt Stage1LifecycleEvent) error {
	if len(w.emitCategoryOnly) > 0 {
		if _, ok := w.emitCategoryOnly[evt.EventType]; !ok {
			return nil
		}
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s\n", string(data)); err != nil {
		return err
	}
	return nil
}

func boolEnv(name string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "off":
		return false
	default:
		return def
	}
}

func taskErrorSignature(reason string) string {
	switch reason {
	case "runtime_offline", "runtime_recovery":
		return "E_RUNTIME"
	case "timeout", "codex_semantic_inactivity":
		return "E_TIMEOUT"
	case "iteration_limit", "agent_fallback_message":
		return "E_AGENT_OUTPUT"
	case "cancelled", "user_cancelled":
		return "E_CANCELLED"
	default:
		return "E_UNKNOWN"
	}
}

// emitTaskLifecycleEvent writes one compact attempt/failure event for Stage 1 ingestion.
func (s *TaskService) emitTaskLifecycleEvent(ctx context.Context, eventType, status string, task db.AgentTaskQueue, failureReason, errorMessage string) {
	if s == nil || s.Stage1Telemetry == nil {
		return
	}

	tc := analytics.TaskContext{}
	if s.Queries != nil {
		tc = s.taskAnalyticsContext(ctx, task)
	}
	rawReason := failureReason
	if rawReason == "" {
		rawReason = taskFailureReason(task)
	}
	retryCount := task.Attempt - 1
	if retryCount < 0 {
		retryCount = 0
	}

	duration := int64(-1)
	if task.StartedAt.Valid && task.CompletedAt.Valid {
		duration = int64(taskRunSeconds(task) * 1000)
	}
	if status == "running" && task.StartedAt.Valid {
		duration = int64(time.Since(task.StartedAt.Time).Milliseconds())
	}
	if duration < 0 {
		duration = 0
	}

	event := Stage1LifecycleEvent{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		EventType:       eventType,
		WorkspaceID:     tc.WorkspaceID,
		AgentID:         util.UUIDToString(task.AgentID),
		IssueID:         util.UUIDToString(task.IssueID),
		TaskID:          util.UUIDToString(task.ID),
		RuntimeID:       util.UUIDToString(task.RuntimeID),
		Status:          status,
		Attempt:         task.Attempt,
		MaxAttempts:     task.MaxAttempts,
		RetryCount:      retryCount,
		RunDurationMs:   duration,
		FailureReason:   rawReason,
		ErrorMessage:    strings.TrimSpace(errorMessage),
		ErrorSignature:  taskErrorSignature(rawReason),
		Source:          "daemon",
		Provider:        tc.Provider,
		RuntimeMode:     tc.RuntimeMode,
		WorkspaceUserID: tc.UserID,
	}

	if task.Error.Valid {
		event.ErrorMessage = strings.TrimSpace(task.Error.String)
	}
	if event.ErrorMessage == "" {
		event.ErrorMessage = strings.TrimSpace(errorMessage)
	}
	if event.FailureReason != "" {
		event.LoopSignature = ""
	}

	event.ResultBytes = len(task.Result)
	if len(task.Result) > 0 {
		sum := sha256.Sum256(task.Result)
		event.ResultSHA256 = hex.EncodeToString(sum[:])
	}

	if err := s.Stage1Telemetry.EmitTaskLifecycleEvent(ctx, event); err != nil {
		slog.Warn("stage1 telemetry emit failed", "task_id", util.UUIDToString(task.ID), "event_type", eventType, "error", err)
	}
}
