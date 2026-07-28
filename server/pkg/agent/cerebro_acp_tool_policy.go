package agent

// Fork-owned ACP tool-policy adapter for the Agent Client Protocol family
// (hermes, kimi, kiro — all three drive hermesClient).
//
// These runtimes have no provider-native hook file and no Firtal harness, so
// for a long time they had no mandatory before-call seam at all and the daemon
// refused to start them. They do not need one: ACP already defines it. Before
// an ACP agent runs a tool it asks its CLIENT for permission via
// `session/request_permission`, and the daemon IS that client. That request is
// the same before-call seam Claude's PreToolUse hook and Codex's approval
// callback give us — it fires before execution, for built-in and MCP tools
// alike, and the agent blocks until we answer.
//
// Two properties make it a real gate rather than a formality:
//
//   - The agent must be launched WITHOUT its auto-approve mode. hermes.go
//     therefore no longer sets HERMES_YOLO_MODE, which suppressed the request
//     entirely and was the actual hole.
//   - We never answer with an `allow_always`-kind option. That answer tells the
//     agent to remember the approval and stop asking, which would silently end
//     enforcement for the rest of the session. Only the once-kinds are used, so
//     every single call is resolved.
//
// A missing policy callback or an unparseable request denies. The upstream
// client keeps only the seam that routes the request here.

import (
	"context"
	"encoding/json"
	"log/slog"
)

// ACP permission option kinds, from the Agent Client Protocol
// `RequestPermissionOption.kind` enum.
const (
	acpOptionAllowOnce    = "allow_once"
	acpOptionAllowAlways  = "allow_always"
	acpOptionRejectOnce   = "reject_once"
	acpOptionRejectAlways = "reject_always"
)

// acpPermissionRequest is the subset of `session/request_permission` params
// this gate reads: which tool is about to run, and which answers the agent
// will accept.
type acpPermissionRequest struct {
	ToolCall struct {
		ToolCallID string         `json:"toolCallId"`
		Name       string         `json:"name"`
		Title      string         `json:"title"`
		Kind       string         `json:"kind"`
		RawInput   map[string]any `json:"rawInput"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

// acpToolName derives the canonical tool key from an ACP tool call. ACP sends a
// human title ("read: main.go") rather than the tool name, so this reuses the
// same title→name mapping the session-update path already uses, and falls back
// to the explicit name field that some ACP backends do send.
func acpToolName(req acpPermissionRequest) string {
	if name := hermesToolNameFromTitle(req.ToolCall.Title, req.ToolCall.Kind); name != "" {
		return name
	}
	if req.ToolCall.Name != "" {
		return req.ToolCall.Name
	}
	if req.ToolCall.Title != "" {
		return req.ToolCall.Title
	}
	return "unknown"
}

// acpSelectOption picks the option id to answer with. On allow it takes a
// once-scoped approval so the next call is resolved again; it never selects an
// always-scoped option, because that ends enforcement for the session. On deny
// it prefers a once-scoped rejection so a later, differently-scoped call is
// still asked about. An empty result means the agent offered nothing usable and
// the caller must cancel instead of selecting.
func acpSelectOption(options []struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}, allowed bool) string {
	want := acpOptionRejectOnce
	fallback := acpOptionRejectAlways
	if allowed {
		want = acpOptionAllowOnce
		fallback = ""
	}
	for _, opt := range options {
		if opt.Kind == want {
			return opt.OptionID
		}
	}
	if fallback != "" {
		for _, opt := range options {
			if opt.Kind == fallback {
				return opt.OptionID
			}
		}
	}
	return ""
}

// cerebroACPPermissionResult resolves one ACP permission request against the
// task's tool policy and returns the JSON-RPC `result` object to answer with.
//
// Every failure path denies: no policy callback, unparseable params, or an
// options list with no rejection option (answered as `cancelled`, which ACP
// defines as "the tool call did not proceed").
func cerebroACPPermissionResult(ctx context.Context, policy func(context.Context, string, map[string]any) (bool, string), logger *slog.Logger, rawParams json.RawMessage) map[string]any {
	if logger == nil {
		logger = slog.Default()
	}

	var req acpPermissionRequest
	if err := json.Unmarshal(rawParams, &req); err != nil {
		logger.Warn("acp: permission request unparseable — denying", "error", err)
		return acpCancelledResult()
	}

	tool := acpToolName(req)

	if policy == nil {
		logger.Warn("acp: no tool policy wired — denying", "tool", tool)
		return acpDenyResult(req, tool, logger)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	allowed, reason := policy(ctx, tool, req.ToolCall.RawInput)
	if !allowed {
		logger.Warn("acp: blocked by Multica tool policy", "tool", tool, "reason", reason)
		return acpDenyResult(req, tool, logger)
	}

	optionID := acpSelectOption(req.Options, true)
	if optionID == "" {
		// The agent offered no once-scoped approval. Selecting an
		// always-scoped one would end enforcement for the session, so
		// the call does not proceed.
		logger.Warn("acp: no once-scoped approval offered — denying", "tool", tool)
		return acpCancelledResult()
	}
	logger.Debug("acp: allowed by Multica tool policy", "tool", tool)
	return acpSelectedResult(optionID)
}

func acpDenyResult(req acpPermissionRequest, tool string, logger *slog.Logger) map[string]any {
	optionID := acpSelectOption(req.Options, false)
	if optionID == "" {
		logger.Warn("acp: no rejection option offered — cancelling call", "tool", tool)
		return acpCancelledResult()
	}
	return acpSelectedResult(optionID)
}

func acpSelectedResult(optionID string) map[string]any {
	return map[string]any{
		"outcome": map[string]any{
			"outcome":  "selected",
			"optionId": optionID,
		},
	}
}

func acpCancelledResult() map[string]any {
	return map[string]any{
		"outcome": map[string]any{
			"outcome": "cancelled",
		},
	}
}
