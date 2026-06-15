package wechat

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ConnectorRegistry tracks active WSConnectors by bot_id so the outbound
// path can send replies through the correct connection. The Hub registers
// connectors on start and unregisters on teardown.
type ConnectorRegistry struct {
	mu    sync.RWMutex
	conns map[string]*WSConnector
}

func NewConnectorRegistry() *ConnectorRegistry {
	return &ConnectorRegistry{conns: make(map[string]*WSConnector)}
}

func (r *ConnectorRegistry) Register(botID string, c *WSConnector) {
	r.mu.Lock()
	r.conns[botID] = c
	r.mu.Unlock()
}

func (r *ConnectorRegistry) Unregister(botID string) {
	r.mu.Lock()
	delete(r.conns, botID)
	r.mu.Unlock()
}

func (r *ConnectorRegistry) Get(botID string) (*WSConnector, bool) {
	r.mu.RLock()
	c, ok := r.conns[botID]
	r.mu.RUnlock()
	return c, ok
}

// Replier sends stream/markdown replies through the active WS connector.
// It is the outbound counterpart to the inbound Dispatcher.
type Replier struct {
	registry *ConnectorRegistry
	logger   *slog.Logger
}

func NewReplier(registry *ConnectorRegistry, logger *slog.Logger) *Replier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Replier{registry: registry, logger: logger}
}

// SendStream sends a streaming reply frame. The connector uses the
// callback's req_id to route the reply to the correct user.
func (r *Replier) SendStream(botID, callbackReqID, streamID, content string, finish bool) error {
	conn, ok := r.registry.Get(botID)
	if !ok {
		return fmt.Errorf("no active connector for bot %s", botID)
	}
	return conn.RespondStream(callbackReqID, streamID, content, finish)
}

// SendMarkdown sends a one-shot markdown reply.
func (r *Replier) SendMarkdown(botID, callbackReqID, content string) error {
	conn, ok := r.registry.Get(botID)
	if !ok {
		return fmt.Errorf("no active connector for bot %s", botID)
	}
	return conn.RespondMarkdown(callbackReqID, content)
}

// SendTypingIndicator sends an empty first stream frame as a "typing"
// indicator so the user sees immediate feedback.
func (r *Replier) SendTypingIndicator(botID, callbackReqID, streamID string) {
	if err := r.SendStream(botID, callbackReqID, streamID, "", false); err != nil {
		r.logger.Debug("wechat: typing indicator failed", "error", err)
	}
}

// GenerateStreamID creates a unique stream ID for a reply sequence.
func GenerateStreamID(messageID string) string {
	return fmt.Sprintf("stream_%s_%d", messageID, time.Now().UnixMilli())
}
