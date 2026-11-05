package service

import (
	"context"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func (s *Service) Navigation(ctx context.Context, scopeID int64, id string, input store.NavigationQuery) (store.Navigation, error) {
	conv, err := s.scopedConversation(ctx, scopeID, id)
	if err != nil {
		return store.Navigation{}, err
	}
	if (input.BeforeIndex != nil && *input.BeforeIndex > conv.TurnCount) || (input.AfterIndex != nil && *input.AfterIndex >= conv.TurnCount) {
		return store.Navigation{}, ErrInvalidRequest
	}
	return s.store.Navigation(ctx, id, input)
}
