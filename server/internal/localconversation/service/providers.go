package service

import (
	"context"

	"github.com/multica-ai/multica/server/internal/localconversation/catalog"
)

// Providers lists the supported providers and whether each is installed.
func (s *Service) Providers(ctx context.Context) []catalog.Provider {
	if s.verificationMode() {
		return []catalog.Provider{{ID: "claude", Name: "Verification (Claude protocol)", Available: true}, {ID: "codex", Name: "Verification (Codex identity)", Available: true}}
	}
	if s.opts.Catalog == nil {
		return []catalog.Provider{{ID: "codex", Name: "Codex"}, {ID: "claude", Name: "Claude Code"}}
	}
	return s.opts.Catalog.Providers(ctx)
}

// Models lists a provider's models.
func (s *Service) Models(ctx context.Context, provider string, force bool) (catalog.Models, error) {
	if s.verificationMode() {
		return catalog.Models{Supported: true, Models: []catalog.Model{{ID: "verification-fake", Label: "Verification fake provider", IsDefault: true}}}, nil
	}
	if s.opts.Catalog == nil {
		return catalog.Models{}, ErrUnsupportedProvider
	}
	return s.opts.Catalog.Models(ctx, provider, force)
}

// Usage reads a provider's account usage and, for a conversation of that
// provider, its context use.
func (s *Service) Usage(ctx context.Context, scopeID int64, provider, conversationID string, force bool) (catalog.Usage, error) {
	if s.verificationMode() {
		return catalog.Usage{}, ErrUnsupportedProvider
	}
	if s.opts.Catalog == nil {
		return catalog.Usage{}, ErrUnsupportedProvider
	}
	var session *catalog.Session
	if conversationID != "" {
		conv, err := s.scopedConversation(ctx, scopeID, conversationID)
		if err != nil {
			return catalog.Usage{}, err
		}
		if conv.Provider != provider {
			return catalog.Usage{}, ErrInvalidRequest
		}
		session = &catalog.Session{ProviderSessionID: conv.ProviderSessionID, Cwd: conv.Cwd}
	}
	return s.opts.Catalog.Usage(ctx, provider, session, force)
}

// StopBackgroundTasks stops the conversation's background tasks that are
// still running.
func (s *Service) StopBackgroundTasks(ctx context.Context, scopeID int64, conversationID string) error {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return err
	}
	ids, err := s.store.OpenBackgroundTasks(ctx, conversationID)
	if err != nil || len(ids) == 0 {
		return err
	}
	s.mu.Lock()
	r := s.runners[conversationID]
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.StopTasks(ctx, ids)
}
