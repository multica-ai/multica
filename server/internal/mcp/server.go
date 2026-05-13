package mcp

// CEREBRO-PATCH(mcp-server): cerebro modification of upstream file

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

// ToolHandler handles a tool invocation.
type ToolHandler func(ctx context.Context, args map[string]any) (CallToolResult, error)

// Server is a Model Context Protocol server using stdio transport.
type Server struct {
	info        ServerInfo
	tools       []Tool
	handlers    map[string]ToolHandler
	mu          sync.Mutex
	binaryPath  string
	binaryMtime time.Time
}

// NewServer creates a new MCP server.
func NewServer(name, version string) *Server {
	s := &Server{
		info:     ServerInfo{Name: name, Version: version},
		handlers: make(map[string]ToolHandler),
	}
	// Record binary modtime for auto-reload.
	if exe, err := os.Executable(); err == nil {
		s.binaryPath = exe
		if info, err := os.Stat(exe); err == nil {
			s.binaryMtime = info.ModTime()
		}
	}
	return s
}

// RegisterTool adds a tool to the server.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.tools = append(s.tools, tool)
	s.handlers[tool.Name] = handler
}

// CEREBRO-PATCH(mcp-tools-inventory): JEH-1171 expose registered tools so the
// permguard regression test can enumerate them without speaking JSON-RPC.
// Returns a fresh slice so callers cannot mutate the server's internal list.
func (s *Server) Tools() []Tool {
	out := make([]Tool, len(s.tools))
	copy(out, s.tools)
	return out
}

// Call invokes a registered tool handler by name. Useful for tests that want
// to exercise the tool dispatch path without piping JSON-RPC over stdio.
// Returns the same CallToolResult the JSON-RPC `tools/call` path would produce.
func (s *Server) Call(ctx context.Context, name string, args map[string]any) (CallToolResult, error) {
	h, ok := s.handlers[name]
	if !ok {
		return ErrorResult(fmt.Sprintf("unknown tool: %s", name)), nil
	}
	return h(ctx, args)
}

// Run starts the stdio server loop. It blocks until stdin is closed or ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	// MCP messages can be large (e.g. tool results with full issue details).
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeResponse(Response{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		resp := s.handleRequest(ctx, req)
		s.writeResponse(resp)

		// After responding, check if binary has been updated.
		// exec replaces the process but inherits stdin/stdout — connection stays alive.
		s.maybeReload()
	}

	return scanner.Err()
}

func (s *Server) handleRequest(ctx context.Context, req Request) Response {
	switch req.Method {
	case "initialize":
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: InitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities:    Capabilities{Tools: &ToolsCapability{}},
				ServerInfo:      s.info,
			},
		}

	case "notifications/initialized":
		// Client acknowledgement, no response needed for notifications.
		return Response{}

	case "tools/list":
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  ToolsListResult{Tools: s.tools},
		}

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: -32602, Message: "invalid params"},
			}
		}

		handler, ok := s.handlers[params.Name]
		if !ok {
			return Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  ErrorResult(fmt.Sprintf("unknown tool: %s", params.Name)),
			}
		}

		result, err := handler(ctx, params.Arguments)
		if err != nil {
			return Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  ErrorResult(err.Error()),
			}
		}

		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}

	case "ping":
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

// maybeReload checks if the binary has been replaced and exec's into the new version.
// syscall.Exec replaces the current process image — stdin/stdout file descriptors are
// inherited, so the MCP stdio connection stays alive. The new process starts fresh
// (re-reads config, re-initializes tools) but the client sees no interruption.
func (s *Server) maybeReload() {
	if s.binaryPath == "" {
		return
	}
	info, err := os.Stat(s.binaryPath)
	if err != nil {
		return
	}
	if !info.ModTime().After(s.binaryMtime) {
		return
	}
	// Binary changed — exec into the new version.
	// Pass the same args and env so the new process behaves identically.
	fmt.Fprintf(os.Stderr, "multica mcp: binary updated, reloading...\n")
	syscall.Exec(s.binaryPath, os.Args, os.Environ())
	// If exec fails, we just continue with the old binary.
}

func (s *Server) writeResponse(resp Response) {
	// Don't write empty responses (notifications).
	if resp.JSONRPC == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		// Last resort: write a JSON-RPC error.
		_, _ = io.WriteString(os.Stdout, `{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal error"}}`+"\n")
		return
	}
	_, _ = os.Stdout.Write(data)
	_, _ = os.Stdout.Write([]byte{'\n'})
}
