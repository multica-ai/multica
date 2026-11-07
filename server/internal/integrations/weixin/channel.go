package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type accountStore interface {
	GetWeixinAccountState(context.Context, db.GetWeixinAccountStateParams) (db.WeixinAccountState, error)
	SaveWeixinContext(context.Context, db.SaveWeixinContextParams) (int64, error)
	SaveWeixinCursor(context.Context, db.SaveWeixinCursorParams) (int64, error)
	ExpireWeixinInstallation(context.Context, db.ExpireWeixinInstallationParams) (int64, error)
}

type ChannelDeps struct {
	Queries *db.Queries
	Box     *secretbox.Box
	Logger  *slog.Logger
	Client  *http.Client
}

type weixinChannel struct {
	id      pgtype.UUID
	creds   credentials
	api     *apiClient
	q       accountStore
	box     *secretbox.Box
	handler channel.InboundHandler
	logger  *slog.Logger
	config  []byte
}

func RegisterWeixin(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeWeixin, func(cfg channel.Config) (channel.Channel, error) {
		creds, err := decodeCredentials(cfg.Raw, deps.Box)
		if err != nil {
			return nil, err
		}
		if deps.Queries == nil {
			return nil, errors.New("weixin: account store is required")
		}
		logger := deps.Logger
		if logger == nil {
			logger = slog.Default()
		}
		return &weixinChannel{id: cfg.ID, config: cfg.Raw, creds: creds, api: newAPI(creds.BaseURL, creds.Token, deps.Client), q: deps.Queries, box: deps.Box, handler: cfg.Handler, logger: logger}, nil
	})
}

func (c *weixinChannel) Type() channel.Type { return TypeWeixin }
func (c *weixinChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapAttachment
}
func (c *weixinChannel) Disconnect(context.Context) error { return nil }

func (c *weixinChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("weixin: inbound handler is required")
	}
	state, err := c.q.GetWeixinAccountState(ctx, db.GetWeixinAccountStateParams{InstallationID: c.id, BotID: c.creds.AppID})
	if err != nil {
		return err
	}
	if err := c.api.notify(ctx, true); err != nil && ctx.Err() == nil {
		c.logger.WarnContext(ctx, "weixin: start notification failed", "error", err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = c.api.notify(stop, false)
	}()
	cursor := state.GetUpdatesBuf
	for ctx.Err() == nil {
		result, err := c.api.Updates(ctx, cursor)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		if err != nil {
			if errors.Is(err, ErrTokenExpired) {
				// Fence against a newer QR login rotating this installation while
				// its predecessor's long poll was still in flight.
				_, _ = c.q.ExpireWeixinInstallation(ctx, db.ExpireWeixinInstallationParams{ID: c.id, Config: c.config})
			}
			return err
		}
		for _, msg := range result.Messages {
			in, ok := normalizeMessage(c.creds, msg)
			if !ok {
				continue
			}
			if msg.ContextToken != "" {
				sealed, err := c.box.Seal([]byte(msg.ContextToken))
				if err != nil {
					return err
				}
				if _, err := c.q.SaveWeixinContext(ctx, db.SaveWeixinContextParams{InstallationID: c.id, BotID: c.creds.AppID, UserID: c.creds.UserID, ContextTokenEncrypted: sealed, ContextTimeMs: msg.CreatedAt}); err != nil {
					return err
				}
			}
			// The shared engine commits dedup and inbound persistence before
			// returning; advance the provider cursor only after the entire batch.
			if err := c.handler(ctx, in); err != nil {
				return err
			}
		}
		if result.Cursor != "" {
			rows, err := c.q.SaveWeixinCursor(ctx, db.SaveWeixinCursorParams{InstallationID: c.id, BotID: c.creds.AppID, GetUpdatesBuf: result.Cursor})
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrNotFound
			}
			cursor = result.Cursor
		}
		// Some transports return immediately without events; avoid a hot loop.
		if len(result.Messages) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

func (c *weixinChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if out.ChatID != c.creds.UserID {
		return channel.SendResult{}, ErrAccess
	}
	state, err := c.q.GetWeixinAccountState(ctx, db.GetWeixinAccountStateParams{InstallationID: c.id, BotID: c.creds.AppID})
	if err != nil {
		return channel.SendResult{}, err
	}
	token, err := c.box.Open(state.ContextTokenEncrypted)
	if err != nil {
		return channel.SendResult{}, ErrNoContext
	}
	id, err := c.api.Send(ctx, textMessage(out.ChatID, string(token), util.UUIDToString(dbid.NewV7()), out.Text))
	return channel.SendResult{MessageID: id}, err
}

type weixinRawEvent struct {
	BotID     string  `json:"bot_id"`
	EventType string  `json:"event_type"`
	Message   message `json:"message"`
}

func normalizeMessage(creds credentials, msg message) (channel.InboundMessage, bool) {
	// A partial update can share its message ID with the final update. Claiming
	// it would make the deduper discard the completed message later.
	if msg.State != nil && *msg.State != 2 {
		return channel.InboundMessage{}, false
	}
	if msg.Type != 1 || msg.ID == "" || msg.GroupID != "" || msg.From != creds.UserID || msg.From == "" || msg.To != "" && msg.To != creds.AppID {
		return channel.InboundMessage{}, false
	}
	var text []string
	kind := channel.MsgTypeText
	for _, item := range msg.Items {
		switch {
		case item.Type == 1 && item.Text != nil:
			text = append(text, item.Text.Text)
		case item.Type == 2 && item.Image != nil:
			text = append(text, "[Image]")
			kind = channel.MsgTypeImage
		case item.Type == 3 && item.Voice != nil:
			if item.Voice.Text != "" {
				text = append(text, item.Voice.Text)
			} else {
				text = append(text, "[Audio]")
				kind = channel.MsgTypeAudio
			}
		case item.Type == 4 && item.File != nil:
			text = append(text, "[File]")
			kind = channel.MsgTypeFile
		case item.Type == 5 && item.Video != nil:
			text = append(text, "[Video]")
			kind = channel.MsgTypeVideo
		}
	}
	if len(text) == 0 {
		return channel.InboundMessage{}, false
	}
	msg.ContextToken = ""
	raw, _ := json.Marshal(weixinRawEvent{BotID: creds.AppID, EventType: "message", Message: msg})
	return channel.InboundMessage{
		Source:    channel.Source{ChannelType: TypeWeixin, ChatID: msg.From, ChatType: channel.ChatTypeP2P, SenderID: msg.From},
		MessageID: string(msg.ID), EventID: string(msg.ID), Type: kind,
		Text: strings.Join(text, "\n"), Raw: raw,
	}, true
}
