package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// ErrCompactUnavailable means there is no native conversation to compact.
var ErrCompactUnavailable = errors.New("local conversation: send a message before compacting this conversation")

// Compact runs the provider's native compaction as a durable turn without an
// authored user message. Completion and failures follow the normal event stream.
func (s *Service) Compact(ctx context.Context, scopeID int64, conversationID, requestID string) (SendResult, error) {
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return SendResult{}, err
	}
	if !nativeID.MatchString(requestID) {
		return SendResult{}, fmt.Errorf("%w: request_id is required", ErrInvalidRequest)
	}
	sum := sha256.Sum256([]byte("compact\x00" + conv.ID))
	fingerprint := hex.EncodeToString(sum[:])
	if result, done, err := s.replay(ctx, scopeID, requestID, fingerprint); done || err != nil {
		return result, err
	}
	if conv.Archived == 1 {
		return SendResult{}, ErrArchived
	}
	if conv.ProviderSessionID == "" || conv.TurnCount == 0 {
		return SendResult{}, ErrCompactUnavailable
	}
	if conv.Status == "running" {
		return SendResult{}, ErrTurnRunning
	}
	if err := s.acceptTurn(); err != nil {
		return SendResult{}, err
	}
	turnID := "turn-" + requestID
	trace := s.traces.turn(conv, requestID, turnID, timing.TurnUser)
	created := trace.Begin(timing.StageRunnerCreate)
	r, err := s.runner(ctx, conv)
	created(err)
	if err != nil {
		return SendResult{}, err
	}
	err = r.Send(ctx, SendRequest{TurnID: turnID, RequestID: requestID, Fingerprint: fingerprint, Hidden: true, Compact: true, Text: "/compact", Trace: trace})
	switch {
	case errors.Is(err, store.ErrDuplicateRequest):
		if result, done, replayErr := s.replay(ctx, scopeID, requestID, fingerprint); done || replayErr != nil {
			return result, replayErr
		}
	case errors.Is(err, ErrOutcomeUnknown):
		if markErr := s.store.MarkUncertain(context.WithoutCancel(ctx), scopeID, requestID); markErr != nil {
			err = errors.Join(err, markErr)
		}
	}
	return SendResult{TurnID: turnID}, err
}
