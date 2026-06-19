package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// artifactEmitInput describes a single artifact to write under the task's
// artifact directory.
type artifactEmitInput struct {
	Filename string          `json:"filename"`
	Format   string          `json:"format"`  // json | markdown | text (default: text)
	Content  json.RawMessage `json:"content"` // JSON value for "json"; JSON string for markdown/text
}

func artifactEmitTool() Tool {
	return Tool{
		Name:        "artifact_emit",
		Description: "Write a structured artifact (JSON, Markdown, or text) to the task artifact directory. Other pipeline steps and the UI consume these artifacts. MUST use instead of echo/cat > file — direct writes skip audit logging, path scoping, and artifact registry. The filename must stay within the artifact directory. This is the only tool that writes files, and it never touches repository sources.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "filename": {"type": "string", "description": "Artifact filename, relative to the artifact directory (no absolute paths or ..)."},
    "format": {"type": "string", "enum": ["json", "markdown", "text"], "description": "Defaults to text."},
    "content": {"description": "For json: any JSON value. For markdown/text: a JSON string."}
  },
  "required": ["filename", "content"],
  "additionalProperties": false
}`),
		Handler: artifactEmitHandler,
	}
}

func artifactEmitHandler(_ context.Context, args json.RawMessage, env ToolEnv) Result {
	var in artifactEmitInput
	if err := strictUnmarshal(args, &in); err != nil {
		return Errf(CodeInvalidInput, "invalid artifact_emit input: %v", err)
	}

	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "text"
	}

	var payload []byte
	switch format {
	case "json":
		// Re-indent the provided JSON value; reject invalid JSON.
		var v any
		if err := json.Unmarshal(in.Content, &v); err != nil {
			return Errf(CodeInvalidInput, "content is not valid JSON: %v", err)
		}
		pretty, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return Errf(CodeInternal, "encode json artifact: %v", err)
		}
		payload = append(pretty, '\n')
	case "markdown", "text":
		// content must be a JSON string for textual formats.
		var s string
		if err := json.Unmarshal(in.Content, &s); err != nil {
			return Errf(CodeInvalidInput, "content must be a string for %s format: %v", format, err)
		}
		payload = []byte(s)
	default:
		return Errf(CodeInvalidInput, "unsupported format %q (want json, markdown, or text)", format)
	}

	artifact, err := writeArtifact(env.WorkDir, env.ArtifactDir, in.Filename, format, payload)
	if err != nil {
		return Errf(CodeInvalidInput, "%v", err)
	}

	return Result{
		Status:    StatusOK,
		Summary:   fmt.Sprintf("wrote %s artifact %s (%d bytes)", format, artifact.Path, len(payload)),
		Artifacts: []Artifact{artifact},
		MachineData: map[string]any{
			"path":   artifact.Path,
			"format": format,
			"bytes":  len(payload),
		},
	}
}
