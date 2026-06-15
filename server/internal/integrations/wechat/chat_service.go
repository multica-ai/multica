package wechat

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ChatSessionQueries interface {
	GetWechatChatSessionBinding(ctx context.Context, arg db.GetWechatChatSessionBindingParams) (db.WechatChatSessionBinding, error)
	CreateWechatChatSessionBinding(ctx context.Context, arg db.CreateWechatChatSessionBindingParams) (db.WechatChatSessionBinding, error)
	CreateChatSession(ctx context.Context, arg db.CreateChatSessionParams) (db.ChatSession, error)
	CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error)
	MarkWechatInboundDedupProcessed(ctx context.Context, arg db.MarkWechatInboundDedupProcessedParams) (int64, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	UpdateWechatChatSessionCallbackReqID(ctx context.Context, arg db.UpdateWechatChatSessionCallbackReqIDParams) error
}

func (s *ChatSessionService) UpdateCallbackReqID(ctx context.Context, chatSessionID pgtype.UUID, callbackReqID string) {
	_ = s.queries.UpdateWechatChatSessionCallbackReqID(ctx, db.UpdateWechatChatSessionCallbackReqIDParams{
		LastCallbackReqID: callbackReqID,
		ChatSessionID:     chatSessionID,
	})
}


type ChatSessionService struct {
	queries ChatSessionQueries
	pool    *pgxpool.Pool
}

func NewChatSessionService(queries ChatSessionQueries, pool *pgxpool.Pool) *ChatSessionService {
	return &ChatSessionService{queries: queries, pool: pool}
}

type EnsureChatSessionParams struct {
	InstallationID pgtype.UUID
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	WechatChatID   string
	ChatType       ChatType
	CreatorUserID  pgtype.UUID
}

func (s *ChatSessionService) EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (db.ChatSession, error) {
	binding, err := s.queries.GetWechatChatSessionBinding(ctx, db.GetWechatChatSessionBindingParams{
		InstallationID: p.InstallationID,
		WechatChatID:   p.WechatChatID,
	})
	if err == nil {
		return s.queries.GetChatSession(ctx, binding.ChatSessionID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.ChatSession{}, fmt.Errorf("lookup binding: %w", err)
	}

	session, err := s.queries.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
		CreatorID:   p.CreatorUserID,
		Title:       "WeChat Chat",
	})
	if err != nil {
		return db.ChatSession{}, fmt.Errorf("create session: %w", err)
	}

	_, err = s.queries.CreateWechatChatSessionBinding(ctx, db.CreateWechatChatSessionBindingParams{
		ChatSessionID:  session.ID,
		InstallationID: p.InstallationID,
		WechatChatID:   p.WechatChatID,
		WechatChatType: string(p.ChatType),
	})
	if err != nil {
		return db.ChatSession{}, fmt.Errorf("create binding: %w", err)
	}

	return session, nil
}

type AppendMessageParams struct {
	ChatSessionID pgtype.UUID
	UserID        pgtype.UUID
	Content       string
	MessageID     string
	ClaimToken    pgtype.UUID
}

func (s *ChatSessionService) AppendUserMessage(ctx context.Context, p AppendMessageParams) error {
	_, err := s.queries.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: p.ChatSessionID,
		Role:          "user",
		Content:       p.Content,
	})
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}

	_, _ = s.queries.MarkWechatInboundDedupProcessed(ctx, db.MarkWechatInboundDedupProcessedParams{
		MessageID:  p.MessageID,
		ClaimToken: p.ClaimToken,
	})

	return nil
}
