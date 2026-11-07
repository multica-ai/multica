// Package contextmcp serves Multica's context tools to local Claude and
// Codex executions over MCP. One loopback HTTP server holds a route per
// running execution; each route has its own bearer token and tool set and
// ends with the execution.
package contextmcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Name is the MCP server name providers see for Multica's context tools.
const Name = "multica_desktop_context"

const (
	maxRequestBytes = 1 << 20
	maxImageBytes   = 4 << 20
	callTimeout     = time.Hour
	shutdownTimeout = 2 * time.Second
)

// Protocol versions this server speaks; it answers with the client's
// version when supported, else the newest.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Tool is one MCP tool.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	ReadOnly    bool
	// Call runs the tool. A returned error is reported to the model as a
	// tool error, never as a protocol error.
	Call func(ctx context.Context, args json.RawMessage) (Result, error)
}

// Result is a tool's output: Value is returned as JSON text, Image (if set)
// as an MCP image block.
type Result struct {
	Value any
	Image *Image
}

// Image is base64 image content.
type Image struct {
	MimeType string
	Data     string
}

// Execution is the tool set of one provider process.
type Execution struct {
	Instructions string
	Tools        []Tool
	// Token, when set, is the route's bearer token; providers that read it
	// from their environment need it before the route exists.
	Token string
	// TokenInPath puts the token in the route URL instead of an Authorization
	// header, for task MCP configs that every provider must accept as a bare
	// URL, like the daemon's other task-local MCP servers.
	TokenInPath bool
}

type route struct {
	token  string
	exec   Execution
	byName map[string]Tool
	ctx    context.Context
	cancel context.CancelFunc
	calls  sync.WaitGroup
}

// Server is the loopback MCP server shared by all executions.
type Server struct {
	listener net.Listener
	http     *http.Server
	host     string

	mu     sync.Mutex
	routes map[string]*route
}

// Start listens on 127.0.0.1.
func Start() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for context MCP: %w", err)
	}
	s := &Server{listener: ln, host: ln.Addr().String(), routes: map[string]*route{}}
	s.http = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.http.Serve(ln) }()
	return s, nil
}

// Binding is a bound execution's endpoint.
type Binding struct {
	URL   string
	Token string
	// Tools are the bound tool names, for provider allow lists.
	Tools []string
}

// Bind exposes exec until Release or until parent ends.
func (s *Server) Bind(parent context.Context, exec Execution) (Binding, error) {
	id, err := randomHex(16)
	if err != nil {
		return Binding{}, err
	}
	token := exec.Token
	if token == "" {
		if token, err = randomHex(32); err != nil {
			return Binding{}, err
		}
	}
	ctx, cancel := context.WithCancel(parent)
	r := &route{token: token, exec: exec, byName: map[string]Tool{}, ctx: ctx, cancel: cancel}
	names := make([]string, 0, len(exec.Tools))
	for _, t := range exec.Tools {
		if _, dup := r.byName[t.Name]; dup {
			cancel()
			return Binding{}, fmt.Errorf("duplicate context tool %s", t.Name)
		}
		r.byName[t.Name] = t
		names = append(names, t.Name)
	}
	s.mu.Lock()
	s.routes[id] = r
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if s.routes[id] == r {
			delete(s.routes, id)
		}
		s.mu.Unlock()
	}()
	url := "http://" + s.host + "/mcp/" + id
	if exec.TokenInPath {
		url += "/" + token
	}
	return Binding{URL: url, Token: token, Tools: names}, nil
}

// Release ends an execution's route and waits for its running tool calls.
func (s *Server) Release(b Binding) {
	id, _, _ := strings.Cut(strings.TrimPrefix(b.URL, "http://"+s.host+"/mcp/"), "/")
	s.mu.Lock()
	r := s.routes[id]
	delete(s.routes, id)
	s.mu.Unlock()
	if r != nil {
		r.cancel()
		r.calls.Wait()
	}
}

// Close stops the server and every route.
func (s *Server) Close() error {
	s.mu.Lock()
	routes := s.routes
	s.routes = map[string]*route{}
	s.mu.Unlock()
	for _, r := range routes {
		r.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.http.Shutdown(ctx)
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ServeHTTP admits only the route's bearer token on the loopback host, with
// no Origin, so neither a browser page nor another local process that only
// knows the port can call the tools.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, ok := strings.CutPrefix(r.URL.Path, "/mcp/")
	if !ok || r.Header.Get("Origin") != "" || r.Host != s.host {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	id, pathToken, inPath := strings.Cut(path, "/")
	s.mu.Lock()
	rt := s.routes[id]
	s.mu.Unlock()
	presented, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if rt != nil && rt.exec.TokenInPath {
		presented = pathToken
	} else if inPath {
		rt = nil
	}
	if rt == nil || subtle.ConstantTimeCompare([]byte(presented), []byte(rt.token)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || len(body) > maxRequestBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, nil, -32700, "Invalid MCP request")
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := protocolVersions[0]
		for _, v := range protocolVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		writeResult(w, req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": Name, "version": "1.0.0"},
			"instructions":    rt.exec.Instructions,
		})
	case "notifications/initialized", "notifications/cancelled":
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		writeResult(w, req.ID, map[string]any{})
	case "tools/list":
		tools := make([]map[string]any, 0, len(rt.exec.Tools))
		for _, t := range rt.exec.Tools {
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema,
				"annotations": map[string]any{"readOnlyHint": t.ReadOnly, "destructiveHint": false, "idempotentHint": t.ReadOnly, "openWorldHint": false}})
		}
		writeResult(w, req.ID, map[string]any{"tools": tools})
	case "tools/call":
		s.call(w, r, rt, req)
	default:
		writeError(w, req.ID, -32601, "unsupported method "+req.Method)
	}
}

func (s *Server) call(w http.ResponseWriter, r *http.Request, rt *route, req rpcRequest) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(w, req.ID, -32602, "invalid tool call parameters")
		return
	}
	tool, ok := rt.byName[p.Name]
	if !ok {
		writeError(w, req.ID, -32602, "unknown tool "+p.Name)
		return
	}
	if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
		p.Arguments = json.RawMessage(`{}`)
	}
	rt.calls.Add(1)
	defer rt.calls.Done()
	if rt.ctx.Err() != nil {
		writeResult(w, req.ID, toolError("This Multica execution has ended"))
		return
	}
	ctx, cancel := context.WithTimeout(rt.ctx, callTimeout)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()

	result, err := tool.Call(ctx, p.Arguments)
	switch {
	case rt.ctx.Err() != nil:
		writeResult(w, req.ID, toolError("This Multica execution has ended"))
	case err != nil:
		writeResult(w, req.ID, toolError(err.Error()))
	default:
		content, err := resultContent(result)
		if err != nil {
			writeResult(w, req.ID, toolError(err.Error()))
			return
		}
		writeResult(w, req.ID, map[string]any{"content": content})
	}
}

func resultContent(r Result) ([]map[string]any, error) {
	text, err := json.Marshal(r.Value)
	if err != nil {
		return nil, err
	}
	content := []map[string]any{{"type": "text", "text": string(text)}}
	if img := r.Image; img != nil {
		switch img.MimeType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return nil, errors.New("unsupported image type")
		}
		if len(img.Data)/4*3 > maxImageBytes {
			return nil, errors.New("MCP image exceeds 4 MB")
		}
		content = append(content, map[string]any{"type": "image", "mimeType": img.MimeType, "data": img.Data})
	}
	return content, nil
}

func toolError(message string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": message}}}
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// NewToken returns a random route token.
func NewToken() (string, error) { return randomHex(32) }

// ReadOnlyInstructions describes a route that only has the context tools.
const ReadOnlyInstructions = "This server only reads data. " + ContextInstructions
