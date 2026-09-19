package sharecrm

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	ackProcessingText  = "👀 On it — I'll reply here when it's ready."
	ackCoalesceWindow  = 5 * time.Second
)

type ackNotifier struct {
	client  *Client
	decrypt Decrypter
	logger  *slog.Logger
	window  time.Duration
	now     func() time.Time

	mu      sync.Mutex
	lastAck map[string]time.Time

	sendText func(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error
}

var _ engine.TypingNotifier = (*ackNotifier)(nil)

func NewAckNotifier(client *Client, decrypt Decrypter, logger *slog.Logger) *ackNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &ackNotifier{
		client:  client,
		decrypt: decrypt,
		logger:  logger,
		window:  ackCoalesceWindow,
		lastAck: make(map[string]time.Time),
		now:     time.Now,
	}
}

func (n *ackNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	if n.suppress(sessionID) {
		return
	}
	send := n.sendText
	if send == nil {
		send = n.realSend
	}
	if err := send(ctx, inst, msg, ackProcessingText); err != nil {
		n.logger.WarnContext(ctx, "sharecrm ack: send failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
	}
}

func (n *ackNotifier) OnSettled(_ context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	delete(n.lastAck, key)
	n.mu.Unlock()
}

func (n *ackNotifier) suppress(sessionID pgtype.UUID) bool {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return false
	}
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	if last, ok := n.lastAck[key]; ok && now.Sub(last) < n.window {
		return true
	}
	for k, last := range n.lastAck {
		if now.Sub(last) >= n.window {
			delete(n.lastAck, k)
		}
	}
	n.lastAck[key] = now
	return false
}

func (n *ackNotifier) realSend(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	_, err := sendInstallationText(ctx, n.client, n.decrypt, inst, targetFromMessage(msg), text)
	return err
}
