package ail

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultStage7OutputDir    = "diagnostics/stage7"
	defaultStage7DecisionFile = "stage7_decision.json"
)

// Stage7ReplayConfig controls one-off replay filtering, deterministic profile capture, and output layout.
type Stage7ReplayConfig struct {
	IndexPath             string
	OutputDir             string
	EventIDs              []string
	IssueIDs              []string
	AgentIDs              []string
	TimeStart             string
	TimeEnd               string
	FailureReasons        []string
	LoopSignatures        []string
	ToolArgs              map[string]string
	EnvKeys               []string
	GitRevision           string
	EvaluationResultsPath string
	LookupEnv             func(string) string
}

// Stage7ReplayDecision is the byte-stable payload written for replay and evaluation reruns.
type Stage7ReplayDecision struct {
	ReplayID           string                   `json:"replay_id"`
	Filters            Stage7ReplayFilters      `json:"filters"`
	DeterminismProfile Stage7DeterminismProfile `json:"determinism_profile"`
	Metrics            Stage7ReplayMetrics      `json:"metrics"`
	EventCount         int                      `json:"event_count"`
	Events             []Stage7ReplayEvent      `json:"events"`
}

// Stage7ReplayFilters records the exact event subset requested by the replay run.
type Stage7ReplayFilters struct {
	EventIDs       []string `json:"event_ids,omitempty"`
	IssueIDs       []string `json:"issue_ids,omitempty"`
	AgentIDs       []string `json:"agent_ids,omitempty"`
	TimeStart      string   `json:"time_start,omitempty"`
	TimeEnd        string   `json:"time_end,omitempty"`
	FailureReasons []string `json:"failure_reasons,omitempty"`
	LoopSignatures []string `json:"loop_signatures,omitempty"`
}

// Stage7DeterminismProfile captures the deterministic replay context without wall-clock data.
type Stage7DeterminismProfile struct {
	ToolArgs      map[string]string `json:"tool_args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	GitRevision   string            `json:"git_revision,omitempty"`
	InputChecksum string            `json:"input_checksum"`
}

// Stage7ReplayMetrics summarizes optional evaluation results for the selected replay sample.
type Stage7ReplayMetrics struct {
	SuccessOnRetryDelta float64 `json:"success_on_retry_delta"`
	RetryReduction      int     `json:"retry_reduction"`
	Precision           float64 `json:"precision"`
	InvocationCost      float64 `json:"invocation_cost"`
	EvaluationCount     int     `json:"evaluation_count"`
}

// Stage7ReplayEvent binds a stable replay event ID to the original Stage 2 event payload.
type Stage7ReplayEvent struct {
	EventID string      `json:"event_id"`
	Event   Stage2Event `json:"event"`
}

type stage7EvaluationResult struct {
	EventID              string  `json:"event_id"`
	SuccessOnRetryBefore bool    `json:"success_on_retry_before"`
	SuccessOnRetryAfter  bool    `json:"success_on_retry_after"`
	FailedRetriesBefore  int     `json:"failed_retries_before"`
	FailedRetriesAfter   int     `json:"failed_retries_after"`
	Actionable           bool    `json:"actionable"`
	InvocationCost       float64 `json:"invocation_cost"`
}

// RunStage7Replay filters a Stage 2 index, computes deterministic replay metrics, and writes stage7_decision.json.
func RunStage7Replay(cfg Stage7ReplayConfig) (Stage7ReplayDecision, error) {
	cfg = normalizeStage7Config(cfg)
	events, err := readStage7ReplayEvents(cfg)
	if err != nil {
		return Stage7ReplayDecision{}, err
	}

	filters := stage7FiltersFromConfig(cfg)
	selected, err := filterStage7Events(events, filters)
	if err != nil {
		return Stage7ReplayDecision{}, err
	}

	profile := buildStage7DeterminismProfile(cfg, selected)
	metrics, err := buildStage7Metrics(cfg.EvaluationResultsPath, selected)
	if err != nil {
		return Stage7ReplayDecision{}, err
	}

	decision := Stage7ReplayDecision{
		Filters:            filters,
		DeterminismProfile: profile,
		Metrics:            metrics,
		EventCount:         len(selected),
		Events:             selected,
	}
	decision.ReplayID = stage7ReplayID(decision)

	if err := writeJSON(filepath.Join(cfg.OutputDir, defaultStage7DecisionFile), decision); err != nil {
		return decision, err
	}
	return decision, nil
}

// Stage7DecisionPath returns the full decision-payload path for this config.
func (cfg Stage7ReplayConfig) Stage7DecisionPath() string {
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = resolveStage7OutputDir()
	}
	return filepath.Join(outputDir, defaultStage7DecisionFile)
}

func normalizeStage7Config(cfg Stage7ReplayConfig) Stage7ReplayConfig {
	if cfg.IndexPath == "" {
		cfg.IndexPath = filepath.Join(resolveStage2OutputDir(), defaultStage2OutputIndexFile)
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = resolveStage7OutputDir()
	}
	if cfg.LookupEnv == nil {
		cfg.LookupEnv = os.Getenv
	}
	cfg.EventIDs = normalizeStage7List(cfg.EventIDs)
	cfg.IssueIDs = normalizeStage7List(cfg.IssueIDs)
	cfg.AgentIDs = normalizeStage7List(cfg.AgentIDs)
	cfg.FailureReasons = normalizeStage7List(cfg.FailureReasons)
	cfg.LoopSignatures = normalizeStage7List(cfg.LoopSignatures)
	cfg.EnvKeys = normalizeStage7List(cfg.EnvKeys)
	cfg.ToolArgs = normalizeStage7Map(cfg.ToolArgs)
	cfg.GitRevision = strings.TrimSpace(cfg.GitRevision)
	cfg.TimeStart = strings.TrimSpace(cfg.TimeStart)
	cfg.TimeEnd = strings.TrimSpace(cfg.TimeEnd)
	cfg.EvaluationResultsPath = strings.TrimSpace(cfg.EvaluationResultsPath)
	return cfg
}

func readStage7ReplayEvents(cfg Stage7ReplayConfig) ([]Stage7ReplayEvent, error) {
	f, err := os.Open(cfg.IndexPath)
	if err != nil {
		return nil, fmt.Errorf("read stage2 index: %w", err)
	}
	defer f.Close()

	events := make([]Stage7ReplayEvent, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt Stage2Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		events = append(events, Stage7ReplayEvent{EventID: Stage7EventID(evt), Event: evt})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sortStage7Events(events)
	return events, nil
}

// Stage7EventID returns the stable SHA-256 event ID for a Stage 2 event.
func Stage7EventID(evt Stage2Event) string {
	b, _ := json.Marshal(evt)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func filterStage7Events(events []Stage7ReplayEvent, filters Stage7ReplayFilters) ([]Stage7ReplayEvent, error) {
	eventIDs := stage7Set(filters.EventIDs)
	issueIDs := stage7Set(filters.IssueIDs)
	agentIDs := stage7Set(filters.AgentIDs)
	failureReasons := stage7Set(filters.FailureReasons)
	loopSignatures := stage7Set(filters.LoopSignatures)
	timeStart, err := parseOptionalStage7Time(filters.TimeStart)
	if err != nil {
		return nil, fmt.Errorf("parse time-start: %w", err)
	}
	timeEnd, err := parseOptionalStage7Time(filters.TimeEnd)
	if err != nil {
		return nil, fmt.Errorf("parse time-end: %w", err)
	}

	selected := make([]Stage7ReplayEvent, 0, len(events))
	for _, evt := range events {
		if len(eventIDs) > 0 && !eventIDs[evt.EventID] {
			continue
		}
		if len(issueIDs) > 0 && !issueIDs[evt.Event.IssueID] {
			continue
		}
		if len(agentIDs) > 0 && !agentIDs[evt.Event.AgentID] {
			continue
		}
		if len(failureReasons) > 0 && !failureReasons[evt.Event.FailureReason] {
			continue
		}
		if len(loopSignatures) > 0 && !loopSignatures[evt.Event.LoopSignature] {
			continue
		}
		when, err := time.Parse(time.RFC3339Nano, evt.Event.TS)
		if err != nil {
			continue
		}
		if timeStart != nil && when.Before(*timeStart) {
			continue
		}
		if timeEnd != nil && !when.Before(*timeEnd) {
			continue
		}
		selected = append(selected, evt)
	}
	sortStage7Events(selected)
	return selected, nil
}

func buildStage7DeterminismProfile(cfg Stage7ReplayConfig, selected []Stage7ReplayEvent) Stage7DeterminismProfile {
	env := make(map[string]string, len(cfg.EnvKeys))
	for _, key := range cfg.EnvKeys {
		env[key] = cfg.LookupEnv(key)
	}
	return Stage7DeterminismProfile{
		ToolArgs:      cfg.ToolArgs,
		Env:           env,
		GitRevision:   cfg.GitRevision,
		InputChecksum: stage7SelectedEventsChecksum(selected),
	}
}

func buildStage7Metrics(path string, selected []Stage7ReplayEvent) (Stage7ReplayMetrics, error) {
	if path == "" {
		return Stage7ReplayMetrics{}, nil
	}

	selectedIDs := make(map[string]struct{}, len(selected))
	for _, evt := range selected {
		selectedIDs[evt.EventID] = struct{}{}
	}

	f, err := os.Open(path)
	if err != nil {
		return Stage7ReplayMetrics{}, fmt.Errorf("read evaluation results: %w", err)
	}
	defer f.Close()

	total := 0
	beforeSuccess := 0
	afterSuccess := 0
	actionable := 0
	retryReduction := 0
	invocationCost := 0.0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var result stage7EvaluationResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			continue
		}
		if _, ok := selectedIDs[result.EventID]; !ok {
			continue
		}
		total++
		if result.SuccessOnRetryBefore {
			beforeSuccess++
		}
		if result.SuccessOnRetryAfter {
			afterSuccess++
		}
		if result.Actionable {
			actionable++
		}
		retryReduction += result.FailedRetriesBefore - result.FailedRetriesAfter
		invocationCost += result.InvocationCost
	}
	if err := scanner.Err(); err != nil {
		return Stage7ReplayMetrics{}, err
	}
	if total == 0 {
		return Stage7ReplayMetrics{}, nil
	}

	return Stage7ReplayMetrics{
		SuccessOnRetryDelta: float64(afterSuccess-beforeSuccess) / float64(total),
		RetryReduction:      retryReduction,
		Precision:           float64(actionable) / float64(total),
		InvocationCost:      invocationCost,
		EvaluationCount:     total,
	}, nil
}

func stage7FiltersFromConfig(cfg Stage7ReplayConfig) Stage7ReplayFilters {
	return Stage7ReplayFilters{
		EventIDs:       cfg.EventIDs,
		IssueIDs:       cfg.IssueIDs,
		AgentIDs:       cfg.AgentIDs,
		TimeStart:      cfg.TimeStart,
		TimeEnd:        cfg.TimeEnd,
		FailureReasons: cfg.FailureReasons,
		LoopSignatures: cfg.LoopSignatures,
	}
}

func stage7SelectedEventsChecksum(selected []Stage7ReplayEvent) string {
	b, _ := json.Marshal(selected)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func stage7ReplayID(decision Stage7ReplayDecision) string {
	decision.ReplayID = ""
	b, _ := json.Marshal(decision)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortStage7Events(events []Stage7ReplayEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		tI, errI := time.Parse(time.RFC3339Nano, events[i].Event.TS)
		tJ, errJ := time.Parse(time.RFC3339Nano, events[j].Event.TS)
		if errI == nil && errJ == nil && !tI.Equal(tJ) {
			return tI.Before(tJ)
		}
		if events[i].Event.TS != events[j].Event.TS {
			return events[i].Event.TS < events[j].Event.TS
		}
		return events[i].EventID < events[j].EventID
	})
}

func parseOptionalStage7Time(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func normalizeStage7List(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				set[part] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeStage7Map(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stage7Set(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func resolveStage7OutputDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, defaultStage7OutputDir)
	}
	return filepath.Join(".", defaultStage7OutputDir)
}
