package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type feishuAgentBotWSRunner struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	handler *handler.Handler

	mu      sync.Mutex
	started map[string]struct{}
}

func newFeishuAgentBotWSRunner(pool *pgxpool.Pool, queries *db.Queries, h *handler.Handler) *feishuAgentBotWSRunner {
	return &feishuAgentBotWSRunner{
		pool:    pool,
		queries: queries,
		handler: h,
		started: make(map[string]struct{}),
	}
}

func (r *feishuAgentBotWSRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	r.sync(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sync(ctx)
		}
	}
}

func (r *feishuAgentBotWSRunner) sync(ctx context.Context) {
	configs, err := r.queries.ListEnabledAgentFeishuBotConfigs(ctx)
	if err != nil {
		slog.Warn("Feishu agent bot websocket config scan failed", "error", err)
		return
	}
	for _, cfg := range configs {
		r.mu.Lock()
		_, exists := r.started[cfg.AppID]
		if !exists {
			r.started[cfg.AppID] = struct{}{}
		}
		r.mu.Unlock()
		if exists {
			continue
		}
		go r.runApp(ctx, cfg)
	}
}

func (r *feishuAgentBotWSRunner) runApp(ctx context.Context, cfg db.AgentFeishuBotConfig) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		r.clearStarted(cfg.AppID)
		slog.Warn("Feishu agent bot websocket lock acquire failed", "app_id", cfg.AppID, "error", err)
		return
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, "select pg_try_advisory_lock(hashtextextended($1, 0))", "feishu-agent-bot:"+cfg.AppID).Scan(&locked); err != nil {
		r.clearStarted(cfg.AppID)
		slog.Warn("Feishu agent bot websocket lock failed", "app_id", cfg.AppID, "error", err)
		return
	}
	if !locked {
		r.clearStarted(cfg.AppID)
		slog.Debug("Feishu agent bot websocket already running elsewhere", "app_id", cfg.AppID)
		return
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "select pg_advisory_unlock(hashtextextended($1, 0))", "feishu-agent-bot:"+cfg.AppID)
		r.clearStarted(cfg.AppID)
	}()

	d := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(eventCtx context.Context, event *larkim.P2MessageReceiveV1) error {
			return r.handleMessage(eventCtx, cfg.AppID, event)
		}).
		OnP2MessageReadV1(func(context.Context, *larkim.P2MessageReadV1) error {
			return nil
		})

	client := ws.NewClient(
		cfg.AppID,
		cfg.AppSecret,
		ws.WithEventHandler(d),
		ws.WithLogLevel(larkcore.LogLevelWarn),
	)
	slog.Info("Feishu agent bot websocket starting", "app_id", cfg.AppID, "agent_id", util.UUIDToString(cfg.AgentID))
	if err := client.Start(ctx); err != nil {
		slog.Warn("Feishu agent bot websocket stopped", "app_id", cfg.AppID, "error", err)
	}
}

func (r *feishuAgentBotWSRunner) handleMessage(ctx context.Context, appID string, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}
	message := event.Event.Message
	var senderOpenID, senderUserID string
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		senderOpenID = stringPtrValue(event.Event.Sender.SenderId.OpenId)
		senderUserID = stringPtrValue(event.Event.Sender.SenderId.UserId)
	}
	err := r.handler.HandleFeishuAgentBotMessage(
		ctx,
		appID,
		senderOpenID,
		senderUserID,
		stringPtrValue(message.MessageId),
		stringPtrValue(message.RootId),
		stringPtrValue(message.ParentId),
		stringPtrValue(message.ChatId),
		stringPtrValue(message.ChatType),
		stringPtrValue(message.MessageType),
		stringPtrValue(message.Content),
	)
	if err != nil {
		slog.Warn("Feishu agent bot websocket message failed", "app_id", appID, "message_id", stringPtrValue(message.MessageId), "error", err)
	}
	return err
}

func (r *feishuAgentBotWSRunner) clearStarted(appID string) {
	r.mu.Lock()
	delete(r.started, appID)
	r.mu.Unlock()
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
