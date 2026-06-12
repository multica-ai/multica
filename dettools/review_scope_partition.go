package step

import "strings"

// Run partitions changed or assigned files into Review Team specialist scopes.
//
// Input:
//   - files or changed_files: []string
//
// Output machine_data:
//   - python_files, dotnet_files, devops_files, misc_files: []string
//   - required_specialists: []string, values python/dotnet/devops
//   - required_result_headings: []string, headings the orchestrator waits for.
func Run(input map[string]any) map[string]any {
	files := stringSlice(firstPresent(input, "files", "changed_files", "assigned_files"))
	seen := map[string]bool{}
	var pythonFiles []any
	var dotnetFiles []any
	var devopsFiles []any
	var miscFiles []any

	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		py := isPythonScope(file)
		dotnet := isDotnetScope(file)
		devops := isDevOpsScope(file)
		if py {
			pythonFiles = append(pythonFiles, file)
		}
		if dotnet {
			dotnetFiles = append(dotnetFiles, file)
		}
		if devops {
			devopsFiles = append(devopsFiles, file)
		}
		if !py && !dotnet && !devops {
			miscFiles = append(miscFiles, file)
		}
	}

	var specialists []any
	var headings []any
	if len(pythonFiles) > 0 {
		specialists = append(specialists, "python")
		headings = append(headings, "## Python Review Result")
	}
	if len(dotnetFiles) > 0 {
		specialists = append(specialists, "dotnet")
		headings = append(headings, "## Dotnet Review Result")
	}
	if len(devopsFiles) > 0 {
		specialists = append(specialists, "devops")
		headings = append(headings, "## DevOps Review Result")
	}

	return map[string]any{
		"status":  "ok",
		"summary": "Partitioned review scope",
		"machine_data": map[string]any{
			"python_files":             pythonFiles,
			"dotnet_files":             dotnetFiles,
			"devops_files":             devopsFiles,
			"misc_files":               miscFiles,
			"required_specialists":     specialists,
			"required_result_headings": headings,
		},
	}
}

func isPythonScope(path string) bool {
	p := strings.ToLower(path)
	base := basename(p)
	return strings.HasSuffix(p, ".py") ||
		base == "pyproject.toml" ||
		strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt") ||
		strings.HasPrefix(base, "pipfile") ||
		base == "poetry.lock" ||
		base == "uv.lock" ||
		base == "tox.ini" ||
		base == "noxfile.py" ||
		base == "hatch.toml"
}

func isDotnetScope(path string) bool {
	p := strings.ToLower(path)
	base := basename(p)
	return strings.HasSuffix(p, ".cs") ||
		strings.HasSuffix(p, ".csproj") ||
		strings.HasSuffix(p, ".sln") ||
		strings.HasSuffix(p, ".fsproj") ||
		strings.HasSuffix(p, ".vbproj") ||
		base == "directory.build.props" ||
		base == "directory.build.targets" ||
		strings.HasPrefix(base, "appsettings") && strings.HasSuffix(base, ".json")
}

func isDevOpsScope(path string) bool {
	p := strings.ToLower(path)
	base := basename(p)
	return strings.HasPrefix(base, "azure-pipelines") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) ||
		strings.HasPrefix(p, ".azure-pipelines/") ||
		strings.HasPrefix(p, ".pipelines/") ||
		strings.HasPrefix(p, ".github/workflows/") && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")) ||
		base == ".gitlab-ci.yml" ||
		strings.HasPrefix(base, "jenkinsfile") ||
		strings.HasPrefix(p, ".circleci/") ||
		strings.HasPrefix(p, ".buildkite/") ||
		strings.HasPrefix(base, "dockerfile") ||
		strings.HasPrefix(base, "docker-compose") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) ||
		base == ".dockerignore" ||
		strings.Contains(base, "entrypoint") && (strings.HasSuffix(base, ".sh") || strings.HasSuffix(base, ".ps1")) ||
		strings.HasPrefix(p, "k8s/") ||
		strings.HasPrefix(p, "kubernetes/") ||
		strings.HasPrefix(p, "helm/") ||
		strings.HasPrefix(p, "charts/") ||
		base == "chart.yaml" ||
		strings.HasPrefix(base, "values") && strings.HasSuffix(base, ".yaml") ||
		base == "kustomization.yaml" ||
		strings.HasSuffix(base, ".tf") ||
		strings.HasSuffix(base, ".tfvars") ||
		strings.HasSuffix(base, ".bicep") ||
		strings.Contains(p, "cloudformation") ||
		strings.Contains(p, "pulumi") ||
		strings.Contains(p, "ansible") ||
		base == "makefile" ||
		strings.HasSuffix(base, ".sh") ||
		strings.HasSuffix(base, ".ps1")
}

func basename(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func firstPresent(input map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := input[key]; ok {
			return v
		}
	}
	return nil
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
