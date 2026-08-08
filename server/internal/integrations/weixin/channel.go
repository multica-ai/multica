package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

type Credentials struct {
	BotID        string
	WeixinUserID string
	BaseURL      string
	Token        string
}

type CredentialsResolver interface {
	Credentials(Installation) (Credentials, error)
}

type SecretboxCredentialsResolver struct{ Box *secretbox.Box }

func NewSecretboxCredentialsResolver(box *secretbox.Box) (*SecretboxCredentialsResolver, error) {
	if box == nil {
		return nil, errors.New("weixin: credentials resolver requires secretbox")
	}
	return &SecretboxCredentialsResolver{Box: box}, nil
}

func (r *SecretboxCredentialsResolver) Credentials(inst Installation) (Credentials, error) {
	token, err := r.Box.Open(inst.TokenEncrypted)
	if err != nil {
		return Credentials{}, fmt.Errorf("weixin: decrypt bot token: %w", err)
	}
	return Credentials{BotID: inst.BotID, WeixinUserID: inst.WeixinUserID, BaseURL: inst.BaseURL, Token: string(token)}, nil
}

type inboundEnvelope struct {
	BotID   string         `json:"bot_id"`
	Message InboundMessage `json:"message"`
}

type liveSender struct {
	client *Client
	creds  Credentials
	mu     sync.RWMutex
	ctx    map[string]string
}

func newLiveSender(client *Client, creds Credentials) *liveSender {
	return &liveSender{client: client, creds: creds, ctx: make(map[string]string)}
}

func (s *liveSender) setContext(userID, token string) {
	if userID == "" || token == "" {
		return
	}
	s.mu.Lock()
	s.ctx[userID] = token
	s.mu.Unlock()
}

func (s *liveSender) context(userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ctx[userID]
}

func (s *liveSender) send(ctx context.Context, userID, text string) (string, error) {
	var lastID string
	for _, chunk := range splitText(text, 4000) {
		contextToken := s.context(userID)
		id, err := s.client.SendText(ctx, s.creds.BaseURL, s.creds.Token, userID, chunk, contextToken)
		var apiErr *APIError
		if err != nil && contextToken != "" && errors.As(err, &apiErr) && (apiErr.Ret == -14 || apiErr.ErrCode == -14) {
			s.mu.Lock()
			delete(s.ctx, userID)
			s.mu.Unlock()
			id, err = s.client.SendText(ctx, s.creds.BaseURL, s.creds.Token, userID, chunk, "")
		}
		if err != nil {
			return "", err
		}
		lastID = id
	}
	return lastID, nil
}

func splitText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}
	runes := []rune(text)
	out := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for len(runes) > 0 {
		n := maxRunes
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

type SendersRegistry struct {
	mu    sync.RWMutex
	items map[string]*liveSender
}

func NewSendersRegistry() *SendersRegistry {
	return &SendersRegistry{items: make(map[string]*liveSender)}
}

func (r *SendersRegistry) set(id pgtype.UUID, sender *liveSender) {
	r.mu.Lock()
	r.items[id.String()] = sender
	r.mu.Unlock()
}

func (r *SendersRegistry) clear(id pgtype.UUID) {
	r.mu.Lock()
	delete(r.items, id.String())
	r.mu.Unlock()
}

func (r *SendersRegistry) get(id pgtype.UUID) *liveSender {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items[id.String()]
}

type weixinChannel struct {
	id       pgtype.UUID
	creds    Credentials
	client   *Client
	handler  channel.InboundHandler
	registry *SendersRegistry
	logger   *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

func (c *weixinChannel) Type() channel.Type               { return TypeWeixin }
func (c *weixinChannel) Capabilities() channel.Capability { return channel.CapText }

func (c *weixinChannel) Connect(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	defer cancel()

	sender := newLiveSender(c.client, c.creds)
	if c.registry != nil {
		c.registry.set(c.id, sender)
		defer c.registry.clear(c.id)
	}
	syncBuf := ""
	timeout := 35 * time.Second
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		pollCtx, pollCancel := context.WithTimeout(ctx, timeout+5*time.Second)
		resp, err := c.client.GetUpdates(pollCtx, c.creds.BaseURL, c.creds.Token, syncBuf)
		pollCancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("weixin: poll updates: %w", err)
		}
		if resp.Ret != 0 || resp.ErrCode != 0 {
			return fmt.Errorf("weixin: getupdates ret=%d errcode=%d: %s", resp.Ret, resp.ErrCode, resp.ErrorMsg)
		}
		if resp.SyncBuf != "" {
			syncBuf = resp.SyncBuf
		}
		if resp.TimeoutMS > 0 && resp.TimeoutMS <= 60000 {
			timeout = time.Duration(resp.TimeoutMS) * time.Millisecond
		}
		for _, msg := range resp.Messages {
			c.handle(ctx, sender, msg)
		}
	}
}

func (c *weixinChannel) handle(ctx context.Context, sender *liveSender, msg InboundMessage) {
	// This integration is deliberately private: only the Weixin identity that
	// scanned the QR code may drive the bound agent, and group messages are ignored.
	if msg.FromUserID != c.creds.WeixinUserID || msg.RoomID != "" || msg.ChatRoomID != "" {
		return
	}
	text := msg.Text()
	if text == "" {
		return
	}
	sender.setContext(msg.FromUserID, msg.ContextToken)
	raw, err := json.Marshal(inboundEnvelope{BotID: c.creds.BotID, Message: msg})
	if err != nil {
		return
	}
	id := msg.ID()
	if c.handler == nil {
		return
	}
	if err := c.handler(ctx, channel.InboundMessage{
		EventID: id, MessageID: id,
		Source: channel.Source{ChannelType: TypeWeixin, ChatID: msg.FromUserID, ChatType: channel.ChatTypeP2P, SenderID: msg.FromUserID},
		Type:   channel.MsgTypeText, Text: text, Raw: raw,
	}); err != nil {
		c.logger.WarnContext(ctx, "weixin: inbound handling failed", "error", err, "installation_id", c.id.String())
	}
}

func (c *weixinChannel) Disconnect(context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
	return nil
}

func (c *weixinChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if c.registry == nil {
		return channel.SendResult{}, errors.New("weixin: sender registry unavailable")
	}
	sender := c.registry.get(c.id)
	if sender == nil {
		return channel.SendResult{}, errors.New("weixin: connection not ready")
	}
	id, err := sender.send(ctx, out.ChatID, out.Text)
	return channel.SendResult{MessageID: id}, err
}

type ChannelDeps struct {
	Credentials CredentialsResolver
	Client      *Client
	Senders     *SendersRegistry
	Logger      *slog.Logger
}

func RegisterWeixin(reg *channel.Registry, deps ChannelDeps) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := deps.Client
	if client == nil {
		client = NewClient(nil)
	}
	reg.Register(TypeWeixin, func(cfg channel.Config) (channel.Channel, error) {
		if deps.Credentials == nil {
			return nil, errors.New("weixin: credentials resolver missing")
		}
		var raw installConfig
		if err := json.Unmarshal(cfg.Raw, &raw); err != nil {
			return nil, err
		}
		inst := Installation{BotID: raw.BotID, WeixinUserID: raw.WeixinUserID, BaseURL: raw.BaseURL, TokenEncrypted: raw.TokenEncrypted}
		creds, err := deps.Credentials.Credentials(inst)
		if err != nil {
			return nil, err
		}
		return &weixinChannel{id: cfg.ID, creds: creds, client: client, handler: cfg.Handler, registry: deps.Senders, logger: logger}, nil
	})
}
