package agent

// DevEco Code model discovery. Self-contained and independent of the OpenCode
// model parser: DevEco maintains its own catalog and CLI, so its discovery code
// lives here rather than reusing parseOpenCodeModels. A missing CLI or empty
// catalog returns an empty list so the caller (the runtime model picker) falls
// back to manual model entry instead of erroring.

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// discoverDevecoModels enumerates the `deveco models` catalog. It shells out to
// the DevEco Code CLI and parses `provider/model` rows. Discovery failures
// (binary missing, non-zero exit, unparseable output) silently yield an empty
// list so model selection degrades to manual entry rather than blocking.
func discoverDevecoModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "deveco"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	// `deveco models` may sync its hosted catalog over the network on first
	// run; allow a generous timeout so the picker isn't empty on a cold start.
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "models")
	hideAgentWindow(cmd)
	out, _ := cmd.Output()
	models := parseDevecoModels(string(out))
	if len(models) == 0 {
		return []Model{}, nil
	}
	return models, nil
}

// parseDevecoModels extracts `provider/model` IDs from `deveco models` output.
// Each non-empty line's first whitespace-delimited token is treated as a model
// id when it contains a `/` and does not look like a header row or JSON. Output
// is de-duplicated, preserving first-seen order. Per-model variant/thinking
// annotation is not parsed here; the thinking-level picker is not offered for
// DevEco in this revision (see thinking.go).
func parseDevecoModels(output string) []Model {
	var models []Model
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := fields[0]
		// Skip JSON objects/arrays, quoted strings, and header rows such as
		// "PROVIDER/MODEL".
		if strings.HasPrefix(id, "{") || strings.HasPrefix(id, "[") || strings.HasPrefix(id, "\"") {
			continue
		}
		if !strings.Contains(id, "/") {
			continue
		}
		if id == strings.ToUpper(id) {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		provider := ""
		if slash := strings.Index(id, "/"); slash > 0 {
			provider = id[:slash]
		}
		models = append(models, Model{ID: id, Label: id, Provider: provider})
	}
	return models
}
