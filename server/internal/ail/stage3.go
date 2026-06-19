package ail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	defaultStage3OutputDir         = "diagnostics/stage3"
	defaultStage3DigestFile        = "stage3_digest.json"
	defaultStage3SignaturesFile    = "stage3_signatures.jsonl"
	defaultStage3WatermarkFile     = "stage3_watermark.json"
	defaultStage3MinSignatureCount = 3
	defaultStage3MinUniqueTasks    = 2
)

// MinSignatureCount is the default minimum occurrence count for a repeat signature to become a dettool candidate.
// Stage 4 (PER-10) reads this constant to share the same qualification threshold.
var MinSignatureCount = defaultStage3MinSignatureCount

// MinUniqueTasks is the default minimum unique task count for a repeat signature to qualify as a candidate.
// Stage 4 (PER-10) reads this constant to share the same qualification threshold.
var MinUniqueTasks = defaultStage3MinUniqueTasks

// Stage3Config controls Stage 3 analysis parameters.
type Stage3Config struct {
	IndexPath      string
	OutputDir      string
	WindowDuration time.Duration
	MinSigCount    int
	MinUniqueTasks int
	Now            func() time.Time
}

// Stage3Result is the full Stage 3 analysis payload.
type Stage3Result struct {
	AnalyzedAt        string                   `json:"analyzed_at"`
	WindowStart       string                   `json:"window_start"`
	WindowEnd         string                   `json:"window_end"`
	WindowDuration    string                   `json:"window_duration"`
	TotalEvents       int                      `json:"total_window_events"`
	TopPainBuckets    []Stage3PainBucket       `json:"top_pain_buckets"`
	RepeatSignatures  []Stage3Signature        `json:"repeat_signatures"`
	CandidateDettools []Stage3CandidateDettool `json:"candidate_dettools"`
	ByFailureReason   map[string]int           `json:"by_failure_reason"`
}

// Stage3PainBucket is a refined pain bucket combining event counts and unique task/agent dimensions.
type Stage3PainBucket struct {
	Key           string `json:"key"`
	FailureReason string `json:"failure_reason"`
	Count         int    `json:"count"`
	UniqueAgents  int    `json:"unique_agents"`
	UniqueTasks   int    `json:"unique_tasks"`
}

// Stage3Signature is a clustered repeat-failure entry keyed by (failure_reason, error_signature, loop_signature).
type Stage3Signature struct {
	Key            string `json:"key"`
	FailureReason  string `json:"failure_reason"`
	ErrorSignature string `json:"error_signature,omitempty"`
	LoopSignature  string `json:"loop_signature,omitempty"`
	Count          int    `json:"count"`
	UniqueTasks    int    `json:"unique_tasks"`
	UniqueAgents   int    `json:"unique_agents"`
	FirstSeen      string `json:"first_seen"`
	LastSeen       string `json:"last_seen"`
	ExampleTaskID  string `json:"example_task_id,omitempty"`
	ExampleRawRef  string `json:"example_raw_ref,omitempty"`
}

// Stage3CandidateDettool is a ranked candidate for a new deterministic tool.
type Stage3CandidateDettool struct {
	SuggestedName           string  `json:"suggested_name"`
	SourceSignatureKey      string  `json:"source_signature_key"`
	ExpectedDeterminismGain float64 `json:"expected_determinism_gain"`
	DecisionHint            string  `json:"decision_hint"`
}

// Stage3Watermark records window bounds and the SHA-256 of the analyzed index for idempotency.
type Stage3Watermark struct {
	WindowStart       string `json:"window_start"`
	WindowEnd         string `json:"window_end"`
	WindowDuration    string `json:"window_duration"`
	IndexPath         string `json:"index_path"`
	IndexSHA256       string `json:"index_sha256"`
	AnalyzedAt        string `json:"analyzed_at"`
	TotalWindowEvents int    `json:"total_window_events"`
}

// RunStage3Analyze reads the Stage 2 index, computes pain buckets, repeat signatures, and candidate dettools,
// and writes stage3_digest.json, stage3_signatures.jsonl, and stage3_watermark.json to the output directory.
// If the watermark indicates the same index was already analyzed under the same window, the cached digest is returned.
func RunStage3Analyze(cfg Stage3Config) (Stage3Result, error) {
	cfg = normalizeStage3Config(cfg)
	now := cfg.Now()

	indexBytes, err := os.ReadFile(cfg.IndexPath)
	if err != nil {
		return Stage3Result{}, fmt.Errorf("read stage2 index: %w", err)
	}

	h := sha256.Sum256(indexBytes)
	indexSHA256 := hex.EncodeToString(h[:])

	watermarkPath := filepath.Join(cfg.OutputDir, defaultStage3WatermarkFile)
	if prior, ok := loadStage3Watermark(watermarkPath); ok {
		if prior.IndexSHA256 == indexSHA256 && prior.WindowDuration == cfg.WindowDuration.String() {
			digestPath := filepath.Join(cfg.OutputDir, defaultStage3DigestFile)
			if b, readErr := os.ReadFile(digestPath); readErr == nil {
				var cached Stage3Result
				if jsonErr := json.Unmarshal(b, &cached); jsonErr == nil {
					return cached, nil
				}
			}
		}
	}

	events, windowStart, windowEnd := parseStage3Index(indexBytes, cfg, now)
	result := buildStage3Result(cfg, events, now, windowStart, windowEnd)

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return result, err
	}

	digestPath := filepath.Join(cfg.OutputDir, defaultStage3DigestFile)
	if err := writeJSON(digestPath, result); err != nil {
		return result, err
	}

	sigsPath := filepath.Join(cfg.OutputDir, defaultStage3SignaturesFile)
	if err := writeStage3SignaturesJSONL(sigsPath, result.RepeatSignatures); err != nil {
		return result, err
	}

	wm := Stage3Watermark{
		WindowStart:       result.WindowStart,
		WindowEnd:         result.WindowEnd,
		WindowDuration:    result.WindowDuration,
		IndexPath:         cfg.IndexPath,
		IndexSHA256:       indexSHA256,
		AnalyzedAt:        result.AnalyzedAt,
		TotalWindowEvents: result.TotalEvents,
	}
	return result, writeJSON(watermarkPath, wm)
}

func parseStage3Index(indexBytes []byte, cfg Stage3Config, now time.Time) ([]Stage2Event, string, string) {
	cutoff := now.Add(-cfg.WindowDuration)
	var events []Stage2Event
	for _, line := range strings.Split(string(indexBytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt Stage2Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		when, err := time.Parse(time.RFC3339Nano, evt.TS)
		if err != nil {
			continue
		}
		if when.Before(cutoff) {
			continue
		}
		events = append(events, evt)
	}
	return events, cutoff.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)
}

func buildStage3Result(cfg Stage3Config, events []Stage2Event, now time.Time, windowStart, windowEnd string) Stage3Result {
	result := Stage3Result{
		AnalyzedAt:        now.UTC().Format(time.RFC3339Nano),
		WindowStart:       windowStart,
		WindowEnd:         windowEnd,
		WindowDuration:    cfg.WindowDuration.String(),
		TotalEvents:       len(events),
		TopPainBuckets:    make([]Stage3PainBucket, 0),
		RepeatSignatures:  make([]Stage3Signature, 0),
		CandidateDettools: make([]Stage3CandidateDettool, 0),
		ByFailureReason:   map[string]int{},
	}

	type painState struct {
		bucket Stage3PainBucket
		agents map[string]struct{}
		tasks  map[string]struct{}
	}
	type sigState struct {
		sig    Stage3Signature
		agents map[string]struct{}
		tasks  map[string]struct{}
		times  []string
	}

	painMap := map[string]*painState{}
	sigMap := map[string]*sigState{}

	for _, evt := range events {
		fr := strings.TrimSpace(evt.FailureReason)
		if fr == "" {
			continue
		}
		result.ByFailureReason[fr]++

		pb := painMap[fr]
		if pb == nil {
			pb = &painState{
				bucket: Stage3PainBucket{Key: fr, FailureReason: fr},
				agents: map[string]struct{}{},
				tasks:  map[string]struct{}{},
			}
			painMap[fr] = pb
		}
		pb.bucket.Count++
		if evt.AgentID != "" {
			pb.agents[evt.AgentID] = struct{}{}
		}
		if evt.TaskID != "" {
			pb.tasks[evt.TaskID] = struct{}{}
		}

		sk := stage3SignatureKey(fr, evt.ErrorSignature, evt.LoopSignature)
		ss := sigMap[sk]
		if ss == nil {
			ss = &sigState{
				sig: Stage3Signature{
					Key:            sk,
					FailureReason:  fr,
					ErrorSignature: evt.ErrorSignature,
					LoopSignature:  evt.LoopSignature,
					ExampleTaskID:  evt.TaskID,
					ExampleRawRef:  evt.RawRef,
				},
				agents: map[string]struct{}{},
				tasks:  map[string]struct{}{},
			}
			sigMap[sk] = ss
		}
		ss.sig.Count++
		if evt.AgentID != "" {
			ss.agents[evt.AgentID] = struct{}{}
		}
		if evt.TaskID != "" {
			ss.tasks[evt.TaskID] = struct{}{}
		}
		ss.times = append(ss.times, evt.TS)
	}

	buckets := make([]Stage3PainBucket, 0, len(painMap))
	for _, pb := range painMap {
		pb.bucket.UniqueAgents = len(pb.agents)
		pb.bucket.UniqueTasks = len(pb.tasks)
		buckets = append(buckets, pb.bucket)
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return buckets[i].Key < buckets[j].Key
	})
	result.TopPainBuckets = buckets

	sigs := make([]Stage3Signature, 0, len(sigMap))
	for _, ss := range sigMap {
		if len(ss.times) > 0 {
			sorted := make([]string, len(ss.times))
			copy(sorted, ss.times)
			sort.Strings(sorted)
			ss.sig.FirstSeen = sorted[0]
			ss.sig.LastSeen = sorted[len(sorted)-1]
		}
		ss.sig.UniqueAgents = len(ss.agents)
		ss.sig.UniqueTasks = len(ss.tasks)
		sigs = append(sigs, ss.sig)
	}
	sort.Slice(sigs, func(i, j int) bool {
		if sigs[i].Count != sigs[j].Count {
			return sigs[i].Count > sigs[j].Count
		}
		return sigs[i].Key < sigs[j].Key
	})
	result.RepeatSignatures = sigs

	result.CandidateDettools = buildCandidateDettools(cfg, sigs, len(events))
	return result
}

func buildCandidateDettools(cfg Stage3Config, sigs []Stage3Signature, totalEvents int) []Stage3CandidateDettool {
	candidates := make([]Stage3CandidateDettool, 0)
	for _, sig := range sigs {
		if sig.Count < cfg.MinSigCount || sig.UniqueTasks < cfg.MinUniqueTasks {
			continue
		}
		// totalEvents > 0 is guaranteed because qualifying sigs only exist when events were processed.
		gain := float64(sig.Count*sig.UniqueTasks) / float64(totalEvents*totalEvents)
		candidates = append(candidates, Stage3CandidateDettool{
			SuggestedName:           stage3ToSnakeCase(sig.Key),
			SourceSignatureKey:      sig.Key,
			ExpectedDeterminismGain: gain,
			DecisionHint:            stage3DecisionHint(sig.Count, sig.UniqueTasks),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ExpectedDeterminismGain != candidates[j].ExpectedDeterminismGain {
			return candidates[i].ExpectedDeterminismGain > candidates[j].ExpectedDeterminismGain
		}
		return candidates[i].SuggestedName < candidates[j].SuggestedName
	})
	return candidates
}

func stage3SignatureKey(failureReason, errorSignature, loopSignature string) string {
	parts := make([]string, 0, 3)
	if fr := strings.TrimSpace(failureReason); fr != "" {
		parts = append(parts, fr)
	}
	if es := strings.TrimSpace(errorSignature); es != "" {
		parts = append(parts, es)
	}
	if ls := strings.TrimSpace(loopSignature); ls != "" {
		parts = append(parts, ls)
	}
	return strings.Join(parts, "::")
}

func stage3DecisionHint(count, uniqueTasks int) string {
	if count >= 10 && uniqueTasks >= 3 {
		return "ready_for_candidate"
	}
	if count >= 5 || uniqueTasks >= 2 {
		return "ready_for_review"
	}
	return "defer"
}

var stage3NonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

func stage3ToSnakeCase(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return '_'
	}, s)
	s = stage3NonAlphanumRE.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return "detect_" + s
}

func loadStage3Watermark(path string) (Stage3Watermark, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Stage3Watermark{}, false
	}
	var wm Stage3Watermark
	if err := json.Unmarshal(b, &wm); err != nil {
		return Stage3Watermark{}, false
	}
	return wm, true
}

func writeStage3SignaturesJSONL(path string, sigs []Stage3Signature) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, sig := range sigs {
		// Stage3Signature contains only strings and ints; json.Encoder.Encode cannot fail for this type.
		_ = enc.Encode(sig)
	}
	return nil
}

func normalizeStage3Config(cfg Stage3Config) Stage3Config {
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = defaultStage2Window
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = resolveStage3OutputDir()
	}
	if cfg.IndexPath == "" {
		cfg.IndexPath = filepath.Join(resolveStage2OutputDir(), defaultStage2OutputIndexFile)
	}
	if cfg.MinSigCount <= 0 {
		cfg.MinSigCount = MinSignatureCount
	}
	if cfg.MinUniqueTasks <= 0 {
		cfg.MinUniqueTasks = MinUniqueTasks
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg
}

func resolveStage3OutputDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, defaultStage3OutputDir)
	}
	return filepath.Join(".", defaultStage3OutputDir)
}

// NewStage3ConfigFromArgs builds a Stage3Config from CLI arguments, applying defaults for unset values.
func NewStage3ConfigFromArgs(indexPath, outputDir string, windowHours, minSigCount, minUniqueTasks int) Stage3Config {
	cfg := Stage3Config{}
	if indexPath != "" {
		cfg.IndexPath = indexPath
	}
	if outputDir != "" {
		cfg.OutputDir = outputDir
	}
	if windowHours > 0 {
		cfg.WindowDuration = time.Duration(windowHours) * time.Hour
	}
	if minSigCount > 0 {
		cfg.MinSigCount = minSigCount
	}
	if minUniqueTasks > 0 {
		cfg.MinUniqueTasks = minUniqueTasks
	}
	return normalizeStage3Config(cfg)
}
