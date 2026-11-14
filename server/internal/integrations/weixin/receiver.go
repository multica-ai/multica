package weixin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// UpdatesClient is the receive-side slice of Client. The interface keeps the
// cursor-ordering contract testable without a network server.
type UpdatesClient interface {
	GetUpdates(ctx context.Context, baseURL, token, cursor string, timeout time.Duration) (Updates, error)
}

var _ UpdatesClient = (*Client)(nil)

// CursorStore persists the opaque get_updates_buf outside installation config.
// Installation config is supervisor-fingerprinted; writing a cursor there after
// every poll would continuously restart the channel connection.
type CursorStore interface {
	LoadCursor(ctx context.Context, installationKey string) (string, error)
	SaveCursor(ctx context.Context, installationKey, cursor string) error
}

// ReceiverConfig is one installation's long-poll receive configuration.
type ReceiverConfig struct {
	InstallationKey string
	BotID           string
	BotToken        string
	BaseURL         string
	PollTimeout     time.Duration
	Client          UpdatesClient
	Cursors         CursorStore
	Handler         channel.InboundHandler
}

// Receiver owns the getupdates loop for one installation. It processes messages
// sequentially and advances the cursor only after the complete returned batch
// has been accepted by the shared Router. A crash before cursor persistence
// therefore causes safe replay rather than message loss; engine dedup absorbs
// already-committed messages.
type Receiver struct {
	installationKey string
	botID           string
	botToken        string
	baseURL         string
	pollTimeout     time.Duration
	client          UpdatesClient
	cursors         CursorStore
	handler         channel.InboundHandler
}

func NewReceiver(cfg ReceiverConfig) (*Receiver, error) {
	if strings.TrimSpace(cfg.InstallationKey) == "" {
		return nil, errors.New("weixin: receiver installation key is required")
	}
	if strings.TrimSpace(cfg.BotID) == "" {
		return nil, errors.New("weixin: receiver bot id is required")
	}
	if strings.TrimSpace(cfg.BotToken) == "" {
		return nil, errors.New("weixin: receiver bot token is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("weixin: receiver base URL is required")
	}
	if cfg.Client == nil {
		return nil, errors.New("weixin: receiver client is required")
	}
	if cfg.Cursors == nil {
		return nil, errors.New("weixin: receiver cursor store is required")
	}
	if cfg.Handler == nil {
		return nil, errors.New("weixin: receiver inbound handler is required")
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = defaultPollTimeout
	}
	return &Receiver{
		installationKey: cfg.InstallationKey,
		botID:           cfg.BotID,
		botToken:        cfg.BotToken,
		baseURL:         cfg.BaseURL,
		pollTimeout:     cfg.PollTimeout,
		client:          cfg.Client,
		cursors:         cfg.Cursors,
		handler:         cfg.Handler,
	}, nil
}

// Run blocks until ctx is cancelled or an infrastructure/protocol failure asks
// the supervisor to reconnect. Product-level drops remain the Router's concern.
func (r *Receiver) Run(ctx context.Context) error {
	cursor, err := r.cursors.LoadCursor(ctx, r.installationKey)
	if err != nil {
		return fmt.Errorf("weixin: load receive cursor: %w", err)
	}
	timeout := r.pollTimeout
	for {
		if ctx.Err() != nil {
			return nil
		}
		updates, err := r.client.GetUpdates(ctx, r.baseURL, r.botToken, cursor, timeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		timeout = updates.pollTimeout(timeout)
		for _, raw := range updates.Messages {
			msg, ok := NormalizeInbound(raw, r.botID)
			if !ok {
				continue
			}
			if err := r.handler(ctx, msg); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("weixin: handle inbound message %q: %w", msg.MessageID, err)
			}
		}
		if updates.Cursor != "" && updates.Cursor != cursor {
			if err := r.cursors.SaveCursor(ctx, r.installationKey, updates.Cursor); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("weixin: save receive cursor: %w", err)
			}
			cursor = updates.Cursor
		}
	}
}
