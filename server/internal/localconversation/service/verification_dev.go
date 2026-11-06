//go:build verificationdev

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

type verificationAdapter struct {
	mu        sync.Mutex
	handler   claude.Handler
	done      chan struct{}
	closed    bool
	pending   bool
	requestID string
	received  []string
}

var verificationAdapters sync.Map

func VerificationEnabled() bool {
	return os.Getenv("MULTICA_VERIFICATION_MODE") == "1" && len(os.Getenv("MULTICA_VERIFICATION_TOKEN")) == 64
}
func (s *Service) verificationMode() bool { return VerificationEnabled() }
func (s *Service) verificationClose() {
	prefix := s.Generation() + ":"
	verificationAdapters.Range(func(key, value any) bool {
		if strings.HasPrefix(key.(string), prefix) {
			_ = value.(*verificationAdapter).Close(context.Background())
			verificationAdapters.Delete(key)
		}
		return true
	})
}

func (s *Service) verificationRunner(ctx context.Context, conv sqlitedb.Conversation) (runner, bool, error) {
	if !VerificationEnabled() {
		return nil, false, nil
	}
	if conv.Provider != "claude" && conv.Provider != "codex" {
		return nil, true, ErrUnsupportedProvider
	}
	candidate := NewClaudeRunner(ClaudeRunnerOptions{
		ConversationID: conv.ID, ScopeID: conv.ScopeID, Files: s.store,
		Session: ClaudeSession{Fields: map[string]any{}},
		Start: func(_ context.Context, handler claude.Handler) (ClaudeAdapter, error) {
			adapter := &verificationAdapter{handler: handler, done: make(chan struct{})}
			if previous, ok := verificationAdapters.Load(s.Generation() + ":" + conv.ID); ok {
				old := previous.(*verificationAdapter)
				old.mu.Lock()
				adapter.received = append([]string{}, old.received...)
				old.mu.Unlock()
			}
			verificationAdapters.Store(s.Generation()+":"+conv.ID, adapter)
			return adapter, nil
		},
		Batcher: s.batcher, Limiter: s.limiter, Now: s.opts.Now,
		OnTurnEnd: func(turnID string) { s.turnEnded(conv.ID, turnID) },
	})
	kept, err := s.keepRunner(ctx, conv, candidate)
	return kept, true, err
}
func (a *verificationAdapter) Request(_ context.Context, typ string, fields map[string]any) (json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("verification provider closed")
	}
	if typ == "send" {
		if a.pending {
			return nil, errors.New("verification provider is busy")
		}
		a.pending = true
		a.requestID, _ = fields["request_id"].(string)
		a.received = append(a.received, a.requestID)
	}
	if typ == "interrupt" {
		a.pending = false
		// Let the runner finish cancellation and release the stopped turn's execution slot.
		return json.RawMessage(`{"outcome":"closed"}`), nil
	}
	return json.RawMessage(`{}`), nil
}
func (a *verificationAdapter) Close(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.closed {
		a.closed = true
		close(a.done)
	}
	return nil
}
func (a *verificationAdapter) Done() <-chan struct{} { return a.done }
func (a *verificationAdapter) Err() error            { return nil }

// VerificationFixture controls only the in-memory fake adapter of one scoped conversation.
func (s *Service) VerificationFixture(ctx context.Context, scopeID int64, id, action, text, expectedRequestID string) (map[string]any, error) {
	if !VerificationEnabled() {
		return nil, ErrUnsupportedProvider
	}
	if _, err := s.scopedConversation(ctx, scopeID, id); err != nil {
		return nil, err
	}
	value, ok := verificationAdapters.Load(s.Generation() + ":" + id)
	if !ok {
		if action == "inspect" {
			return map[string]any{"received": []string{}, "pending": false, "pendingRequestId": "", "generation": s.Generation()}, nil
		}
		return nil, errors.New("verification provider has not received a request")
	}
	a := value.(*verificationAdapter)
	a.mu.Lock()
	if action == "inspect" {
		result := map[string]any{"received": append([]string{}, a.received...), "pending": a.pending, "pendingRequestId": a.requestID, "generation": s.Generation()}
		a.mu.Unlock()
		return result, nil
	}
	if expectedRequestID == "" || expectedRequestID != a.requestID {
		a.mu.Unlock()
		return nil, ErrRequestConflict
	}
	if !a.pending {
		a.mu.Unlock()
		return nil, errors.New("verification provider has no pending reply")
	}
	if action != "release" && action != "fail" && action != "chunk" {
		a.mu.Unlock()
		return nil, ErrInvalidRequest
	}
	if action != "chunk" {
		a.pending = false
	}
	handler := a.handler
	requestID := a.requestID
	a.mu.Unlock()
	if text != "" {
		message, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"id": "verification-" + requestID, "role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
		handler(claude.Output{Type: "sdk_message", Message: message})
	}
	if action != "chunk" {
		message, _ := json.Marshal(map[string]any{"type": "result", "subtype": map[bool]string{true: "error_during_execution", false: "success"}[action == "fail"], "is_error": action == "fail", "result": text})
		handler(claude.Output{Type: "sdk_message", Message: message})
	}
	return map[string]any{"action": action, "at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func verificationOptions(opts Options) Options {
	if VerificationEnabled() {
		opts.Catalog = nil
		opts.CodexSpare = nil
		opts.ClaudeSpare = nil
		opts.PreheatOldConversations = func() bool { return false }
	}
	return opts
}
