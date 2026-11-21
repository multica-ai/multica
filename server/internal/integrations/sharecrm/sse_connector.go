package sharecrm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sse_connector runs one installation's SSE session against the ShareCRM IM
// Gateway. Reconnect/backoff/lease live in engine.Supervisor; run() returns on
// ctx cancel, max_lifetime expiry (server closes), or a broken stream.

const (
	defaultReconnectDelay = time.Second
	sseConnectTimeout     = 30 * time.Second
)

// sseConnector owns a single SSE GET for one appId.
type sseConnector struct {
	httpClient *http.Client
	baseURL    string
	appID      string
	appSecret  string
	client     *Client
	onMessage  func(ctx context.Context, data *botMessageData) error
	logger     *slog.Logger

	lastEventID string
	botFullID   string
}

func (c *sseConnector) withDefaults() {
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	if c.client == nil {
		c.client = NewClient(c.httpClient)
	}
	c.baseURL = strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	if c.baseURL == "" {
		c.baseURL = DefaultGatewayBaseURL
	}
}

// run blocks until ctx is cancelled or the stream ends fatally.
func (c *sseConnector) run(ctx context.Context) error {
	c.withDefaults()
	delay := defaultReconnectDelay

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		err := c.openAndRead(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			c.logger.WarnContext(ctx, "sharecrm sse: stream ended",
				"app_id", c.appID, "error", err, "reconnect_in", delay)
		} else {
			c.logger.InfoContext(ctx, "sharecrm sse: stream closed, reconnecting",
				"app_id", c.appID, "reconnect_in", delay)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		// Cap backoff at 60s; reset on successful connected event inside openAndRead.
		if delay < 60*time.Second {
			delay *= 2
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
		}
	}
}

func (c *sseConnector) openAndRead(ctx context.Context) error {
	token, err := c.client.accessToken(ctx, c.baseURL, c.appID, c.appSecret)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	url := fmt.Sprintf("%s/im-gateway/bot/events?token=%s&version=%s",
		c.baseURL, token, GatewayProtocolVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.lastEventID != "" {
		req.Header.Set("Last-Event-ID", c.lastEventID)
	}

	// Use a client without overall Timeout so the stream can live for hours.
	// The shared Client may have one; clone transport only.
	httpClient := c.httpClient
	if httpClient.Timeout > 0 {
		clone := *httpClient
		clone.Timeout = 0
		httpClient = &clone
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sse dial: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		c.client.invalidate(c.appID)
		return errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("sse http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	return c.readStream(ctx, resp.Body)
}

func (c *sseConnector) readStream(ctx context.Context, body io.Reader) error {
	reader := bufio.NewReaderSize(body, 64*1024)
	var (
		eventName string
		eventID   string
		dataLines []string
		retryMS   int
	)
	flush := func() error {
		if len(dataLines) == 0 {
			eventName, eventID, dataLines, retryMS = "", "", nil, 0
			return nil
		}
		data := strings.Join(dataLines, "\n")
		name := eventName
		if name == "" {
			name = "message"
		}
		id := eventID
		eventName, eventID, dataLines, retryMS = "", "", nil, 0
		return c.handleEvent(ctx, name, id, data)
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				_ = flush()
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")

		// SSE comment (heartbeat): ": keepalive"
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(line[6:])
			continue
		}
		if strings.HasPrefix(line, "id:") {
			eventID = strings.TrimSpace(line[3:])
			continue
		}
		if strings.HasPrefix(line, "retry:") {
			if n, e := strconv.Atoi(strings.TrimSpace(line[6:])); e == nil && n >= 0 {
				retryMS = n
				_ = retryMS // absorbed on connected; reconnect uses Supervisor backoff
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(line[5:]))
		}
	}
}

func (c *sseConnector) handleEvent(ctx context.Context, name, id, data string) error {
	if id != "" {
		c.lastEventID = id
	}
	var envelope struct {
		Type   string          `json:"type"`
		Reason string          `json:"reason"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		c.logger.WarnContext(ctx, "sharecrm sse: bad event json", "error", err)
		return nil
	}
	typ := envelope.Type
	if typ == "" {
		typ = name
	}
	switch typ {
	case "connected":
		var cd struct {
			BotFullID       string `json:"bot_full_id"`
			ProtocolVersion string `json:"protocol_version"`
			ClientVersion   string `json:"client_version"`
			MaxLifetime      int64  `json:"max_lifetime"`
			Retry           int64  `json:"retry"`
		}
		if len(envelope.Data) > 0 {
			_ = json.Unmarshal(envelope.Data, &cd)
		}
		c.botFullID = cd.BotFullID
		c.logger.InfoContext(ctx, "sharecrm sse: connected",
			"app_id", c.appID, "bot_full_id", c.botFullID,
			"protocol_version", cd.ProtocolVersion, "max_lifetime_ms", cd.MaxLifetime)
		return nil
	case "reset":
		c.logger.WarnContext(ctx, "sharecrm sse: reset", "reason", envelope.Reason)
		c.lastEventID = ""
		return nil
	case "message":
		// Full envelope: {type, version, data: botMessageData}
		var full struct {
			Data botMessageData `json:"data"`
		}
		if err := json.Unmarshal([]byte(data), &full); err != nil {
			c.logger.WarnContext(ctx, "sharecrm sse: bad message envelope", "error", err)
			return nil
		}
		if c.onMessage != nil {
			if err := c.onMessage(ctx, &full.Data); err != nil {
				c.logger.WarnContext(ctx, "sharecrm sse: onMessage error", "error", err)
			}
		}
		return nil
	case "error":
		c.logger.WarnContext(ctx, "sharecrm sse: error event", "data", truncate(data, 200))
		return nil
	default:
		c.logger.DebugContext(ctx, "sharecrm sse: ignored event", "type", typ)
		return nil
	}
}
