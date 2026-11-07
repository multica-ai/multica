package localhost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/localconversation/computer"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
)

// ComputerHostProtocolVersion is the JSON-RPC channel version between Desktop
// and this daemon, compared with computer.Compatible. The protocol is
// specified in docs/engineering/computer-host-protocol.md.
const ComputerHostProtocolVersion = "1.0.0"

const maxComputerPromptBytes = 16 << 10

// DaemonIdentity describes this daemon in the initialize response. UserID is
// the account the daemon runs tasks for, empty while unknown.
type DaemonIdentity struct {
	Version string
	UserID  string
}

// computerHost is device-local and main-only. It never follows the viewer
// connection, forwards to another device, or replays requests on reconnect.
type computerHost struct {
	mu         sync.Mutex
	connection *computerConnection
	identity   func() DaemonIdentity
	svc        *service.Service
}

type computerState int

const (
	computerConnected computerState = iota
	computerInitialized
	computerReady
)

type computerConnection struct {
	ws      *websocket.Conn
	write   sync.Mutex
	mu      sync.Mutex
	pending map[string]chan v2HostReply
	closed  bool
	state   computerState
	// capability is the reported Computer capability when it is compatible
	// and valid; nil otherwise.
	capability *service.ComputerCapability
}

func (c *computerConnection) send(value any) error {
	c.write.Lock()
	defer c.write.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.ws.WriteJSON(value)
}

func (c *computerConnection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	_ = c.ws.Close()
	for _, ch := range c.pending {
		select {
		case ch <- v2HostReply{err: errors.New("outcome_unknown: Computer Use host disconnected; inspect the app before retrying")}:
		default:
		}
	}
}

func (h *computerHost) current() *computerConnection {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connection
}

// Computer implements service.ComputerHost.
func (h *computerHost) Computer() (service.ComputerCapability, bool) {
	c := h.current()
	if c == nil {
		return service.ComputerCapability{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.state != computerReady || c.capability == nil {
		return service.ComputerCapability{}, false
	}
	return *c.capability, true
}

// Control implements service.ComputerHost.
func (h *computerHost) Control(ctx context.Context, version string, envelope map[string]any) (json.RawMessage, error) {
	c := h.current()
	if c == nil {
		return nil, service.ErrHostUnavailable
	}
	id := newRequestID()
	ch := make(chan v2HostReply, 1)
	c.mu.Lock()
	if c.closed || c.state != computerReady {
		c.mu.Unlock()
		return nil, service.ErrHostUnavailable
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	request := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(`"` + id + `"`), Method: "computer/control"}
	request.Params, _ = json.Marshal(map[string]any{"capabilityVersion": version, "envelope": envelope})
	if err := c.send(request); err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		return reply.result, reply.err
	case <-ctx.Done():
		cancelled := rpcMessage{JSONRPC: "2.0", Method: "notifications/cancelled"}
		cancelled.Params, _ = json.Marshal(map[string]string{"requestId": id})
		_ = c.send(cancelled)
		return nil, ctx.Err()
	}
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// clientState is the part of initialize that notifications/client/updated
// replaces as a whole.
type clientState struct {
	User *struct {
		ID *string `json:"id"`
	} `json:"user"`
	Capabilities struct {
		Computer *computerParams `json:"computer"`
	} `json:"capabilities"`
}

type computerParams struct {
	Version string `json:"version"`
	Prompts struct {
		Conversation string `json:"conversation"`
		Task         string `json:"task"`
		TaskBrief    string `json:"taskBrief"`
	} `json:"prompts"`
	Runtime struct {
		Executable string `json:"executable"`
		Entry      string `json:"entry"`
	} `json:"runtime"`
}

// evaluate returns the usable Computer capability, if any, and its status for
// the initialize response.
func (s clientState) evaluate() (*service.ComputerCapability, map[string]any) {
	p := s.Capabilities.Computer
	if p == nil {
		return nil, nil
	}
	status := map[string]any{"version": computer.CapabilityVersion}
	if !computer.Compatible(p.Version, computer.CapabilityVersion) {
		status["status"] = "incompatible"
		status["supported"] = supportedRange(computer.CapabilityVersion)
		return nil, status
	}
	if reason := p.invalid(); reason != "" {
		status["status"] = "invalid"
		status["reason"] = reason
		return nil, status
	}
	status["status"] = "enabled"
	capability := &service.ComputerCapability{
		Version: p.Version,
		Prompts: service.ComputerPrompts{Conversation: p.Prompts.Conversation, Task: p.Prompts.Task, TaskBrief: p.Prompts.TaskBrief},
		Runtime: computer.Command{Executable: p.Runtime.Executable, Entry: p.Runtime.Entry},
	}
	if s.User != nil && s.User.ID != nil {
		capability.UserID = *s.User.ID
	}
	return capability, status
}

func (p *computerParams) invalid() string {
	for _, prompt := range []string{p.Prompts.Conversation, p.Prompts.Task, p.Prompts.TaskBrief} {
		if prompt == "" || len(prompt) > maxComputerPromptBytes || !utf8.ValidString(prompt) {
			return "prompts must be non-empty UTF-8 of at most 16 KiB"
		}
	}
	for _, path := range []string{p.Runtime.Executable, p.Runtime.Entry} {
		if !filepath.IsAbs(path) {
			return "runtime paths must be absolute"
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return "runtime paths must name existing files"
		}
	}
	return ""
}

// supportedRange renders "1.0.x" for version "1.0.3".
func supportedRange(version string) string {
	if i := strings.LastIndex(version, "."); i > 0 {
		return version[:i] + ".x"
	}
	return version
}

func (v *V2) serveComputerHost(w http.ResponseWriter, r *http.Request) {
	if callerOf(r.Context()).kind != callerMain {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	v.computer.mu.Lock()
	if v.computer.connection != nil {
		v.computer.mu.Unlock()
		http.Error(w, "Computer Use host already connected", http.StatusConflict)
		return
	}
	ws, err := v.upgrader.Upgrade(w, r, nil)
	if err != nil {
		v.computer.mu.Unlock()
		return
	}
	c := &computerConnection{ws: ws, pending: map[string]chan v2HostReply{}}
	v.computer.connection = c
	v.computer.mu.Unlock()
	defer func() {
		c.close()
		v.svc.StopComputerHost()
		v.computer.mu.Lock()
		if v.computer.connection == c {
			v.computer.connection = nil
		}
		v.computer.mu.Unlock()
	}()
	ws.SetReadLimit(1 << 20)
	for {
		var msg rpcMessage
		if ws.ReadJSON(&msg) != nil {
			return
		}
		v.computer.handle(c, msg)
	}
}

func (h *computerHost) handle(c *computerConnection, msg rpcMessage) {
	isRequest := len(msg.ID) > 0 && string(msg.ID) != "null"
	reply := func(result any, failure *rpcError) {
		if !isRequest {
			return
		}
		out := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
		if failure != nil {
			out.Error = failure
		} else {
			out.Result, _ = json.Marshal(result)
		}
		_ = c.send(out)
	}
	if msg.JSONRPC != "2.0" {
		reply(nil, &rpcError{Code: -32600, Message: "invalid_request"})
		return
	}
	if msg.Method == "" {
		h.resolve(c, msg)
		return
	}
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	switch msg.Method {
	case "initialize":
		if state != computerConnected {
			reply(nil, &rpcError{Code: -32600, Message: "already_initialized"})
			return
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
			clientState
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			reply(nil, &rpcError{Code: -32602, Message: "invalid_params"})
			return
		}
		if !computer.Compatible(params.ProtocolVersion, ComputerHostProtocolVersion) {
			reply(nil, &rpcError{Code: -32602, Message: "protocol_incompatible", Data: map[string]string{"supported": supportedRange(ComputerHostProtocolVersion)}})
			return
		}
		capability, status := params.clientState.evaluate()
		c.mu.Lock()
		c.state, c.capability = computerInitialized, capability
		c.mu.Unlock()
		identity := DaemonIdentity{}
		if h.identity != nil {
			identity = h.identity()
		}
		var user any
		if identity.UserID != "" {
			user = identity.UserID
		}
		capabilities := map[string]any{}
		if status != nil {
			capabilities["computer"] = status
		}
		reply(map[string]any{
			"protocolVersion": ComputerHostProtocolVersion,
			"serverInfo":      map[string]any{"name": "multica-daemon", "version": identity.Version},
			"user":            map[string]any{"id": user},
			"capabilities":    capabilities,
		}, nil)
	case "notifications/initialized":
		c.mu.Lock()
		if c.state == computerInitialized {
			c.state = computerReady
		}
		c.mu.Unlock()
	case "notifications/client/updated":
		if state != computerReady {
			return
		}
		var params clientState
		if json.Unmarshal(msg.Params, &params) != nil {
			return
		}
		capability, _ := params.evaluate()
		c.mu.Lock()
		changed := !sameCapability(c.capability, capability)
		c.capability = capability
		c.mu.Unlock()
		// A different account or capability ends control granted under the
		// previous one; Desktop's broker revokes its side independently.
		if changed && h.svc != nil {
			h.svc.StopComputerHost()
		}
	case "notifications/computer/stopped":
		if state != computerReady {
			return
		}
		var params struct {
			ConversationID string `json:"conversationId"`
			TurnID         string `json:"turnId"`
		}
		if json.Unmarshal(msg.Params, &params) == nil && h.svc != nil {
			h.svc.StopComputer(params.ConversationID, params.TurnID)
		}
	default:
		if state != computerReady {
			reply(nil, &rpcError{Code: -32002, Message: "not_initialized"})
			return
		}
		reply(nil, &rpcError{Code: -32601, Message: "method_not_found"})
	}
}

// resolve delivers a response to its pending computer/control request.
func (h *computerHost) resolve(c *computerConnection, msg rpcMessage) {
	var id string
	if json.Unmarshal(msg.ID, &id) != nil {
		return
	}
	c.mu.Lock()
	ch := c.pending[id]
	c.mu.Unlock()
	if ch == nil {
		return
	}
	reply := v2HostReply{result: msg.Result}
	if msg.Error != nil {
		reply.err = errors.New(msg.Error.Message)
	}
	select {
	case ch <- reply:
	default:
	}
}

func sameCapability(a, b *service.ComputerCapability) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
