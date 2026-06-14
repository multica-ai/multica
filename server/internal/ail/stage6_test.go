package ail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunStage6GenerateCreatesCandidateTestAndManifest(t *testing.T) {
	tmp := t.TempDir()
	candidatePath := filepath.Join(tmp, "contract.json")
	prospectDir := filepath.Join(tmp, "prospect")
	contract := stage6TestContract("detect_agent_error_e_parse")
	writeStage6TestContract(t, candidatePath, contract)

	result, err := RunStage6Generate(Stage6Config{
		CandidateJSONPath: candidatePath,
		ProspectDir:       prospectDir,
		HumanApproveRef:   "PER-12",
		Owner:             "platform",
		GeneratedAt:       time.Date(2026, 6, 14, 16, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RunStage6Generate: %v", err)
	}

	for _, path := range []string{result.CandidatePath, result.TestPath, result.ManifestPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated path %q not created: %v", path, err)
		}
	}
	source, err := os.ReadFile(result.CandidatePath)
	if err != nil {
		t.Fatalf("read candidate: %v", err)
	}
	for _, want := range []string{"DisallowUnknownFields", "INVALID_INPUT", "DetectAgentErrorEParse", "agent_error::E_PARSE::loop"} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("candidate source missing %q:\n%s", want, source)
		}
	}

	manifest := readStage6TestManifest(t, result.ManifestPath)
	if manifest.GeneratedAt != "2026-06-14T16:00:00Z" {
		t.Fatalf("manifest generated_at = %q, want fixed time", manifest.GeneratedAt)
	}
	if len(manifest.Items) != 1 {
		t.Fatalf("manifest item len = %d, want 1", len(manifest.Items))
	}
	if manifest.Items[0].Status != stage6CandidateStatus {
		t.Fatalf("status = %q, want candidate", manifest.Items[0].Status)
	}
}

func TestRunStage6GenerateReplacesExistingManifestItem(t *testing.T) {
	tmp := t.TempDir()
	candidatePath := filepath.Join(tmp, "contract.json")
	prospectDir := filepath.Join(tmp, "prospect")
	writeStage6TestContract(t, candidatePath, stage6TestContract("detect_agent_error_e_parse"))

	cfg := Stage6Config{
		CandidateJSONPath: candidatePath,
		ProspectDir:       prospectDir,
		HumanApproveRef:   "PER-12",
		Owner:             "platform",
		GeneratedAt:       time.Date(2026, 6, 14, 16, 0, 0, 0, time.UTC),
	}
	if _, err := RunStage6Generate(cfg); err != nil {
		t.Fatalf("first RunStage6Generate: %v", err)
	}
	cfg.Owner = "runtime"
	cfg.GeneratedAt = time.Date(2026, 6, 15, 16, 0, 0, 0, time.UTC)
	if _, err := RunStage6Generate(cfg); err != nil {
		t.Fatalf("second RunStage6Generate: %v", err)
	}

	manifest := readStage6TestManifest(t, filepath.Join(prospectDir, defaultStage6ManifestFile))
	if len(manifest.Items) != 1 {
		t.Fatalf("manifest item len = %d, want replacement not append", len(manifest.Items))
	}
	if manifest.Items[0].Owner != "runtime" {
		t.Fatalf("owner = %q, want runtime", manifest.Items[0].Owner)
	}
}

func TestRunStage6GenerateNormalizesToolOverride(t *testing.T) {
	tmp := t.TempDir()
	candidatePath := filepath.Join(tmp, "contract.json")
	writeStage6TestContract(t, candidatePath, stage6TestContract("detect_original"))

	result, err := RunStage6Generate(Stage6Config{
		CandidateJSONPath: candidatePath,
		ToolName:          "Detect Runtime.Offline/E Conn",
		ProspectDir:       filepath.Join(tmp, "prospect"),
		HumanApproveRef:   "PER-12",
		Owner:             "platform",
	})
	if err != nil {
		t.Fatalf("RunStage6Generate: %v", err)
	}

	if result.ToolName != "detect_runtime_offline_e_conn" {
		t.Fatalf("tool name = %q, want normalized override", result.ToolName)
	}
}

func TestRunStage6GenerateDerivesContractFromStage3Digest(t *testing.T) {
	tmp := t.TempDir()
	stage3Path := filepath.Join(tmp, "stage3.json")
	stage3 := Stage3Result{
		WindowDuration: "24h0m0s",
		TotalEvents:    3,
		RepeatSignatures: []Stage3Signature{
			{Key: "runtime_offline::E_CONN::loop", FailureReason: "runtime_offline", ErrorSignature: "E_CONN", LoopSignature: "loop", Count: 3, UniqueTasks: 2, UniqueAgents: 1, ExampleTaskID: "task-2"},
		},
		CandidateDettools: []Stage3CandidateDettool{
			{SuggestedName: "detect_runtime_offline_e_conn_loop", SourceSignatureKey: "runtime_offline::E_CONN::loop", ExpectedDeterminismGain: 0.7, DecisionHint: "ready_for_review"},
		},
	}
	raw, err := json.Marshal(stage3)
	if err != nil {
		t.Fatalf("marshal stage3: %v", err)
	}
	if err := os.WriteFile(stage3Path, raw, 0o644); err != nil {
		t.Fatalf("write stage3: %v", err)
	}

	result, err := RunStage6Generate(Stage6Config{
		Stage3DigestPath: stage3Path,
		ToolName:         "detect_runtime_offline_e_conn_loop",
		ProspectDir:      filepath.Join(tmp, "prospect"),
		HumanApproveRef:  "PER-12",
		Owner:            "platform",
	})
	if err != nil {
		t.Fatalf("RunStage6Generate: %v", err)
	}

	if result.Contract.DecisionHint != "ready_for_review" {
		t.Fatalf("decision hint = %q, want ready_for_review", result.Contract.DecisionHint)
	}
	if result.ExampleInput["failure_reason"] != "runtime_offline" {
		t.Fatalf("example failure_reason = %v, want runtime_offline", result.ExampleInput["failure_reason"])
	}
}

func TestNewStage6ConfigFromArgsTrimsAndDefaults(t *testing.T) {
	cfg := NewStage6ConfigFromArgs(" stage3.json ", " ", " detect_x ", "", "", " PER-12 ", " platform ")

	if cfg.Stage3DigestPath != "stage3.json" {
		t.Fatalf("Stage3DigestPath = %q, want trimmed", cfg.Stage3DigestPath)
	}
	if cfg.CandidateJSONPath != "" {
		t.Fatalf("CandidateJSONPath = %q, want empty", cfg.CandidateJSONPath)
	}
	if cfg.ToolName != "detect_x" || cfg.HumanApproveRef != "PER-12" || cfg.Owner != "platform" {
		t.Fatalf("trimmed config fields not set correctly: %#v", cfg)
	}
	if cfg.ProspectDir != defaultStage6ProspectDir {
		t.Fatalf("ProspectDir = %q, want default", cfg.ProspectDir)
	}
	if cfg.ManifestPath != filepath.Join(defaultStage6ProspectDir, defaultStage6ManifestFile) {
		t.Fatalf("ManifestPath = %q, want default manifest", cfg.ManifestPath)
	}
	if cfg.GeneratedAt.IsZero() {
		t.Fatal("GeneratedAt should be defaulted")
	}
}

func TestRunStage6GenerateErrorsGivenBadInputs(t *testing.T) {
	tmp := t.TempDir()
	candidatePath := filepath.Join(tmp, "contract.json")
	writeStage6TestContract(t, candidatePath, stage6TestContract("detect_agent_error"))

	tests := []struct {
		name string
		cfg  Stage6Config
		want string
	}{
		{name: "missing human approval ref", cfg: Stage6Config{CandidateJSONPath: candidatePath, Owner: "platform"}, want: "human approve ref"},
		{name: "missing owner", cfg: Stage6Config{CandidateJSONPath: candidatePath, HumanApproveRef: "PER-12"}, want: "owner"},
		{name: "missing input source", cfg: Stage6Config{HumanApproveRef: "PER-12", Owner: "platform"}, want: "candidate-json or stage3-digest"},
		{name: "mutually exclusive input source", cfg: Stage6Config{CandidateJSONPath: candidatePath, Stage3DigestPath: candidatePath, HumanApproveRef: "PER-12", Owner: "platform"}, want: "mutually exclusive"},
		{name: "stage3 missing tool", cfg: Stage6Config{Stage3DigestPath: candidatePath, HumanApproveRef: "PER-12", Owner: "platform"}, want: "tool"},
		{name: "missing source cluster", cfg: Stage6Config{CandidateJSONPath: writeStage6RawContract(t, tmp, `{"suggested_name":"detect_x","example_input":{"failure_reason":"x"}}`), HumanApproveRef: "PER-12", Owner: "platform"}, want: "source_cluster_id"},
		{name: "empty normalized tool name", cfg: Stage6Config{CandidateJSONPath: writeStage6RawContract(t, tmp, `{"suggested_name":"///","source_signature_key":"x","example_input":{"failure_reason":"x"}}`), HumanApproveRef: "PER-12", Owner: "platform"}, want: "tool name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RunStage6Generate(tt.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRunStage6GenerateReturnsParseAndSelectionErrors(t *testing.T) {
	tmp := t.TempDir()
	_, err := RunStage6Generate(Stage6Config{CandidateJSONPath: filepath.Join(tmp, "missing.json"), HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil || !strings.Contains(err.Error(), "read candidate json") {
		t.Fatalf("read candidate error = %v, want read candidate json", err)
	}

	badJSON := writeStage6RawContract(t, tmp, "{")
	_, err = RunStage6Generate(Stage6Config{CandidateJSONPath: badJSON, HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil || !strings.Contains(err.Error(), "parse candidate json") {
		t.Fatalf("parse error = %v, want parse candidate json", err)
	}

	_, err = RunStage6Generate(Stage6Config{Stage3DigestPath: filepath.Join(tmp, "missing-stage3.json"), ToolName: "detect_x", HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil || !strings.Contains(err.Error(), "read stage3 digest") {
		t.Fatalf("read stage3 error = %v, want read stage3 digest", err)
	}

	badStage3 := writeStage6RawContract(t, tmp, "{")
	_, err = RunStage6Generate(Stage6Config{Stage3DigestPath: badStage3, ToolName: "detect_x", HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil || !strings.Contains(err.Error(), "parse stage3 digest") {
		t.Fatalf("parse stage3 error = %v, want parse stage3 digest", err)
	}

	stage3Path := filepath.Join(tmp, "stage3.json")
	stage3 := Stage3Result{
		WindowDuration: "24h0m0s",
		TotalEvents:    3,
		CandidateDettools: []Stage3CandidateDettool{
			{SuggestedName: "detect_present", SourceSignatureKey: "present", ExpectedDeterminismGain: 0.8},
		},
	}
	raw, marshalErr := json.Marshal(stage3)
	if marshalErr != nil {
		t.Fatalf("marshal stage3: %v", marshalErr)
	}
	if writeErr := os.WriteFile(stage3Path, raw, 0o644); writeErr != nil {
		t.Fatalf("write stage3: %v", writeErr)
	}
	_, err = RunStage6Generate(Stage6Config{Stage3DigestPath: stage3Path, ToolName: "detect_missing", HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("selection error = %v, want not found", err)
	}
}

func TestRunStage6GenerateReturnsFilesystemErrors(t *testing.T) {
	tmp := t.TempDir()
	candidatePath := filepath.Join(tmp, "contract.json")
	writeStage6TestContract(t, candidatePath, stage6TestContract("detect_agent_error"))

	blockingProspectFile := filepath.Join(tmp, "prospect-file")
	if err := os.WriteFile(blockingProspectFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking prospect file: %v", err)
	}
	_, err := RunStage6Generate(Stage6Config{CandidateJSONPath: candidatePath, ProspectDir: filepath.Join(blockingProspectFile, "child"), HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil {
		t.Fatal("expected mkdir error, got nil")
	}

	prospectDir := filepath.Join(tmp, "prospect")
	manifestPath := filepath.Join(tmp, "bad-manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write bad manifest: %v", err)
	}
	_, err = RunStage6Generate(Stage6Config{CandidateJSONPath: candidatePath, ProspectDir: prospectDir, ManifestPath: manifestPath, HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil || !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("manifest error = %v, want parse manifest", err)
	}

	candidateBlockDir := filepath.Join(tmp, "candidate-block")
	if err := os.MkdirAll(filepath.Join(candidateBlockDir, "detect_agent_error_candidate.go"), 0o755); err != nil {
		t.Fatalf("create candidate blocking dir: %v", err)
	}
	_, err = RunStage6Generate(Stage6Config{CandidateJSONPath: candidatePath, ProspectDir: candidateBlockDir, HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil {
		t.Fatal("expected candidate write error, got nil")
	}

	testBlockDir := filepath.Join(tmp, "test-block")
	if err := os.MkdirAll(filepath.Join(testBlockDir, "detect_agent_error_candidate_test.go"), 0o755); err != nil {
		t.Fatalf("create test blocking dir: %v", err)
	}
	_, err = RunStage6Generate(Stage6Config{CandidateJSONPath: candidatePath, ProspectDir: testBlockDir, HumanApproveRef: "PER-12", Owner: "platform"})
	if err == nil {
		t.Fatal("expected test write error, got nil")
	}
}

func TestReadStage6ManifestHandlesMissingAndInvalidFiles(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "missing", "manifest.json")
	manifest, err := readStage6Manifest(missing)
	if err != nil {
		t.Fatalf("read missing manifest: %v", err)
	}
	if manifest.Version != stage6ManifestVersion || len(manifest.Items) != 0 {
		t.Fatalf("missing manifest fallback = %#v", manifest)
	}

	invalid := filepath.Join(tmp, "invalid.json")
	if err := os.WriteFile(invalid, []byte("{"), 0o644); err != nil {
		t.Fatalf("write invalid manifest: %v", err)
	}
	if _, err := readStage6Manifest(invalid); err == nil {
		t.Fatal("expected invalid manifest error, got nil")
	}

	empty := filepath.Join(tmp, "empty.json")
	if err := os.WriteFile(empty, []byte(" \n"), 0o644); err != nil {
		t.Fatalf("write empty manifest: %v", err)
	}
	manifest, err = readStage6Manifest(empty)
	if err != nil {
		t.Fatalf("read empty manifest: %v", err)
	}
	if len(manifest.Items) != 0 {
		t.Fatalf("empty manifest items len = %d, want 0", len(manifest.Items))
	}

	noItems := filepath.Join(tmp, "no-items.json")
	if err := os.WriteFile(noItems, []byte(`{"version":"1"}`), 0o644); err != nil {
		t.Fatalf("write no-items manifest: %v", err)
	}
	manifest, err = readStage6Manifest(noItems)
	if err != nil {
		t.Fatalf("read no-items manifest: %v", err)
	}
	if manifest.Items == nil {
		t.Fatal("items should be normalized to an empty slice")
	}

	if _, err := readStage6Manifest(tmp); err == nil {
		t.Fatal("expected directory read error, got nil")
	}
}

func TestStage6TemplateHelpersUseFallbacks(t *testing.T) {
	data := buildStage6TemplateData(Stage5ToolContract{
		SuggestedName:      "detect_x",
		SourceSignatureKey: "cluster",
		ExampleOutput: map[string]any{
			"matched": "not-bool",
		},
	}, Stage6Config{HumanApproveRef: "ref", Owner: "owner"}, "2026-06-14T16:00:00Z")

	if data.Decision != "ready_for_review" {
		t.Fatalf("decision = %q, want fallback", data.Decision)
	}
	if !data.Matched {
		t.Fatal("matched should use true fallback for non-bool values")
	}
	if got := stage6StringValue(nil, "missing"); got != "" {
		t.Fatalf("nil string value = %q, want empty", got)
	}
	if got := stage6StringValue(map[string]any{"x": 1}, "x"); got != "" {
		t.Fatalf("non-string value = %q, want empty", got)
	}
	if got := stage6BoolValue(nil, "missing", false); got {
		t.Fatal("nil bool value should use false fallback")
	}
}

func TestRenderStage6TemplateReturnsParseAndFormatErrors(t *testing.T) {
	if _, err := renderStage6Template("{{", stage6CandidateTemplateData{}); err == nil {
		t.Fatal("expected template parse error, got nil")
	}
	if _, err := renderStage6Template(`{{template "missing" .}}`, stage6CandidateTemplateData{}); err == nil {
		t.Fatal("expected template execution error, got nil")
	}
	if _, err := renderStage6Template("package prospect\nfunc {", stage6CandidateTemplateData{}); err == nil {
		t.Fatal("expected go format error, got nil")
	}
}

func TestUpsertStage6ManifestAppendsAndSorts(t *testing.T) {
	tmp := t.TempDir()
	manifestPath := filepath.Join(tmp, "manifest.json")

	first := Stage6ManifestItem{ToolName: "z_tool", SourceClusterID: "b", Status: stage6CandidateStatus, GeneratedAt: "2026-06-14T16:00:00Z"}
	second := Stage6ManifestItem{ToolName: "a_tool", SourceClusterID: "a", Status: stage6CandidateStatus, GeneratedAt: "2026-06-14T16:01:00Z"}
	if err := upsertStage6Manifest(manifestPath, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	if err := upsertStage6Manifest(manifestPath, second); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	manifest := readStage6TestManifest(t, manifestPath)
	if len(manifest.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(manifest.Items))
	}
	if manifest.Items[0].ToolName != "a_tool" || manifest.Items[1].ToolName != "z_tool" {
		t.Fatalf("items not sorted: %#v", manifest.Items)
	}

	blockingPath := filepath.Join(tmp, "blocking-file", "manifest.json")
	if err := os.WriteFile(filepath.Dir(blockingPath), []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	if err := upsertStage6Manifest(blockingPath, first); err == nil {
		t.Fatal("expected mkdir error from blocked manifest parent, got nil")
	}
}

func stage6TestContract(name string) Stage5ToolContract {
	return Stage5ToolContract{
		Rank:               1,
		SuggestedName:      name,
		SourceSignatureKey: "agent_error::E_PARSE::loop",
		ExampleInput: map[string]any{
			"failure_reason":  "agent_error",
			"error_signature": "E_PARSE",
			"loop_signature":  "loop",
			"example_task_id": "task-1",
		},
		ExampleOutput: map[string]any{
			"decision":       "ready_for_candidate",
			"matched":        true,
			"source_cluster": "agent_error::E_PARSE::loop",
		},
		DecisionHint: "ready_for_candidate",
	}
}

func writeStage6TestContract(t *testing.T, path string, contract Stage5ToolContract) {
	t.Helper()
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("marshal contract: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
}

func writeStage6RawContract(t *testing.T, dir string, raw string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, strings.ReplaceAll(t.Name(), "/", "_")+"-*.json")
	if err != nil {
		t.Fatalf("create raw contract: %v", err)
	}
	path := f.Name()
	if _, err := f.WriteString(raw); err != nil {
		t.Fatalf("write raw contract: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close raw contract: %v", err)
	}
	return path
}

func readStage6TestManifest(t *testing.T, path string) Stage6Manifest {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest Stage6Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return manifest
}
