package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/computer"
	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// ComputerCapability is what the connected Desktop reported for Computer Use.
// Desktop owns the SDK, its prompts and the REPL runtime; the daemon uses them
// as given.
type ComputerCapability struct {
	Version string
	// UserID is the account signed in to Desktop; empty when signed out.
	UserID  string
	Prompts ComputerPrompts
	Runtime computer.Command
}

// ComputerPrompts are Desktop's model-facing texts, used verbatim.
type ComputerPrompts struct {
	// Conversation joins a local conversation's system prompt.
	Conversation string
	// Task is the instructions of a task's Computer Use MCP server.
	Task string
	// TaskBrief is appended to a task's prompt.
	TaskBrief string
}

// ComputerHost is the executing device's Desktop main process. Ordinary
// renderer/remote host routing must never service desktop control.
type ComputerHost interface {
	// Computer returns the capability while Desktop is connected, initialized
	// and reported a compatible Computer capability.
	Computer() (ComputerCapability, bool)
	// Control sends one envelope; version is the capability version of the
	// REPL that produced it.
	Control(ctx context.Context, version string, envelope map[string]any) (json.RawMessage, error)
}

type computerUse struct {
	mu     sync.Mutex
	host   ComputerHost
	active map[string]*computerExecution
}
type computerExecution struct {
	turn    string
	scopeID int64
	payload map[string]any
	cancel  context.CancelFunc
}

// SetComputerHost binds the executing machine's authenticated main process.
func (s *Service) SetComputerHost(host ComputerHost) {
	s.computers.mu.Lock()
	s.computers.host = host
	s.computers.mu.Unlock()
}

// computerCapability returns the connected Desktop's Computer capability.
func (s *Service) computerCapability() (ComputerCapability, bool) {
	s.computers.mu.Lock()
	h := s.computers.host
	s.computers.mu.Unlock()
	if h == nil {
		return ComputerCapability{}, false
	}
	return h.Computer()
}

// StopComputer cancels JavaScript as well as native input. The turn guard
// prevents a late stop notification from interrupting a subsequent turn.
func (s *Service) StopComputer(conversationID, turnID string) {
	s.computers.mu.Lock()
	e := s.computers.active[conversationID]
	if e == nil || e.turn != turnID {
		s.computers.mu.Unlock()
		return
	}
	if e.cancel != nil {
		e.cancel()
	}
	s.computer.Reset(fmt.Sprintf("%d/%s", e.scopeID, conversationID))
	delete(s.computers.active, conversationID)
	h := s.computers.host
	s.computers.mu.Unlock()
	releaseComputer(e, h)
}

func (s *Service) StopComputerHost() {
	s.computers.mu.Lock()
	turns := map[string]string{}
	for id, e := range s.computers.active {
		turns[id] = e.turn
	}
	s.computers.mu.Unlock()
	for id, turn := range turns {
		s.StopComputer(id, turn)
	}
}

var errDesktopUnavailable = errors.New("desktop_unavailable: Multica Desktop is not connected to this device's runtime")

// computerHost sends envelope for a REPL of version, or for the current
// capability when version is empty.
func (s *Service) computerHost(ctx context.Context, version string, payload map[string]any) (json.RawMessage, error) {
	s.computers.mu.Lock()
	h := s.computers.host
	s.computers.mu.Unlock()
	if h == nil {
		return nil, errDesktopUnavailable
	}
	if version == "" {
		capability, ok := h.Computer()
		if !ok {
			return nil, errDesktopUnavailable
		}
		version = capability.Version
	}
	return h.Control(ctx, version, payload)
}

func (s *Service) endComputer(conversationID string) <-chan error {
	s.computers.mu.Lock()
	e := s.computers.active[conversationID]
	delete(s.computers.active, conversationID)
	h := s.computers.host
	if e != nil && e.cancel != nil {
		e.cancel()
	}
	s.computers.mu.Unlock()
	return releaseComputer(e, h)
}

func releaseComputer(e *computerExecution, h ComputerHost) <-chan error {
	done := make(chan error, 1)
	if e == nil || h == nil {
		done <- nil
		return done
	}
	go func() {
		capability, ok := h.Computer()
		if !ok {
			// A disconnected Desktop already dropped every lease.
			done <- nil
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		p := map[string]any{}
		for k, v := range e.payload {
			p[k] = v
		}
		p["op"] = "release"
		_, err := h.Control(ctx, capability.Version, p)
		done <- err
	}()
	return done
}

func (s *Service) computerToolsFor(conv sqlitedb.Conversation, current func() map[string]any) []contextmcp.Tool {
	return s.computerTools(conv, current, func(ctx context.Context) (map[string]any, error) {
		scope, err := s.store.ScopeByID(ctx, conv.ScopeID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"scope": map[string]string{"userId": scope.UserID, "workspaceId": scope.WorkspaceID}}, nil
	})
}

// TaskComputer is Computer Use bound to one Multica agent task run.
type TaskComputer struct {
	// URL is the task's MCP endpoint; its path carries the route token.
	URL string
	// Brief is Desktop's text for the end of the task prompt.
	Brief string
}

// BindTaskComputer exposes Computer Use to one Multica agent task run on this
// device. ok is false unless Desktop is connected with a compatible Computer
// capability and signed in as userID, the account this daemon runs tasks
// for; userID is empty when that account is unknown. Desktop approves task
// apps without a dialog and keeps the device lease, stop menu and blocked-app
// rules of a local conversation. release ends the route, the REPL and any
// desktop control the run still holds.
func (s *Service) BindTaskComputer(taskID, workspaceID, userID string) (task TaskComputer, release func(), ok bool, err error) {
	capability, connected := s.computerCapability()
	if !connected || taskID == "" || workspaceID == "" || userID == "" || capability.UserID != userID {
		return TaskComputer{}, nil, false, nil
	}
	conv := sqlitedb.Conversation{ID: "task:" + taskID}
	current := func() map[string]any { return map[string]any{"turn_id": taskID} }
	tools := s.computerTools(conv, current, func(context.Context) (map[string]any, error) {
		return map[string]any{"origin": "task", "scope": map[string]string{"userId": userID, "workspaceId": workspaceID}}, nil
	})
	binding, err := s.context.Bind(context.Background(), contextmcp.Execution{Instructions: capability.Prompts.Task, Tools: tools, TokenInPath: true})
	if err != nil {
		return TaskComputer{}, nil, false, err
	}
	return TaskComputer{URL: binding.URL, Brief: capability.Prompts.TaskBrief}, func() {
		<-s.endComputer(conv.ID)
		s.computer.Reset(fmt.Sprintf("%d/%s", conv.ScopeID, conv.ID))
		s.context.Release(binding)
	}, true, nil
}

// computerTools builds the Computer Use tools for one controller. identity
// supplies the envelope fields that tell Desktop whose control this is.
func (s *Service) computerTools(conv sqlitedb.Conversation, current func() map[string]any, identity func(context.Context) (map[string]any, error)) []contextmcp.Tool {
	t := &sideTools{s: s, conv: conv, current: current, writes: map[string]*sideWrite{}}
	var out []contextmcp.Tool
	payload := func(ctx context.Context, op string) (map[string]any, error) {
		turn := t.turnID()
		if turn == "" {
			return nil, errors.New("Computer Use requires an active turn")
		}
		fields, err := identity(ctx)
		if err != nil {
			return nil, err
		}
		p := map[string]any{"op": op, "conversationId": conv.ID, "turnId": turn}
		for k, v := range fields {
			p[k] = v
		}
		return p, nil
	}
	key := fmt.Sprintf("%d/%s", conv.ScopeID, conv.ID)
	t.tool(&out, "computer_status", "Check this execution device's Computer Use runtime and macOS permissions. Does not prompt for permissions or read screens.", obj(``), false, func(ctx context.Context, _ map[string]any) (contextmcp.Result, error) {
		if _, ok := s.computerCapability(); !ok {
			return contextmcp.Result{Value: map[string]any{"available": false, "reason": "desktop_unavailable"}}, nil
		}
		p, err := payload(ctx, "status")
		if err != nil {
			return contextmcp.Result{}, err
		}
		raw, err := s.computerHost(ctx, "", p)
		var value any
		_ = json.Unmarshal(raw, &value)
		return contextmcp.Result{Value: value}, err
	})
	t.tool(&out, "computer_execute", "Execute JavaScript with the persistent Computer SDK. See Computer Use instructions. Desktop approves apps; arguments cannot supply approvals. Errors may follow partial actions. Only the latest observed image is returned. Use a stable request_id for identical retries.", obj(`"code":{"type":"string","minLength":1,"maxLength":32000},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120},"request_id":`+sideReqSchema, "code", "request_id"), true, func(ctx context.Context, in map[string]any) (contextmcp.Result, error) {
		capability, ok := s.computerCapability()
		if !ok {
			return contextmcp.Result{}, errDesktopUnavailable
		}
		p, err := payload(ctx, "begin")
		if err != nil {
			return contextmcp.Result{}, err
		}
		seconds := 60
		if n, ok := in["timeout_seconds"].(float64); ok {
			seconds = int(n)
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
		e := &computerExecution{turn: t.turnID(), scopeID: conv.ScopeID, payload: p, cancel: cancel}
		s.computers.mu.Lock()
		if previous := s.computers.active[conv.ID]; previous != nil && previous.cancel != nil {
			s.computers.mu.Unlock()
			return contextmcp.Result{}, errors.New("device_busy: This conversation already has a Computer Use execution")
		}
		s.computers.active[conv.ID] = e
		s.computers.mu.Unlock()
		defer func() {
			s.computers.mu.Lock()
			if s.computers.active[conv.ID] == e {
				e.cancel = nil
			}
			s.computers.mu.Unlock()
		}()
		if _, err := s.computerHost(ctx, capability.Version, p); err != nil {
			return contextmcp.Result{}, err
		}
		runtime := computer.Runtime{Command: capability.Runtime, Version: capability.Version}
		result, restarted, runErr := s.computer.Execute(ctx, key, runtime, t.turnID()+":"+in["request_id"].(string), in["code"].(string), func(ctx context.Context, action json.RawMessage) (json.RawMessage, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			req := map[string]any{}
			for k, v := range p {
				req[k] = v
			}
			req["op"] = "action"
			req["action"] = action
			return s.computerHost(ctx, runtime.Version, req)
		})
		if runErr != nil && strings.HasPrefix(runErr.Error(), "runtime_outdated") {
			// Desktop moved to an incompatible capability mid-execution; the
			// next execution starts a REPL for it.
			s.computer.Reset(key)
		}
		value := map[string]any{"status": "completed", "output": result.Output, "value": result.Value, "actions": result.Actions, "trace": result.Trace}
		if restarted {
			value["runtime"] = map[string]any{"restarted": true, "reason": "capability_updated", "note": "Desktop's Computer capability changed; REPL variables were cleared."}
		}
		if runErr != nil {
			value["status"] = "failed"
			value["error"] = runErr.Error()
		}
		reply := contextmcp.Result{Value: value}
		if result.Image != nil {
			reply.Image = &contextmcp.Image{MimeType: result.Image.MimeType, Data: result.Image.Data}
		}
		return reply, nil
	})
	t.tool(&out, "computer_reset", "Discard this conversation's JavaScript variables and release its desktop control. Does not undo completed actions.", obj(`"request_id":`+sideReqSchema, "request_id"), true, func(ctx context.Context, _ map[string]any) (contextmcp.Result, error) {
		released := s.endComputer(conv.ID)
		s.computer.Reset(key)
		select {
		case err := <-released:
			if err != nil {
				return contextmcp.Result{}, fmt.Errorf("outcome_unknown: REPL reset, but desktop release was not confirmed: %w", err)
			}
		case <-ctx.Done():
			return contextmcp.Result{}, ctx.Err()
		}
		return contextmcp.Result{Value: map[string]any{"status": "reset"}}, nil
	})
	return out
}
