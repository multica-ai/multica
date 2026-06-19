package ail

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultStage2OutputDir       = "diagnostics/stage2"
	defaultStage2OutputIndexFile = "stage2_index.jsonl"
	defaultStage2SummaryFile     = "stage2_summary.json"
	defaultStage2EmitCategories  = "agent_event,attempt_event,failure_event"
	defaultStage2Window          = 24 * time.Hour
	defaultStage2TopN            = 10
)

// Stage2Event mirrors the Stage1 telemetry event schema used by the loop.
type Stage2Event struct {
	TS             string   `json:"ts"`
	EventType      string   `json:"event_type"`
	WorkspaceID    string   `json:"workspace_id"`
	AgentID        string   `json:"agent_id"`
	IssueID        string   `json:"issue_id,omitempty"`
	TaskID         string   `json:"task_id"`
	RuntimeID      string   `json:"runtime_id,omitempty"`
	Status         string   `json:"status"`
	Attempt        int32    `json:"attempt"`
	MaxAttempts    int32    `json:"max_attempts"`
	RetryCount     int32    `json:"retry_count"`
	FailureReason  string   `json:"failure_reason,omitempty"`
	ErrorMessage   string   `json:"error_message,omitempty"`
	ErrorSignature string   `json:"error_signature,omitempty"`
	LoopSignature  string   `json:"loop_signature,omitempty"`
	DettoolsUsed   []string `json:"dettools_used,omitempty"`
	RunDurationMs  int64    `json:"run_duration_ms,omitempty"`
	Source         string   `json:"source,omitempty"`
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	RawRef         string   `json:"raw_ref,omitempty"`
}

// Stage2Config controls capture windowing and file layout.
type Stage2Config struct {
	InputPath      string
	OutputDir      string
	WindowDuration time.Duration
	EmitCategories map[string]struct{}
	TopN           int
}

// Stage2Result is the complete capture+analysis payload.
type Stage2Result struct {
	GeneratedAt     string             `json:"generated_at"`
	WindowStart     string             `json:"window_start"`
	WindowEnd       string             `json:"window_end"`
	WindowDuration  string             `json:"window_duration"`
	TotalInput      int                `json:"total_input_events"`
	TotalWindow     int                `json:"total_window_events"`
	TotalSkipped    int                `json:"total_skipped_lines"`
	ByEventType     map[string]int     `json:"by_event_type"`
	ByFailureReason map[string]int     `json:"by_failure_reason"`
	UniqueTasks     int                `json:"unique_tasks"`
	UniqueAgents    int                `json:"unique_agents"`
	TopPainBuckets  []Stage2PainBucket `json:"top_pain_buckets"`
}

type Stage2PainBucket struct {
	FailureReason string `json:"failure_reason"`
	AgentID       string `json:"agent_id"`
	IssueID       string `json:"issue_id"`
	Count         int    `json:"count"`
	EventType     string `json:"event_type"`
	TaskCount     int    `json:"task_count"`
	Key           string `json:"key"`
}

type Stage2Payload struct {
	events     []Stage2Event
	totalInput int
}

// RunStage2Capture reads Stage1 events, writes index + summary.
func RunStage2Capture(cfg Stage2Config) (Stage2Result, error) {
	cfg = normalizeStage2Config(cfg)
	loaded, skipped, err := readStage2Events(cfg)
	if err != nil {
		return Stage2Result{}, err
	}

	if err := writeStage2Index(cfg, loaded.events); err != nil {
		return Stage2Result{}, err
	}

	result := summarizeStage2(cfg, loaded)
	result.TotalSkipped = skipped
	result.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)

	summaryPath := filepath.Join(cfg.OutputDir, defaultStage2SummaryFile)
	if err := writeJSON(summaryPath, result); err != nil {
		return result, err
	}

	return result, nil
}

func readStage2Events(cfg Stage2Config) (Stage2Payload, int, error) {
	f, err := os.Open(cfg.InputPath)
	if err != nil {
		return Stage2Payload{}, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	events := make([]Stage2Event, 0)
	skipped := 0
	totalInput := 0
	cutoff := time.Now().UTC().Add(-cfg.WindowDuration)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		totalInput++

		var evt Stage2Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			skipped++
			continue
		}
		if !cfg.shouldKeepEventType(evt.EventType) {
			continue
		}

		when, err := time.Parse(time.RFC3339Nano, evt.TS)
		if err != nil {
			skipped++
			continue
		}
		if when.Before(cutoff) {
			continue
		}

		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		return Stage2Payload{}, skipped, err
	}

	sort.SliceStable(events, func(i, j int) bool {
		tI, errI := time.Parse(time.RFC3339Nano, events[i].TS)
		tJ, errJ := time.Parse(time.RFC3339Nano, events[j].TS)
		if errI != nil || errJ != nil {
			return events[i].TS < events[j].TS
		}
		if !tI.Equal(tJ) {
			return tI.Before(tJ)
		}
		return i < j
	})

	return Stage2Payload{events: events, totalInput: totalInput}, skipped, nil
}

func summarizeStage2(cfg Stage2Config, p Stage2Payload) Stage2Result {
	result := Stage2Result{
		ByEventType:     map[string]int{},
		ByFailureReason: map[string]int{},
	}
	type bucketState struct {
		bucket Stage2PainBucket
		tasks  map[string]struct{}
	}

	result.TotalWindow = len(p.events)
	result.TotalInput = p.totalInput

	cutoff := time.Now().UTC().Add(-cfg.WindowDuration)
	result.WindowStart = cutoff.Format(time.RFC3339Nano)
	result.WindowEnd = time.Now().UTC().Format(time.RFC3339Nano)
	result.WindowDuration = cfg.WindowDuration.String()

	taskSet := map[string]struct{}{}
	agentSet := map[string]struct{}{}
	byBucket := map[string]*bucketState{}

	for _, evt := range p.events {
		result.ByEventType[evt.EventType]++
		if evt.TaskID != "" {
			taskSet[evt.TaskID] = struct{}{}
		}
		if evt.AgentID != "" {
			agentSet[evt.AgentID] = struct{}{}
		}

		failureReason := strings.TrimSpace(evt.FailureReason)
		if failureReason == "" {
			continue
		}

		result.ByFailureReason[failureReason]++
		key := bucketKey(failureReason, evt.AgentID, evt.IssueID)
		state := byBucket[key]
		if state == nil {
			displayKey := failureReason
			if evt.IssueID != "" {
				displayKey = failureReason + "::" + evt.AgentID + "::" + evt.IssueID
			}
			state = &bucketState{
				bucket: Stage2PainBucket{
					FailureReason: failureReason,
					AgentID:       evt.AgentID,
					IssueID:       evt.IssueID,
					EventType:     evt.EventType,
					Key:           displayKey,
				},
				tasks: make(map[string]struct{}),
			}
			byBucket[key] = state
		}
		state.bucket.Count++
		if evt.TaskID != "" {
			state.tasks[evt.TaskID] = struct{}{}
		}
	}

	result.UniqueTasks = len(taskSet)
	result.UniqueAgents = len(agentSet)

	buckets := make([]Stage2PainBucket, 0, len(byBucket))
	for _, state := range byBucket {
		state.bucket.TaskCount = len(state.tasks)
		buckets = append(buckets, state.bucket)
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count == buckets[j].Count {
			return buckets[i].Key < buckets[j].Key
		}
		return buckets[i].Count > buckets[j].Count
	})

	if cfg.TopN > 0 && cfg.TopN < len(buckets) {
		buckets = buckets[:cfg.TopN]
	}
	result.TopPainBuckets = buckets

	return result
}

func writeStage2Index(cfg Stage2Config, events []Stage2Event) error {
	indexPath := filepath.Join(cfg.OutputDir, defaultStage2OutputIndexFile)
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}

	f, err := os.Create(indexPath)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := bufio.NewWriter(f)
	defer enc.Flush()

	for _, evt := range events {
		b, err := json.Marshal(evt)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(enc, "%s\n", b); err != nil {
			return err
		}
	}

	return nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func normalizeStage2Config(cfg Stage2Config) Stage2Config {
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = defaultStage2Window
	}
	if cfg.InputPath == "" {
		cfg.InputPath = defaultStage2InputPath()
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = resolveStage2OutputDir()
	}
	if cfg.TopN <= 0 {
		cfg.TopN = defaultStage2TopN
	}
	if len(cfg.EmitCategories) == 0 {
		cfg.EmitCategories = ParseEmitCategories(defaultStage2EmitCategories)
	}
	return cfg
}

func defaultStage2InputPath() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".multica", "agent-improvement-loop", "stage1-events.jsonl")
	}
	return filepath.Join(".", ".multica", "agent-improvement-loop", "stage1-events.jsonl")
}

func resolveStage2OutputDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, defaultStage2OutputDir)
	}
	return filepath.Join(".", defaultStage2OutputDir)
}

// IndexFilePath returns the full path to the Stage 2 index JSONL file for this config.
func (cfg Stage2Config) IndexFilePath() string {
	return filepath.Join(cfg.OutputDir, defaultStage2OutputIndexFile)
}

func (cfg Stage2Config) shouldKeepEventType(eventType string) bool {
	if len(cfg.EmitCategories) == 0 {
		return true
	}
	_, ok := cfg.EmitCategories[eventType]
	return ok
}

func ParseEmitCategories(raw string) map[string]struct{} {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == ';'
	})
	out := map[string]struct{}{}
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}

func bucketKey(failureReason, agentID, issueID string) string {
	parts := []string{strings.TrimSpace(failureReason), strings.TrimSpace(agentID)}
	if i := strings.TrimSpace(issueID); i != "" {
		parts = append(parts, i)
	}
	return strings.Join(parts, "::")
}

func NewStage2ConfigFromArgs(configPath, eventsPath, outputDir, emitCats string, windowHours int) (Stage2Config, error) {
	cfg := Stage2Config{}
	if configPath != "" {
		var cfgFile struct {
			Stage1 struct {
				EventsPath     string   `json:"events_path"`
				EmitCategories []string `json:"emit_categories"`
			} `json:"stage1"`
		}
		b, err := os.ReadFile(configPath)
		if err != nil {
			return Stage2Config{}, fmt.Errorf("read stage2 config: %w", err)
		}
		if err := json.Unmarshal(b, &cfgFile); err != nil {
			return Stage2Config{}, fmt.Errorf("parse stage2 config: %w", err)
		}
		if cfgFile.Stage1.EventsPath != "" {
			cfg.InputPath = cfgFile.Stage1.EventsPath
		}
		if len(cfgFile.Stage1.EmitCategories) > 0 {
			e := make([]string, 0, len(cfgFile.Stage1.EmitCategories))
			for _, cat := range cfgFile.Stage1.EmitCategories {
				cat = strings.TrimSpace(cat)
				if cat != "" {
					e = append(e, cat)
				}
			}
			cfg.EmitCategories = ParseEmitCategories(strings.Join(e, ","))
		}
	}

	if eventsPath != "" {
		cfg.InputPath = eventsPath
	}
	if outputDir != "" {
		cfg.OutputDir = outputDir
	}
	if emitCats != "" {
		cfg.EmitCategories = ParseEmitCategories(emitCats)
	}
	if windowHours > 0 {
		cfg.WindowDuration = time.Duration(windowHours) * time.Hour
	}

	return normalizeStage2Config(cfg), nil
}
