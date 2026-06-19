package dettools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"
)

// protocolVersion is the MCP revision this server defaults to advertising. When
// a client requests a specific version in initialize, we echo it back so the
// handshake matches the client's expectation.
const protocolVersion = "2025-06-18"

// JSON-RPC error codes (subset of the spec we use).
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
)

// ServerInfo identifies this server in the MCP initialize response.
type ServerInfo struct {
	Name    string
	Version string
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve runs the MCP stdio loop: it reads newline-delimited JSON-RPC messages
// from in and writes responses to out until in reaches EOF or ctx is cancelled.
// out is the protocol channel and must carry nothing but JSON-RPC frames — all
// logging goes to env.Logger (stderr).
func Serve(ctx context.Context, in io.Reader, out io.Writer, reg *Registry, info ServerInfo, env ToolEnv, logger *slog.Logger) error {
	reader := bufio.NewReaderSize(in, 1<<20)
	enc := json.NewEncoder(out)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, readErr := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			handleLine(ctx, trimmed, enc, reg, info, env, logger)
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

func handleLine(ctx context.Context, line []byte, enc *json.Encoder, reg *Registry, info ServerInfo, env ToolEnv, logger *slog.Logger) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		writeError(enc, nil, rpcParseError, "parse error")
		return
	}
	// A JSON-RPC notification is a request without an id; it gets no response.
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		writeResult(enc, req.ID, initializeResult(req.Params, info))
	case "notifications/initialized", "notifications/cancelled":
		// Notifications: acknowledge silently.
	case "ping":
		if !isNotification {
			writeResult(enc, req.ID, map[string]any{})
		}
	case "tools/list":
		if !isNotification {
			writeResult(enc, req.ID, map[string]any{"tools": reg.Descriptors()})
		}
	case "tools/call":
		if !isNotification {
			writeResult(enc, req.ID, handleToolCall(ctx, req.Params, reg, env, logger))
		}
	default:
		if !isNotification {
			writeError(enc, req.ID, rpcMethodNotFound, "method not found: "+req.Method)
		}
	}
}

func initializeResult(params json.RawMessage, info ServerInfo) map[string]any {
	version := protocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(params, &p) == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": info.Name, "version": info.Version},
	}
}

func handleToolCall(ctx context.Context, params json.RawMessage, reg *Registry, env ToolEnv, logger *slog.Logger) map[string]any {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return toolResultPayload(Errf(CodeInvalidInput, "invalid tools/call params: %v", err))
	}
	tool, ok := reg.Lookup(call.Name)
	if !ok {
		return toolResultPayload(Errf(CodeInvalidInput, "unknown or disabled tool: %q", call.Name))
	}
	args := call.Arguments
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}

	cctx := ctx
	if env.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, env.Timeout)
		defer cancel()
	}

	start := time.Now()
	res := runHandler(cctx, tool, args, env)
	if logger != nil {
		// Audit record: tool, normalized outcome, duration, input size, and
		// artifact paths. The raw argument payload is intentionally not logged
		// (it may carry repo-specific or sensitive command strings); its byte
		// size is recorded instead.
		logger.Info("dettools invocation",
			"tool", tool.Name,
			"status", res.Status,
			"error_code", res.ErrorCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"work_dir", env.WorkDir,
			"arg_bytes", len(args),
			"artifacts", artifactPaths(res.Artifacts),
		)
	}
	return toolResultPayload(res)
}

// runHandler invokes a tool with panic recovery and timeout normalization, so a
// buggy handler returns INTERNAL_ERROR and a deadline returns TIMEOUT instead of
// crashing the server or leaking a confusing message.
func runHandler(ctx context.Context, tool Tool, args json.RawMessage, env ToolEnv) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = Errf(CodeInternal, "tool %q panicked: %v", tool.Name, r)
		}
	}()
	res = tool.Handler(ctx, args, env)
	if ctx.Err() == context.DeadlineExceeded {
		return Errf(CodeTimeout, "tool %q timed out after %s", tool.Name, env.Timeout)
	}
	return res
}

// toolResultPayload renders a Result into an MCP tools/call result: the envelope
// is provided both as human/agent-readable text content and as structuredContent
// for programmatic consumers. isError mirrors a non-ok status so the agent knows
// the call did not succeed.
func toolResultPayload(res Result) map[string]any {
	text, _ := json.MarshalIndent(res, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(text)}},
		"structuredContent": res,
		"isError":           res.Status != StatusOK,
	}
}

// artifactPaths returns the relative paths of the given artifacts for audit
// logging.
func artifactPaths(artifacts []Artifact) []string {
	if len(artifacts) == 0 {
		return nil
	}
	paths := make([]string, len(artifacts))
	for i, a := range artifacts {
		paths[i] = a.Path
	}
	return paths
}

func writeResult(enc *json.Encoder, id json.RawMessage, result any) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeError(enc *json.Encoder, id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}
