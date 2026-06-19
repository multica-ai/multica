package dettools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// ToolEnv is the execution context handed to every tool handler. It carries the
// daemon-enforced policy (working directory, network allowance, timeout) and a
// stderr logger.
type ToolEnv struct {
	WorkDir      string
	AllowNetwork bool
	Timeout      time.Duration
	ArtifactDir  string
	Logger       *slog.Logger
}

// Handler runs a single tool invocation. Implementations must be read-only and
// must not write outside WorkDir/ArtifactDir.
type Handler func(ctx context.Context, args json.RawMessage, env ToolEnv) Result

// Tool is a registered deterministic tool.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage // advertised to the client as JSON Schema
	Handler     Handler
}

// AllToolNames returns the names of every built-in tool, in registration order.
// Exposed so other packages (e.g. the workspace tool CRUD) can reason about the
// built-in catalog without depending on internals.
func AllToolNames() []string {
	tools := allTools()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// allTools returns every implemented tool. Add new tools here.
func allTools() []Tool {
	return []Tool{
		repoFactsTool(),
		policyCheckTool(),
		buildProbeTool(),
		testGateTool(),
		dotnetTestGateTool(),
		diffSummarizeTool(),
		artifactEmitTool(),
		agentImprovementEvaluateTool(),
	}
}

// Registry holds the tools exposed by one server instance, filtered to an
// allowlist.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry builds a registry containing only the named tools. An empty
// allowed slice exposes every implemented tool (used for direct CLI invocation);
// the daemon always passes an explicit allowlist.
func NewRegistry(allowed []string) *Registry {
	allow := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allow[a] = true
	}
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range allTools() {
		if len(allowed) > 0 && !allow[t.Name] {
			continue
		}
		r.tools[t.Name] = t
		r.order = append(r.order, t.Name)
	}
	return r
}

// Add registers an extra tool (e.g. a workspace-authored step) unconditionally,
// bypassing the allowlist filter. The daemon is the policy authority for these —
// it only delivers ones already enabled and permitted for the agent — so the
// registry serves what it is given. A built-in tool of the same name always
// wins: the collision is ignored rather than shadowing the compiled handler.
func (r *Registry) Add(t Tool) bool {
	if _, exists := r.tools[t.Name]; exists {
		return false
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
	return true
}

// Lookup returns the tool with the given name, if exposed.
func (r *Registry) Lookup(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Descriptors returns the tool list for an MCP tools/list response, in
// registration order.
func (r *Registry) Descriptors() []map[string]any {
	out := make([]map[string]any, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return out
}
