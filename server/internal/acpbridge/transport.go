package acpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Bridge struct {
	api       API
	workspace string
	writeMu   sync.Mutex
	writer    *json.Encoder
	mu        sync.Mutex
	loadMu    sync.Mutex
	sessions  map[string]*session
	poll      time.Duration
	pollers   sync.WaitGroup
}

func New(api API, workspace string) *Bridge {
	return &Bridge{api: api, workspace: workspace, sessions: map[string]*session{}, poll: 500 * time.Millisecond}
}

func (b *Bridge) write(v any) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.writer.Encode(v)
}
func (b *Bridge) update(id string, update any) {
	_ = b.write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": update}})
}
func (b *Bridge) text(id, text string) {
	b.update(id, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text + "\n"}})
}

// Serve speaks newline-delimited JSON-RPC. EOF only detaches clients; it does
// not cancel remote work. Bound both frames and concurrent requests.
func (b *Bridge) Serve(parent context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	b.writer = json.NewEncoder(out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		b.mu.Lock()
		for _, s := range b.sessions {
			s.cancel()
		}
		b.mu.Unlock()
		wg.Wait()
		b.pollers.Wait()
	}()
	slots := make(chan struct{}, 16)
	for scanner.Scan() {
		var req request
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
			_ = b.write(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32600, "message": "invalid JSON-RPC request"}})
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			if len(req.ID) > 0 {
				b.reply(req.ID, nil, errors.New("too many outstanding requests"))
			}
			continue
		}
		wg.Add(1)
		go func(req request) {
			defer wg.Done()
			defer func() { <-slots }()
			result, err := b.handle(ctx, req)
			if len(req.ID) > 0 {
				b.reply(req.ID, result, err)
			}
		}(req)
	}
	return scanner.Err()
}

func (b *Bridge) reply(id json.RawMessage, result any, err error) {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		msg["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		msg["result"] = result
	}
	_ = b.write(msg)
}

func (b *Bridge) handle(ctx context.Context, req request) (any, error) {
	var p struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
		Cursor    string `json:"cursor"`
		Prompt    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"prompt"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
		return nil, errors.New("invalid parameters")
	}
	switch req.Method {
	case "initialize":
		return map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "multica-acp", "version": "0.1.0"}, "authMethods": []any{}, "agentCapabilities": map[string]any{"loadSession": true, "sessionCapabilities": map[string]any{"list": map[string]any{}}}}, nil
	case "session/list":
		list, err := b.discover(ctx, p.Cwd)
		if err != nil {
			return nil, err
		}
		start := 0
		if p.Cursor != "" {
			start, err = strconv.Atoi(p.Cursor)
			if err != nil || start < 0 || start > len(list) {
				return nil, errors.New("invalid cursor")
			}
		}
		end := min(start+50, len(list))
		result := map[string]any{"sessions": list[start:end]}
		if end < len(list) {
			result["nextCursor"] = strconv.Itoa(end)
		}
		return result, nil
	case "session/load":
		return b.load(ctx, p.SessionID)
	case "session/new":
		return nil, errors.New("Multica ACP joins existing issue conversations. Select one from agent session history; create/assign issues in Multica")
	case "session/prompt", "session/cancel":
		b.mu.Lock()
		s := b.sessions[p.SessionID]
		b.mu.Unlock()
		if s == nil {
			return nil, errors.New("load this issue conversation first")
		}
		if req.Method == "session/cancel" {
			call, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			_, err := b.control(call, s, "interrupt", "")
			if err != nil {
				b.text(s.id, "Interrupt could not be confirmed: "+err.Error())
			}
			return nil, err
		}
		text := ""
		for _, part := range p.Prompt {
			if part.Type != "text" {
				return nil, errors.New("this bridge currently accepts text only; paste relevant context as text")
			}
			text += part.Text
		}
		return b.prompt(ctx, s, text)
	default:
		return nil, fmt.Errorf("unsupported ACP method: %s", req.Method)
	}
}
