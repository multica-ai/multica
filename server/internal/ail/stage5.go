package ail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultStage5OutputDir      = "diagnostics/stage5"
	defaultStage5DigestFile     = "stage5_digest.json"
	defaultStage5WatermarkFile  = "stage5_watermark.json"
	defaultStage5TopPainLimit   = 5
	stage5MarkerPrefix          = "<!-- multica:ail-stage5:v1:"
	stage5MarkerSuffix          = " -->"
	stage5DettoolNoneAlert      = "dettool.none"
	stage5ToolSignatureTemplate = "func %s(ctx context.Context, input %sInput) (%sOutput, error)"
)

// Stage5Config controls Stage 5 digest artifact generation.
type Stage5Config struct {
	OutputDir string
}

// Stage5Digest is the deterministic human-readable Stage 5 reporting payload.
type Stage5Digest struct {
	Marker                    string                `json:"marker"`
	WindowStart               string                `json:"window_start"`
	WindowEnd                 string                `json:"window_end"`
	WindowDuration            string                `json:"window_duration"`
	SignalCount               int                   `json:"signal_count"`
	RecommendedCandidateCount int                   `json:"recommended_candidate_count"`
	TopPainSignatures         []Stage5PainSignature `json:"top_pain_signatures"`
	RecommendedTools          []Stage5ToolContract  `json:"recommended_tools"`
	Alerts                    []string              `json:"alerts"`
}

// Stage5PainSignature is the trimmed signature view surfaced in the tuning digest.
type Stage5PainSignature struct {
	Rank           int    `json:"rank"`
	Key            string `json:"key"`
	FailureReason  string `json:"failure_reason"`
	ErrorSignature string `json:"error_signature,omitempty"`
	LoopSignature  string `json:"loop_signature,omitempty"`
	Count          int    `json:"count"`
	UniqueTasks    int    `json:"unique_tasks"`
	UniqueAgents   int    `json:"unique_agents"`
	ExampleTaskID  string `json:"example_task_id,omitempty"`
	ExampleRawRef  string `json:"example_raw_ref,omitempty"`
}

// Stage5ToolContract describes a suggested deterministic tool and example IO.
type Stage5ToolContract struct {
	Rank               int            `json:"rank"`
	SuggestedName      string         `json:"suggested_name"`
	SourceSignatureKey string         `json:"source_signature_key"`
	GoSignature        string         `json:"go_signature"`
	ExampleInput       map[string]any `json:"example_input"`
	ExampleOutput      map[string]any `json:"example_output"`
	DecisionHint       string         `json:"decision_hint"`
}

// Stage5Watermark records the digest marker written for local auditability.
type Stage5Watermark struct {
	Marker                    string `json:"marker"`
	WindowStart               string `json:"window_start"`
	WindowEnd                 string `json:"window_end"`
	SignalCount               int    `json:"signal_count"`
	RecommendedCandidateCount int    `json:"recommended_candidate_count"`
}

// BuildStage5Digest converts a Stage 3 analyzer result into the human-readable Stage 5 digest model.
func BuildStage5Digest(stage3 Stage3Result) Stage5Digest {
	digest := Stage5Digest{
		WindowStart:               stage3.WindowStart,
		WindowEnd:                 stage3.WindowEnd,
		WindowDuration:            stage3.WindowDuration,
		SignalCount:               stage3.TotalEvents,
		RecommendedCandidateCount: len(stage3.CandidateDettools),
		TopPainSignatures:         buildStage5PainSignatures(stage3.RepeatSignatures),
		RecommendedTools:          buildStage5ToolContracts(stage3),
		Alerts:                    make([]string, 0, 1),
	}
	if digest.SignalCount > 0 && digest.RecommendedCandidateCount == 0 {
		digest.Alerts = append(digest.Alerts, stage5DettoolNoneAlert)
	}
	digest.Marker = Stage5Marker(digest)
	return digest
}

// RunStage5Digest writes the Stage 5 digest and watermark artifacts, then returns the digest.
func RunStage5Digest(cfg Stage5Config, stage3 Stage3Result) (Stage5Digest, error) {
	cfg = normalizeStage5Config(cfg)
	digest := BuildStage5Digest(stage3)
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return digest, err
	}
	if err := writeJSON(filepath.Join(cfg.OutputDir, defaultStage5DigestFile), digest); err != nil {
		return digest, err
	}
	watermark := Stage5Watermark{
		Marker:                    digest.Marker,
		WindowStart:               digest.WindowStart,
		WindowEnd:                 digest.WindowEnd,
		SignalCount:               digest.SignalCount,
		RecommendedCandidateCount: digest.RecommendedCandidateCount,
	}
	return digest, writeJSON(filepath.Join(cfg.OutputDir, defaultStage5WatermarkFile), watermark)
}

// RenderStage5Comment renders a stable tuning issue comment with the hidden idempotency marker.
func RenderStage5Comment(digest Stage5Digest) string {
	var b strings.Builder
	b.WriteString(digest.Marker)
	b.WriteString("\n\n## Agent Improvement Digest\n\n")
	fmt.Fprintf(&b, "Window: %s to %s (%s)\n\n", digest.WindowStart, digest.WindowEnd, digest.WindowDuration)
	fmt.Fprintf(&b, "Signals: %d\n", digest.SignalCount)
	fmt.Fprintf(&b, "Recommended candidates: %d\n\n", digest.RecommendedCandidateCount)

	if len(digest.Alerts) > 0 {
		b.WriteString("### Alerts\n")
		for _, alert := range digest.Alerts {
			if alert == stage5DettoolNoneAlert {
				b.WriteString("- dettool.none: signals were present, but no deterministic tool candidates met the recommendation threshold.\n")
				continue
			}
			fmt.Fprintf(&b, "- %s\n", alert)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Top pain signatures\n")
	if len(digest.TopPainSignatures) == 0 {
		b.WriteString("- None in this window.\n")
	} else {
		for _, sig := range digest.TopPainSignatures {
			fmt.Fprintf(&b, "%d. `%s` - count=%d tasks=%d agents=%d", sig.Rank, sig.Key, sig.Count, sig.UniqueTasks, sig.UniqueAgents)
			if sig.ExampleTaskID != "" {
				fmt.Fprintf(&b, " example_task=%s", sig.ExampleTaskID)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	b.WriteString("### Suggested tools\n")
	if len(digest.RecommendedTools) == 0 {
		b.WriteString("- None recommended in this window.\n")
	} else {
		for _, tool := range digest.RecommendedTools {
			fmt.Fprintf(&b, "%d. `%s`\n", tool.Rank, tool.SuggestedName)
			fmt.Fprintf(&b, "   - Signature: `%s`\n", tool.GoSignature)
			fmt.Fprintf(&b, "   - Source: `%s`\n", tool.SourceSignatureKey)
			fmt.Fprintf(&b, "   - Example input: `%s`\n", compactJSON(tool.ExampleInput))
			fmt.Fprintf(&b, "   - Example output: `%s`\n", compactJSON(tool.ExampleOutput))
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// Stage5Marker returns a stable hidden comment marker for duplicate suppression.
func Stage5Marker(digest Stage5Digest) string {
	payload := struct {
		WindowStart               string                `json:"window_start"`
		WindowEnd                 string                `json:"window_end"`
		WindowDuration            string                `json:"window_duration"`
		SignalCount               int                   `json:"signal_count"`
		RecommendedCandidateCount int                   `json:"recommended_candidate_count"`
		TopPainSignatures         []Stage5PainSignature `json:"top_pain_signatures"`
		RecommendedTools          []Stage5ToolContract  `json:"recommended_tools"`
		Alerts                    []string              `json:"alerts"`
	}{
		WindowStart:               digest.WindowStart,
		WindowEnd:                 digest.WindowEnd,
		WindowDuration:            digest.WindowDuration,
		SignalCount:               digest.SignalCount,
		RecommendedCandidateCount: digest.RecommendedCandidateCount,
		TopPainSignatures:         digest.TopPainSignatures,
		RecommendedTools:          digest.RecommendedTools,
		Alerts:                    digest.Alerts,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return stage5MarkerPrefix + hex.EncodeToString(sum[:]) + stage5MarkerSuffix
}

func buildStage5PainSignatures(signatures []Stage3Signature) []Stage5PainSignature {
	limit := len(signatures)
	if limit > defaultStage5TopPainLimit {
		limit = defaultStage5TopPainLimit
	}
	out := make([]Stage5PainSignature, 0, limit)
	for i := 0; i < limit; i++ {
		sig := signatures[i]
		out = append(out, Stage5PainSignature{
			Rank:           i + 1,
			Key:            sig.Key,
			FailureReason:  sig.FailureReason,
			ErrorSignature: sig.ErrorSignature,
			LoopSignature:  sig.LoopSignature,
			Count:          sig.Count,
			UniqueTasks:    sig.UniqueTasks,
			UniqueAgents:   sig.UniqueAgents,
			ExampleTaskID:  sig.ExampleTaskID,
			ExampleRawRef:  sig.ExampleRawRef,
		})
	}
	return out
}

func buildStage5ToolContracts(stage3 Stage3Result) []Stage5ToolContract {
	signaturesByKey := make(map[string]Stage3Signature, len(stage3.RepeatSignatures))
	for _, sig := range stage3.RepeatSignatures {
		signaturesByKey[sig.Key] = sig
	}

	candidates := append([]Stage3CandidateDettool(nil), stage3.CandidateDettools...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ExpectedDeterminismGain != candidates[j].ExpectedDeterminismGain {
			return candidates[i].ExpectedDeterminismGain > candidates[j].ExpectedDeterminismGain
		}
		return candidates[i].SuggestedName < candidates[j].SuggestedName
	})

	out := make([]Stage5ToolContract, 0, len(candidates))
	for i, candidate := range candidates {
		typeName := stage5ExportedTypeName(candidate.SuggestedName)
		sig := signaturesByKey[candidate.SourceSignatureKey]
		out = append(out, Stage5ToolContract{
			Rank:               i + 1,
			SuggestedName:      candidate.SuggestedName,
			SourceSignatureKey: candidate.SourceSignatureKey,
			GoSignature:        fmt.Sprintf(stage5ToolSignatureTemplate, stage5ExportedFuncName(candidate.SuggestedName), typeName, typeName),
			ExampleInput: map[string]any{
				"failure_reason":  sig.FailureReason,
				"error_signature": sig.ErrorSignature,
				"loop_signature":  sig.LoopSignature,
				"example_task_id": sig.ExampleTaskID,
			},
			ExampleOutput: map[string]any{
				"decision":       candidate.DecisionHint,
				"matched":        true,
				"source_cluster": candidate.SourceSignatureKey,
			},
			DecisionHint: candidate.DecisionHint,
		})
	}
	return out
}

func stage5ExportedFuncName(s string) string {
	name := strings.TrimPrefix(s, "detect_")
	return "Detect" + stage5ExportedTypeName(name)
}

func stage5ExportedTypeName(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ':' || r == '.' || r == '/'
	})
	if len(parts) == 0 {
		return "Tool"
	}
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	return b.String()
}

func compactJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func normalizeStage5Config(cfg Stage5Config) Stage5Config {
	if cfg.OutputDir == "" {
		cfg.OutputDir = resolveStage5OutputDir()
	}
	return cfg
}

func resolveStage5OutputDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, defaultStage5OutputDir)
	}
	return filepath.Join(".", defaultStage5OutputDir)
}
