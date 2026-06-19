package ail

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"
)

const (
	defaultStage6ProspectDir  = "dettools/prospect"
	defaultStage6ManifestFile = "manifest.json"
	stage6ManifestVersion     = "1"
	stage6CandidateStatus     = "candidate"
	stage6InvalidInputCode    = "INVALID_INPUT"
)

// Stage6Config controls candidate scaffold generation and manifest updates.
type Stage6Config struct {
	Stage3DigestPath  string
	CandidateJSONPath string
	ToolName          string
	ProspectDir       string
	ManifestPath      string
	HumanApproveRef   string
	Owner             string
	GeneratedAt       time.Time
}

// Stage6Result records generated prospect files and the manifest entry that tracks them.
type Stage6Result struct {
	ToolName       string             `json:"tool_name"`
	CandidatePath  string             `json:"candidate_path"`
	TestPath       string             `json:"test_path"`
	ManifestPath   string             `json:"manifest_path"`
	ManifestEntry  Stage6ManifestItem `json:"manifest_entry"`
	GeneratedFiles []string           `json:"generated_files"`
	Contract       Stage5ToolContract `json:"contract"`
	ExampleInput   map[string]any     `json:"example_input"`
	ExampleOutput  map[string]any     `json:"example_output"`
}

// Stage6Manifest tracks prospect candidate lifecycle metadata.
type Stage6Manifest struct {
	Version     string               `json:"version"`
	GeneratedAt string               `json:"generated_at"`
	Items       []Stage6ManifestItem `json:"items"`
}

// Stage6ManifestItem is one generated candidate entry in dettools/prospect/manifest.json.
type Stage6ManifestItem struct {
	ToolName        string `json:"tool_name"`
	Status          string `json:"status"`
	HumanApproveRef string `json:"human_approve_ref"`
	Owner           string `json:"owner"`
	SourceClusterID string `json:"source_cluster_id"`
	GeneratedAt     string `json:"generated_at"`
	CandidatePath   string `json:"candidate_path"`
	TestPath        string `json:"test_path"`
}

type stage6CandidateTemplateData struct {
	PackageName       string
	ToolName          string
	TypeName          string
	FuncName          string
	InputType         string
	OutputType        string
	ErrorType         string
	InvalidInputCode  string
	Decision          string
	Matched           bool
	SourceClusterID   string
	FailureReason     string
	ErrorSignature    string
	LoopSignature     string
	ExampleTaskID     string
	GeneratedAt       string
	HumanApproveRef   string
	Owner             string
	ExampleInputJSON  string
	ExampleOutputJSON string
	ExampleInputGo    string
	UnknownInputGo    string
}

// NewStage6ConfigFromArgs builds Stage6Config from CLI flags.
func NewStage6ConfigFromArgs(stage3DigestPath, candidateJSONPath, toolName, prospectDir, manifestPath, humanApproveRef, owner string) Stage6Config {
	cfg := Stage6Config{
		Stage3DigestPath:  strings.TrimSpace(stage3DigestPath),
		CandidateJSONPath: strings.TrimSpace(candidateJSONPath),
		ToolName:          strings.TrimSpace(toolName),
		ProspectDir:       strings.TrimSpace(prospectDir),
		ManifestPath:      strings.TrimSpace(manifestPath),
		HumanApproveRef:   strings.TrimSpace(humanApproveRef),
		Owner:             strings.TrimSpace(owner),
	}
	return normalizeStage6Config(cfg)
}

// RunStage6Generate writes a candidate scaffold, unit-test harness, and manifest entry.
func RunStage6Generate(cfg Stage6Config) (Stage6Result, error) {
	cfg = normalizeStage6Config(cfg)
	if err := validateStage6Config(cfg); err != nil {
		return Stage6Result{}, err
	}

	contract, err := loadStage6Contract(cfg)
	if err != nil {
		return Stage6Result{}, err
	}
	if cfg.ToolName != "" {
		contract.SuggestedName = cfg.ToolName
	}
	contract.SuggestedName = normalizeStage6ToolName(contract.SuggestedName)
	if contract.SuggestedName == "" {
		return Stage6Result{}, errors.New("tool name is required")
	}
	if contract.SourceSignatureKey == "" {
		return Stage6Result{}, errors.New("source_cluster_id is required")
	}

	generatedAt := cfg.GeneratedAt.UTC().Format(time.RFC3339Nano)
	data := buildStage6TemplateData(contract, cfg, generatedAt)
	candidateBytes, err := renderStage6Template(stage6CandidateTemplate, data)
	if err != nil {
		return Stage6Result{}, err
	}
	testBytes, err := renderStage6Template(stage6CandidateTestTemplate, data)
	if err != nil {
		return Stage6Result{}, err
	}

	candidatePath := filepath.Join(cfg.ProspectDir, contract.SuggestedName+"_candidate.go")
	testPath := filepath.Join(cfg.ProspectDir, contract.SuggestedName+"_candidate_test.go")
	if err := os.MkdirAll(cfg.ProspectDir, 0o755); err != nil {
		return Stage6Result{}, err
	}
	if err := os.WriteFile(candidatePath, candidateBytes, 0o644); err != nil {
		return Stage6Result{}, err
	}
	if err := os.WriteFile(testPath, testBytes, 0o644); err != nil {
		return Stage6Result{}, err
	}

	entry := Stage6ManifestItem{
		ToolName:        contract.SuggestedName,
		Status:          stage6CandidateStatus,
		HumanApproveRef: cfg.HumanApproveRef,
		Owner:           cfg.Owner,
		SourceClusterID: contract.SourceSignatureKey,
		GeneratedAt:     generatedAt,
		CandidatePath:   filepath.ToSlash(candidatePath),
		TestPath:        filepath.ToSlash(testPath),
	}
	if err := upsertStage6Manifest(cfg.ManifestPath, entry); err != nil {
		return Stage6Result{}, err
	}

	return Stage6Result{
		ToolName:      contract.SuggestedName,
		CandidatePath: entry.CandidatePath,
		TestPath:      entry.TestPath,
		ManifestPath:  filepath.ToSlash(cfg.ManifestPath),
		ManifestEntry: entry,
		GeneratedFiles: []string{
			entry.CandidatePath,
			entry.TestPath,
			filepath.ToSlash(cfg.ManifestPath),
		},
		Contract:      contract,
		ExampleInput:  contract.ExampleInput,
		ExampleOutput: contract.ExampleOutput,
	}, nil
}

func normalizeStage6Config(cfg Stage6Config) Stage6Config {
	if cfg.ProspectDir == "" {
		cfg.ProspectDir = defaultStage6ProspectDir
	}
	if cfg.ManifestPath == "" {
		cfg.ManifestPath = filepath.Join(cfg.ProspectDir, defaultStage6ManifestFile)
	}
	if cfg.GeneratedAt.IsZero() {
		cfg.GeneratedAt = time.Now()
	}
	return cfg
}

func validateStage6Config(cfg Stage6Config) error {
	if cfg.HumanApproveRef == "" {
		return errors.New("human approve ref is required")
	}
	if cfg.Owner == "" {
		return errors.New("owner is required")
	}
	if cfg.CandidateJSONPath == "" && cfg.Stage3DigestPath == "" {
		return errors.New("candidate-json or stage3-digest is required")
	}
	if cfg.CandidateJSONPath != "" && cfg.Stage3DigestPath != "" {
		return errors.New("candidate-json and stage3-digest are mutually exclusive")
	}
	if cfg.Stage3DigestPath != "" && cfg.ToolName == "" {
		return errors.New("tool is required when using stage3-digest")
	}
	return nil
}

func loadStage6Contract(cfg Stage6Config) (Stage5ToolContract, error) {
	if cfg.CandidateJSONPath != "" {
		return readStage6ContractJSON(cfg.CandidateJSONPath)
	}
	raw, err := os.ReadFile(cfg.Stage3DigestPath)
	if err != nil {
		return Stage5ToolContract{}, fmt.Errorf("read stage3 digest: %w", err)
	}
	var stage3 Stage3Result
	if err := json.Unmarshal(raw, &stage3); err != nil {
		return Stage5ToolContract{}, fmt.Errorf("parse stage3 digest: %w", err)
	}
	digest := BuildStage5Digest(stage3)
	for _, contract := range digest.RecommendedTools {
		if contract.SuggestedName == cfg.ToolName {
			return contract, nil
		}
	}
	return Stage5ToolContract{}, fmt.Errorf("tool %q not found in stage3 digest candidates", cfg.ToolName)
}

func readStage6ContractJSON(path string) (Stage5ToolContract, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Stage5ToolContract{}, fmt.Errorf("read candidate json: %w", err)
	}
	var contract Stage5ToolContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		return Stage5ToolContract{}, fmt.Errorf("parse candidate json: %w", err)
	}
	return contract, nil
}

func buildStage6TemplateData(contract Stage5ToolContract, cfg Stage6Config, generatedAt string) stage6CandidateTemplateData {
	typeName := stage5ExportedTypeName(contract.SuggestedName)
	input := stage6StringValue(contract.ExampleInput, "failure_reason")
	errorSignature := stage6StringValue(contract.ExampleInput, "error_signature")
	loopSignature := stage6StringValue(contract.ExampleInput, "loop_signature")
	exampleTaskID := stage6StringValue(contract.ExampleInput, "example_task_id")
	decision := stage6StringValue(contract.ExampleOutput, "decision")
	if decision == "" {
		decision = contract.DecisionHint
	}
	if decision == "" {
		decision = "ready_for_review"
	}
	exampleInputJSON := compactJSON(contract.ExampleInput)
	exampleOutputJSON := compactJSON(contract.ExampleOutput)
	unknownInputJSON := compactJSON(map[string]any{"failure_reason": input, "unexpected": true})
	return stage6CandidateTemplateData{
		PackageName:       "prospect",
		ToolName:          contract.SuggestedName,
		TypeName:          typeName,
		FuncName:          stage5ExportedFuncName(contract.SuggestedName),
		InputType:         typeName + "Input",
		OutputType:        typeName + "Output",
		ErrorType:         typeName + "Error",
		InvalidInputCode:  stage6InvalidInputCode,
		Decision:          decision,
		Matched:           stage6BoolValue(contract.ExampleOutput, "matched", true),
		SourceClusterID:   contract.SourceSignatureKey,
		FailureReason:     input,
		ErrorSignature:    errorSignature,
		LoopSignature:     loopSignature,
		ExampleTaskID:     exampleTaskID,
		GeneratedAt:       generatedAt,
		HumanApproveRef:   cfg.HumanApproveRef,
		Owner:             cfg.Owner,
		ExampleInputJSON:  exampleInputJSON,
		ExampleOutputJSON: exampleOutputJSON,
		ExampleInputGo:    strconv.Quote(exampleInputJSON),
		UnknownInputGo:    strconv.Quote(unknownInputJSON),
	}
}

func renderStage6Template(tmpl string, data stage6CandidateTemplateData) ([]byte, error) {
	parsed, err := template.New("stage6").Parse(tmpl)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if err := parsed.Execute(&b, data); err != nil {
		return nil, err
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w\n%s", err, b.String())
	}
	return formatted, nil
}

func upsertStage6Manifest(path string, entry Stage6ManifestItem) error {
	manifest, err := readStage6Manifest(path)
	if err != nil {
		return err
	}
	manifest.Version = stage6ManifestVersion
	manifest.GeneratedAt = entry.GeneratedAt
	replaced := false
	for i := range manifest.Items {
		if manifest.Items[i].ToolName == entry.ToolName && manifest.Items[i].SourceClusterID == entry.SourceClusterID {
			manifest.Items[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		manifest.Items = append(manifest.Items, entry)
	}
	sort.SliceStable(manifest.Items, func(i, j int) bool {
		if manifest.Items[i].ToolName != manifest.Items[j].ToolName {
			return manifest.Items[i].ToolName < manifest.Items[j].ToolName
		}
		return manifest.Items[i].SourceClusterID < manifest.Items[j].SourceClusterID
	})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, manifest)
}

func readStage6Manifest(path string) (Stage6Manifest, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Stage6Manifest{Version: stage6ManifestVersion, Items: []Stage6ManifestItem{}}, nil
	}
	if err != nil {
		return Stage6Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Stage6Manifest{Version: stage6ManifestVersion, Items: []Stage6ManifestItem{}}, nil
	}
	var manifest Stage6Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Stage6Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.Items == nil {
		manifest.Items = []Stage6ManifestItem{}
	}
	return manifest, nil
}

func normalizeStage6ToolName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func stage6StringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func stage6BoolValue(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	if value, ok := values[key].(bool); ok {
		return value
	}
	return fallback
}

var stage6CandidateTemplate = `package {{.PackageName}}

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	{{.TypeName}}InvalidInputCode = "{{.InvalidInputCode}}"
	{{.TypeName}}SourceClusterID = "{{.SourceClusterID}}"
)

// {{.InputType}} is the strict input schema generated from Stage 5 example IO.
type {{.InputType}} struct {
	FailureReason string ` + "`json:\"failure_reason\"`" + `
	ErrorSignature string ` + "`json:\"error_signature,omitempty\"`" + `
	LoopSignature string ` + "`json:\"loop_signature,omitempty\"`" + `
	ExampleTaskID string ` + "`json:\"example_task_id,omitempty\"`" + `
}

// {{.OutputType}} is the bounded deterministic output for the candidate detector.
type {{.OutputType}} struct {
	Decision string ` + "`json:\"decision\"`" + `
	Matched bool ` + "`json:\"matched\"`" + `
	SourceCluster string ` + "`json:\"source_cluster\"`" + `
}

// {{.ErrorType}} carries stable machine-readable error codes for the generated candidate.
type {{.ErrorType}} struct {
	Code string
	Message string
}

// Error renders the stable candidate error code and message.
func (e {{.ErrorType}}) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Decode{{.InputType}} strictly decodes candidate input JSON.
func Decode{{.InputType}}(raw []byte) ({{.InputType}}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var input {{.InputType}}
	if err := dec.Decode(&input); err != nil {
		return {{.InputType}}{}, {{.ErrorType}}{Code: {{.TypeName}}InvalidInputCode, Message: err.Error()}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return {{.InputType}}{}, {{.ErrorType}}{Code: {{.TypeName}}InvalidInputCode, Message: "multiple JSON values are not allowed"}
	}
	return input, nil
}

// {{.FuncName}} evaluates the Stage 6 candidate scaffold.
func {{.FuncName}}(input {{.InputType}}) ({{.OutputType}}, error) {
	if input.FailureReason == "" {
		return {{.OutputType}}{}, {{.ErrorType}}{Code: {{.TypeName}}InvalidInputCode, Message: "failure_reason is required"}
	}
	return {{.OutputType}}{
		Decision: "{{.Decision}}",
		Matched: {{.Matched}},
		SourceCluster: {{.TypeName}}SourceClusterID,
	}, nil
}

// Run{{.TypeName}} decodes JSON input and evaluates the candidate.
func Run{{.TypeName}}(raw []byte) ({{.OutputType}}, error) {
	input, err := Decode{{.InputType}}(raw)
	if err != nil {
		return {{.OutputType}}{}, err
	}
	return {{.FuncName}}(input)
}

// Is{{.TypeName}}InvalidInput reports whether err is the candidate's INVALID_INPUT error.
func Is{{.TypeName}}InvalidInput(err error) bool {
	var candidateErr {{.ErrorType}}
	return errors.As(err, &candidateErr) && candidateErr.Code == {{.TypeName}}InvalidInputCode
}
`

var stage6CandidateTestTemplate = `package {{.PackageName}}

import "testing"

func TestRun{{.TypeName}}ReturnsExampleOutput(t *testing.T) {
	raw := []byte({{.ExampleInputGo}})

	got, err := Run{{.TypeName}}(raw)
	if err != nil {
		t.Fatalf("Run{{.TypeName}}: %v", err)
	}

	if got.Decision != "{{.Decision}}" {
		t.Fatalf("Decision = %q, want {{.Decision}}", got.Decision)
	}
	if got.SourceCluster != "{{.SourceClusterID}}" {
		t.Fatalf("SourceCluster = %q, want {{.SourceClusterID}}", got.SourceCluster)
	}
}

func TestRun{{.TypeName}}RejectsUnknownFields(t *testing.T) {
	_, err := Run{{.TypeName}}([]byte({{.UnknownInputGo}}))

	if err == nil {
		t.Fatal("expected invalid input error, got nil")
	}
	if !Is{{.TypeName}}InvalidInput(err) {
		t.Fatalf("error = %v, want INVALID_INPUT", err)
	}
}

func Test{{.FuncName}}RejectsMissingFailureReason(t *testing.T) {
	_, err := {{.FuncName}}({{.InputType}}{})

	if err == nil {
		t.Fatal("expected invalid input error, got nil")
	}
	if !Is{{.TypeName}}InvalidInput(err) {
		t.Fatalf("error = %v, want INVALID_INPUT", err)
	}
}
`
