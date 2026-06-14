package ail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildStage5DigestLimitsPainSignaturesAndBuildsContracts(t *testing.T) {
	stage3 := Stage3Result{
		WindowStart:    "2026-01-15T00:00:00Z",
		WindowEnd:      "2026-01-16T00:00:00Z",
		WindowDuration: "24h0m0s",
		TotalEvents:    9,
		RepeatSignatures: []Stage3Signature{
			{Key: "agent_error::E_PARSE", FailureReason: "agent_error", ErrorSignature: "E_PARSE", Count: 9, UniqueTasks: 4, UniqueAgents: 2, ExampleTaskID: "task-1", ExampleRawRef: "run-1"},
			{Key: "runtime_offline", FailureReason: "runtime_offline", Count: 8, UniqueTasks: 3},
			{Key: "auth_error", FailureReason: "auth_error", Count: 7, UniqueTasks: 3},
			{Key: "network_error", FailureReason: "network_error", Count: 6, UniqueTasks: 3},
			{Key: "repo_error", FailureReason: "repo_error", Count: 5, UniqueTasks: 3},
			{Key: "hidden_error", FailureReason: "hidden_error", Count: 4, UniqueTasks: 3},
		},
		CandidateDettools: []Stage3CandidateDettool{
			{SuggestedName: "detect_agent_error_e_parse", SourceSignatureKey: "agent_error::E_PARSE", ExpectedDeterminismGain: 0.9, DecisionHint: "ready_for_candidate"},
		},
	}

	digest := BuildStage5Digest(stage3)

	if len(digest.TopPainSignatures) != 5 {
		t.Fatalf("top pain signatures len = %d, want 5", len(digest.TopPainSignatures))
	}
	if digest.TopPainSignatures[0].Rank != 1 {
		t.Fatalf("first rank = %d, want 1", digest.TopPainSignatures[0].Rank)
	}
	if digest.TopPainSignatures[4].Key != "repo_error" {
		t.Fatalf("fifth signature = %q, want repo_error", digest.TopPainSignatures[4].Key)
	}
	if len(digest.RecommendedTools) != 1 {
		t.Fatalf("recommended tools len = %d, want 1", len(digest.RecommendedTools))
	}
	tool := digest.RecommendedTools[0]
	if !strings.Contains(tool.GoSignature, "DetectAgentErrorEParse") {
		t.Fatalf("go signature should contain exported function name, got %q", tool.GoSignature)
	}
	if tool.ExampleInput["error_signature"] != "E_PARSE" {
		t.Fatalf("example input error_signature = %v, want E_PARSE", tool.ExampleInput["error_signature"])
	}
	if len(digest.Alerts) != 0 {
		t.Fatalf("alerts = %#v, want none", digest.Alerts)
	}
	if !strings.HasPrefix(digest.Marker, stage5MarkerPrefix) {
		t.Fatalf("marker = %q, want stage5 prefix", digest.Marker)
	}
}

func TestBuildStage5DigestAddsDettoolNoneAlertGivenSignalsWithoutCandidates(t *testing.T) {
	stage3 := Stage3Result{
		WindowStart:    "2026-01-15T00:00:00Z",
		WindowEnd:      "2026-01-16T00:00:00Z",
		WindowDuration: "24h0m0s",
		TotalEvents:    3,
		RepeatSignatures: []Stage3Signature{
			{Key: "agent_error::E_TIMEOUT", FailureReason: "agent_error", ErrorSignature: "E_TIMEOUT", Count: 3, UniqueTasks: 1, UniqueAgents: 1},
		},
	}

	digest := BuildStage5Digest(stage3)
	comment := RenderStage5Comment(digest)

	if len(digest.Alerts) != 1 || digest.Alerts[0] != stage5DettoolNoneAlert {
		t.Fatalf("alerts = %#v, want dettool.none", digest.Alerts)
	}
	if !strings.Contains(comment, "dettool.none") {
		t.Fatalf("comment should contain dettool.none alert, got:\n%s", comment)
	}
}

func TestBuildStage5DigestOmitsDettoolNoneAlertGivenNoSignals(t *testing.T) {
	digest := BuildStage5Digest(Stage3Result{WindowDuration: "24h0m0s"})

	if len(digest.Alerts) != 0 {
		t.Fatalf("alerts = %#v, want none", digest.Alerts)
	}
	if digest.SignalCount != 0 {
		t.Fatalf("signal count = %d, want 0", digest.SignalCount)
	}
}

func TestRunStage5DigestWritesDigestAndWatermark(t *testing.T) {
	tmp := t.TempDir()
	stage3 := Stage3Result{
		WindowStart:    "2026-01-15T00:00:00Z",
		WindowEnd:      "2026-01-16T00:00:00Z",
		WindowDuration: "24h0m0s",
		TotalEvents:    1,
	}

	digest, err := RunStage5Digest(Stage5Config{OutputDir: tmp}, stage3)
	if err != nil {
		t.Fatalf("RunStage5Digest: %v", err)
	}

	for _, name := range []string{defaultStage5DigestFile, defaultStage5WatermarkFile} {
		if _, err := os.Stat(filepath.Join(tmp, name)); err != nil {
			t.Fatalf("%s not created: %v", name, err)
		}
	}

	wmBytes, err := os.ReadFile(filepath.Join(tmp, defaultStage5WatermarkFile))
	if err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	var wm Stage5Watermark
	if err := json.Unmarshal(wmBytes, &wm); err != nil {
		t.Fatalf("parse watermark: %v", err)
	}
	if wm.Marker != digest.Marker {
		t.Fatalf("watermark marker = %q, want %q", wm.Marker, digest.Marker)
	}
}

func TestRunStage5DigestReturnsErrorGivenBlockedWatermarkPath(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, defaultStage5WatermarkFile), 0o755); err != nil {
		t.Fatalf("create blocking watermark directory: %v", err)
	}

	digest, err := RunStage5Digest(Stage5Config{OutputDir: tmp}, Stage3Result{WindowDuration: "24h0m0s"})

	if err == nil {
		t.Fatal("expected error for blocked watermark path, got nil")
	}
	if digest.Marker == "" {
		t.Fatal("digest marker should be returned with the error")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, defaultStage5DigestFile)); statErr != nil {
		t.Fatalf("digest artifact should be written before watermark error: %v", statErr)
	}
}

func TestRunStage5DigestReturnsErrorGivenBlockedDigestPath(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, defaultStage5DigestFile), 0o755); err != nil {
		t.Fatalf("create blocking digest directory: %v", err)
	}

	_, err := RunStage5Digest(Stage5Config{OutputDir: tmp}, Stage3Result{WindowDuration: "24h0m0s"})

	if err == nil {
		t.Fatal("expected error for blocked digest path, got nil")
	}
}

func TestRunStage5DigestReturnsErrorGivenBlockedOutputPath(t *testing.T) {
	tmp := t.TempDir()
	blockingFile := filepath.Join(tmp, "blocking-file")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	_, err := RunStage5Digest(Stage5Config{OutputDir: filepath.Join(blockingFile, "stage5")}, Stage3Result{})

	if err == nil {
		t.Fatal("expected error for blocked output path, got nil")
	}
}

func TestRenderStage5CommentHandlesEmptySections(t *testing.T) {
	digest := BuildStage5Digest(Stage3Result{
		WindowStart:    "2026-01-15T00:00:00Z",
		WindowEnd:      "2026-01-16T00:00:00Z",
		WindowDuration: "24h0m0s",
	})

	comment := RenderStage5Comment(digest)

	for _, want := range []string{"None in this window", "None recommended", digest.Marker} {
		if !strings.Contains(comment, want) {
			t.Fatalf("comment missing %q:\n%s", want, comment)
		}
	}
}

func TestRenderStage5CommentIncludesSuggestedToolContractAndUnknownAlert(t *testing.T) {
	digest := BuildStage5Digest(Stage3Result{
		WindowStart:    "2026-01-15T00:00:00Z",
		WindowEnd:      "2026-01-16T00:00:00Z",
		WindowDuration: "24h0m0s",
		TotalEvents:    2,
		RepeatSignatures: []Stage3Signature{
			{Key: "agent_error::E_PARSE", FailureReason: "agent_error", ErrorSignature: "E_PARSE", Count: 2, UniqueTasks: 2, UniqueAgents: 1, ExampleTaskID: "task-1"},
		},
		CandidateDettools: []Stage3CandidateDettool{
			{SuggestedName: "detect_agent_error_e_parse", SourceSignatureKey: "agent_error::E_PARSE", ExpectedDeterminismGain: 0.7, DecisionHint: "ready_for_review"},
		},
	})
	digest.Alerts = append(digest.Alerts, "custom.alert")

	comment := RenderStage5Comment(digest)

	for _, want := range []string{"custom.alert", "DetectAgentErrorEParse", "Example input", `"error_signature":"E_PARSE"`, "ready_for_review"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("comment missing %q:\n%s", want, comment)
		}
	}
}

func TestRunStage5DigestUsesDefaultOutputDirUnderHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, err := RunStage5Digest(Stage5Config{}, Stage3Result{WindowDuration: "24h0m0s"})
	if err != nil {
		t.Fatalf("RunStage5Digest: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, defaultStage5OutputDir, defaultStage5DigestFile)); err != nil {
		t.Fatalf("default digest artifact not created: %v", err)
	}
}

func TestResolveStage5OutputDirFallsBackToRelativePathGivenNoHome(t *testing.T) {
	t.Setenv("HOME", "")

	got := resolveStage5OutputDir()
	want := filepath.Join(".", defaultStage5OutputDir)

	if got != want {
		t.Fatalf("resolveStage5OutputDir = %q, want %q", got, want)
	}
}

func TestStage5HelperFallbacks(t *testing.T) {
	if got := stage5ExportedTypeName(""); got != "Tool" {
		t.Fatalf("empty type name = %q, want Tool", got)
	}
	if got := stage5ExportedTypeName("///"); got != "Tool" {
		t.Fatalf("separator-only type name = %q, want Tool", got)
	}
	if got := compactJSON(func() {}); got != "{}" {
		t.Fatalf("compactJSON unsupported value = %q, want {}", got)
	}
}

func TestBuildStage5DigestSortsToolContractsAndHandlesMissingSourceSignature(t *testing.T) {
	digest := BuildStage5Digest(Stage3Result{
		WindowDuration: "24h0m0s",
		TotalEvents:    3,
		CandidateDettools: []Stage3CandidateDettool{
			{SuggestedName: "detect_zeta", SourceSignatureKey: "missing", ExpectedDeterminismGain: 0.5, DecisionHint: "ready_for_review"},
			{SuggestedName: "detect_alpha", SourceSignatureKey: "missing", ExpectedDeterminismGain: 0.5, DecisionHint: "defer"},
			{SuggestedName: "detect_winner", SourceSignatureKey: "missing", ExpectedDeterminismGain: 0.9, DecisionHint: "ready_for_candidate"},
		},
	})

	if len(digest.RecommendedTools) != 3 {
		t.Fatalf("recommended tools len = %d, want 3", len(digest.RecommendedTools))
	}
	if digest.RecommendedTools[0].SuggestedName != "detect_winner" {
		t.Fatalf("first tool = %q, want detect_winner", digest.RecommendedTools[0].SuggestedName)
	}
	if digest.RecommendedTools[1].SuggestedName != "detect_alpha" {
		t.Fatalf("second tool = %q, want detect_alpha", digest.RecommendedTools[1].SuggestedName)
	}
	if digest.RecommendedTools[1].ExampleInput["failure_reason"] != "" {
		t.Fatalf("missing signature failure_reason = %v, want empty string", digest.RecommendedTools[1].ExampleInput["failure_reason"])
	}
}

func TestStage5MarkerStableForEquivalentDigestAndChangesWithWindow(t *testing.T) {
	stage3 := Stage3Result{
		WindowStart:    "2026-01-15T00:00:00Z",
		WindowEnd:      "2026-01-16T00:00:00Z",
		WindowDuration: "24h0m0s",
		TotalEvents:    2,
		RepeatSignatures: []Stage3Signature{
			{Key: "agent_error::E_TIMEOUT", FailureReason: "agent_error", ErrorSignature: "E_TIMEOUT", Count: 2, UniqueTasks: 2, UniqueAgents: 1},
		},
	}
	first := BuildStage5Digest(stage3)
	second := BuildStage5Digest(stage3)
	second.Marker = "ignored stale marker"

	if Stage5Marker(first) != Stage5Marker(second) {
		t.Fatalf("equivalent digests should have the same marker: %q vs %q", Stage5Marker(first), Stage5Marker(second))
	}

	stage3.WindowEnd = "2026-01-17T00:00:00Z"
	changed := BuildStage5Digest(stage3)
	if first.Marker == changed.Marker {
		t.Fatalf("marker should change when the digest window changes: %q", first.Marker)
	}
}
