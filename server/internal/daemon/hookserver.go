package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// hookServer is a localhost-only HTTP listener that receives Claude Code
// hook deliveries and routes them to per-task subscriber channels. The
// daemon runs exactly one instance, shared across all in-flight tasks.
//
// Subscribers are keyed by an opaque token the backend chooses (task UUID
// today). Inbound POSTs include the token as a `task` query parameter so
// the server can dispatch each event to the right channel without
// inspecting the payload's cwd.
type hookServer struct {
	logger *slog.Logger
	addr   string // 127.0.0.1:<port>, resolved at Start

	mu   sync.Mutex
	subs map[string]chan agent.HookEvent

	listener net.Listener
	srv      *http.Server
}

// hookSubscriberFor returns the daemon's HookSubscriber when provider is a
// backend that consumes Claude Code hooks, and nil otherwise. Wiring nil
// into agent.Config keeps non-TUI backends unaware of the hook plumbing.
func (d *Daemon) hookSubscriberFor(provider string) agent.HookSubscriber {
	if d.hooks == nil || provider != "claude-tui" {
		return nil
	}
	return d.hooks
}

// newHookServer constructs an unstarted hook server. Call Start before use.
func newHookServer(logger *slog.Logger) *hookServer {
	return &hookServer{
		logger: logger,
		subs:   map[string]chan agent.HookEvent{},
	}
}

// Start binds 127.0.0.1:0 (OS-allocated port), records the listening
// address, and serves on a background goroutine. Returns the resolved
// address. The server runs until ctx is cancelled.
func (s *hookServer) Start(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("hook server listen: %w", err)
	}
	s.listener = ln
	s.addr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/hook", s.handleHook)
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("hook server failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info("hook server listening", "addr", s.addr)
	return s.addr, nil
}

// BaseURL returns the URL prefix backends embed in Claude's settings.local.json
// to route hook deliveries here. Implements agent.HookSubscriber.
func (s *hookServer) BaseURL() string {
	return "http://" + s.addr + "/hook"
}

// Subscribe registers a channel for the given task token. The returned cancel
// must be called exactly once when the task completes; it closes the channel
// and removes the entry so a slow hook arriving after Execute returned does
// not panic on send.
func (s *hookServer) Subscribe(token string) (<-chan agent.HookEvent, func()) {
	ch := make(chan agent.HookEvent, 32)
	s.mu.Lock()
	s.subs[token] = ch
	s.mu.Unlock()

	cancelled := false
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if cancelled {
			return
		}
		cancelled = true
		delete(s.subs, token)
		close(ch)
	}
	return ch, cancel
}

// handleHook is the single POST endpoint. It expects ?task=<token> and a JSON
// body matching Claude Code's hook payload shape. Unknown tokens are 404'd;
// well-formed events are pushed onto the subscriber channel with a 500ms
// best-effort send (a stuck subscriber drops, not blocks, the server).
func (s *hookServer) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("task")
	if token == "" {
		http.Error(w, "missing task token", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var raw struct {
		HookEventName     string          `json:"hook_event_name"`
		SessionID         string          `json:"session_id"`
		Cwd               string          `json:"cwd"`
		TranscriptPath    string          `json:"transcript_path"`
		ToolName          string          `json:"tool_name"`
		ToolUseID         string          `json:"tool_use_id"`
		ToolInput         json.RawMessage `json:"tool_input"`
		ToolResponse      json.RawMessage `json:"tool_response"`
		LastAssistantText string          `json:"last_assistant_message"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "parse body: "+err.Error(), http.StatusBadRequest)
		return
	}

	event := agent.HookEvent{
		Type:              agent.HookEventType(raw.HookEventName),
		SessionID:         raw.SessionID,
		Cwd:               raw.Cwd,
		TranscriptPath:    raw.TranscriptPath,
		ToolName:          raw.ToolName,
		ToolUseID:         raw.ToolUseID,
		ToolInput:         raw.ToolInput,
		ToolResponse:      raw.ToolResponse,
		LastAssistantText: raw.LastAssistantText,
		Raw:               body,
	}

	// Hold the lock across the send so cancel() cannot close ch between the
	// lookup and the channel op (which would panic with "send on closed
	// channel"). Cancel takes the same mutex before close, so any sender
	// that's already past the lookup completes its non-blocking send first;
	// any sender that arrives after cancel finds the entry gone.
	//
	// Non-blocking send (default branch) preserves the prior "drop if the
	// subscriber is stuck" semantics without holding the lock for hundreds
	// of milliseconds.
	s.mu.Lock()
	ch, ok := s.subs[token]
	if !ok {
		s.mu.Unlock()
		// Subscriber gone — common during graceful shutdown after Stop hook
		// fired. Don't 404 (claude treats non-2xx as hook failure and may
		// retry); accept silently.
		w.WriteHeader(http.StatusOK)
		return
	}
	select {
	case ch <- event:
	default:
		s.logger.Warn("hook event dropped: subscriber full", "token", token, "event", raw.HookEventName)
	}
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}
