package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type dotnetTestGateInput struct {
	Targets           []string          `json:"targets"`
	Configuration     string            `json:"configuration"`
	Framework         string            `json:"framework"`
	Filter            string            `json:"filter"`
	Settings          string            `json:"settings"`
	NoRestore         bool              `json:"no_restore"`
	CollectCoverage   bool              `json:"collect_coverage"`
	CoverageThreshold int               `json:"coverage_threshold"`
	MSBuildProperties map[string]string `json:"msbuild_properties"`
}

var msbuildPropertyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func dotnetTestGateTool() Tool {
	return Tool{
		Name:        "dotnet_test_gate",
		Description: "Run `dotnet test` in the working directory with structured arguments and normalize the outcome. Returns MISSING_DEPENDENCY when dotnet is unavailable and POLICY_FAILURE unless every test invocation exits 0. No shell is used.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "targets": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Optional relative solution or project paths. Empty runs dotnet test in the working directory."
    },
    "configuration": {"type": "string", "description": "Optional build configuration, e.g. Debug or Release."},
    "framework": {"type": "string", "description": "Optional target framework, e.g. net8.0."},
    "filter": {"type": "string", "description": "Optional dotnet test filter expression."},
    "settings": {"type": "string", "description": "Optional relative runsettings path."},
    "no_restore": {"type": "boolean", "description": "Pass --no-restore."},
    "collect_coverage": {"type": "boolean", "description": "Set Coverlet MSBuild coverage collection properties."},
    "coverage_threshold": {"type": "integer", "minimum": 0, "maximum": 100, "description": "When >0, set Coverlet line coverage threshold."},
    "msbuild_properties": {
      "type": "object",
      "additionalProperties": {"type": "string"},
      "description": "Additional /p:Name=Value properties. Property names are validated."
    }
  },
  "additionalProperties": false
}`),
		Handler: dotnetTestGateHandler,
	}
}

func dotnetTestGateHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	var in dotnetTestGateInput
	if err := strictUnmarshal(args, &in); err != nil {
		return Errf(CodeInvalidInput, "invalid dotnet_test_gate input: %v", err)
	}
	dotnet, err := exec.LookPath("dotnet")
	if err != nil {
		return Errf(CodeMissingDependency, "dotnet not found on PATH")
	}
	if in.CoverageThreshold < 0 || in.CoverageThreshold > 100 {
		return Errf(CodeInvalidInput, "coverage_threshold must be between 0 and 100")
	}
	targets := in.Targets
	if len(targets) == 0 {
		targets = []string{""}
	}

	var results []map[string]any
	failed := 0
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		dotnetArgs, err := buildDotnetTestArgs(env.WorkDir, target, in)
		if err != nil {
			return Errf(CodeInvalidInput, "%v", err)
		}
		start := time.Now()
		out, code, _ := runCommand(ctx, env.WorkDir, dotnet, dotnetArgs...)
		passed := code == 0
		if !passed {
			failed++
		}
		results = append(results, map[string]any{
			"target":      target,
			"command":     "dotnet " + strings.Join(dotnetArgs, " "),
			"exit_code":   code,
			"passed":      passed,
			"duration_ms": time.Since(start).Milliseconds(),
			"output_tail": tail(out, testOutputTailBytes),
		})
	}

	data := map[string]any{
		"results":    results,
		"ran":        len(results),
		"failed":     failed,
		"all_passed": failed == 0,
		"work_dir":   env.WorkDir,
	}
	if failed > 0 {
		return Result{
			Status:      StatusError,
			ErrorCode:   CodePolicyFailure,
			Summary:     fmt.Sprintf("%d of %d dotnet test invocation(s) failed", failed, len(results)),
			MachineData: data,
			Retryable:   false,
		}
	}
	return OK(fmt.Sprintf("all %d dotnet test invocation(s) passed", len(results)), data)
}

func buildDotnetTestArgs(workDir, target string, in dotnetTestGateInput) ([]string, error) {
	args := []string{"test"}
	if strings.TrimSpace(target) != "" {
		if err := validateRelativePath(workDir, target, "target"); err != nil {
			return nil, err
		}
		args = append(args, target)
	}
	if in.Configuration != "" {
		args = append(args, "--configuration", in.Configuration)
	}
	if in.Framework != "" {
		args = append(args, "--framework", in.Framework)
	}
	if in.Filter != "" {
		args = append(args, "--filter", in.Filter)
	}
	if in.Settings != "" {
		if err := validateRelativePath(workDir, in.Settings, "settings"); err != nil {
			return nil, err
		}
		args = append(args, "--settings", in.Settings)
	}
	if in.NoRestore {
		args = append(args, "--no-restore")
	}
	props := map[string]string{}
	for k, v := range in.MSBuildProperties {
		if !msbuildPropertyName.MatchString(k) {
			return nil, fmt.Errorf("invalid MSBuild property name %q", k)
		}
		props[k] = v
	}
	if in.CollectCoverage {
		props["CollectCoverage"] = "true"
		props["CoverletOutputFormat"] = "cobertura"
		props["CoverletOutput"] = "./coverage/"
	}
	if in.CoverageThreshold > 0 {
		props["Threshold"] = fmt.Sprintf("%d", in.CoverageThreshold)
		props["ThresholdType"] = "line"
		props["ThresholdStat"] = "total"
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := props[k]
		args = append(args, "/p:"+k+"="+v)
	}
	return args, nil
}

func validateRelativePath(workDir, path, field string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be relative: %s", field, path)
	}
	clean := filepath.Clean(path)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return fmt.Errorf("%s path escapes the working directory: %s", field, path)
	}
	abs := filepath.Join(workDir, clean)
	rel, err := filepath.Rel(workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s path escapes the working directory: %s", field, path)
	}
	return nil
}
