package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// A local MCP server that presents this workspace's plugin hooks as tools.
//
// Unlike the Remote MCP broker beside it, there is no upstream MCP server to
// proxy to: a plugin author writes an HTTP endpoint and never learns what MCP
// is. This synthesises the protocol from the manifest — the hook description
// becomes the tool description, the hook's input_schema becomes the tool's.
//
// A tool call does NOT go to the plugin from here. It goes back to Multica,
// which makes the signed request. The daemon runs on someone's laptop; putting
// the signing secret there would mean every machine running an agent holds a
// credential that can impersonate the server to every plugin backend. Routing
// through the server also means the rate limit, circuit breaker, `net:` check
// and invocation record are the same code for all four triggers.

const (
	pluginHookMCPProtocolVersion = "2024-11-05"
	pluginHookMCPMaxRequestBytes = 1 << 20
	pluginHookMCPCallTimeout     = 60 * time.Second
)

// pluginHookInvoker performs one hook call against the Multica server.
type pluginHookInvoker func(ctx context.Context, taskID, installationID, hookKey string, input json.RawMessage) (json.RawMessage, error)

type pluginHookMCPServer struct {
	taskID string
	tools  []PluginHookTool
	byName map[string]PluginHookTool
	invoke pluginHookInvoker
	path   string
	logger *slog.Logger
}

type pluginHookMCPSet struct {
	server   *http.Server
	listener net.Listener
	once     sync.Once
}

func (set *pluginHookMCPSet) Close() {
	if set == nil {
		return
	}
	set.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if set.server != nil {
			_ = set.server.Shutdown(ctx)
		}
		if set.listener != nil {
			_ = set.listener.Close()
		}
	})
}

// startTaskPluginHookMCP starts the tool server for one task, if it has tools.
//
// Returns the MCP config fragment to merge into the agent's, exactly like the
// Remote MCP broker, so the two arrive at the agent through one path.
func startTaskPluginHookMCP(lifetimeCtx context.Context, taskID string, tools []PluginHookTool, invoke pluginHookInvoker, logger *slog.Logger) (json.RawMessage, *pluginHookMCPSet, error) {
	if len(tools) == 0 || invoke == nil {
		return nil, nil, nil
	}

	byName := make(map[string]PluginHookTool, len(tools))
	for _, tool := range tools {
		// The server namespaces these, but a duplicate arriving anyway must
		// resolve to exactly one hook rather than whichever came last.
		if _, clash := byName[tool.Name]; clash {
			if logger != nil {
				logger.Warn("plugin hook tool name collided; ignoring the duplicate", "task_id", taskID, "tool", tool.Name)
			}
			continue
		}
		byName[tool.Name] = tool
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("listen for plugin hook MCP server: %w", err)
	}
	pathToken, err := randomBrokerToken()
	if err != nil {
		_ = listener.Close()
		return nil, nil, err
	}

	handler := &pluginHookMCPServer{
		taskID: taskID, tools: tools, byName: byName,
		invoke: invoke, path: "/" + pathToken, logger: logger,
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	set := &pluginHookMCPSet{server: server, listener: listener}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
			logger.Warn("plugin hook MCP server stopped unexpectedly", "task_id", taskID, "error", serveErr)
		}
	}()
	go func() {
		<-lifetimeCtx.Done()
		set.Close()
	}()

	raw, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"multica-plugins": map[string]any{
			"type": "http",
			"url":  "http://" + listener.Addr().String() + handler.path,
		},
	}})
	if err != nil {
		set.Close()
		return nil, nil, err
	}
	return raw, set, nil
}

type pluginHookMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *pluginHookMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The path is a per-task random token, so a process that merely knows the
	// port cannot reach the tools.
	if r.URL.Path != s.path || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, pluginHookMCPMaxRequestBytes))
	if err != nil {
		writePluginHookMCPError(w, nil, -32700, "could not read the request")
		return
	}
	var request pluginHookMCPRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writePluginHookMCPError(w, nil, -32700, "request is not valid JSON-RPC")
		return
	}

	switch request.Method {
	case "initialize":
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"protocolVersion": pluginHookMCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "multica-plugins", "version": "1"},
		})
	case "notifications/initialized":
		// A notification has no id and takes no reply.
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writePluginHookMCPResult(w, request.ID, map[string]any{"tools": s.toolDescriptors()})
	case "tools/call":
		s.handleCall(w, r, request)
	default:
		writePluginHookMCPError(w, request.ID, -32601, "unsupported method "+request.Method)
	}
}

func (s *pluginHookMCPServer) toolDescriptors() []map[string]any {
	descriptors := make([]map[string]any, 0, len(s.tools))
	for _, tool := range s.tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			// A tool with no declared input still needs a schema, or providers
			// reject the list outright.
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		descriptors = append(descriptors, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": schema,
		})
	}
	return descriptors
}

func (s *pluginHookMCPServer) handleCall(w http.ResponseWriter, r *http.Request, request pluginHookMCPRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		writePluginHookMCPError(w, request.ID, -32602, "invalid tool call parameters")
		return
	}
	tool, ok := s.byName[params.Name]
	if !ok {
		writePluginHookMCPError(w, request.ID, -32602, "unknown tool "+params.Name)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), pluginHookMCPCallTimeout)
	defer cancel()
	output, err := s.invoke(ctx, s.taskID, tool.InstallationID, tool.HookKey, params.Arguments)
	if err != nil {
		// A TOOL error, not a protocol error. The agent reads this, decides the
		// tool did not work, and carries on with the task — which is the whole
		// point: an unreachable plugin endpoint must not fail somebody's issue.
		if s.logger != nil {
			s.logger.Info("plugin hook tool call failed", "task_id", s.taskID, "tool", tool.Name, "error", err)
		}
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
		})
		return
	}

	text := string(output)
	if len(output) == 0 {
		text = "The hook completed and returned nothing."
	}
	writePluginHookMCPResult(w, request.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

func writePluginHookMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writePluginHookMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
