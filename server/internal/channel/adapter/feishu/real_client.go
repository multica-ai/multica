package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	channelmetrics "github.com/multica-ai/multica/server/internal/channel/metrics"
)

const rawEventEnqueueTimeout = 5 * time.Second

// RealClient implements Client using the official Feishu/Lark SDK
// (github.com/larksuite/oapi-sdk-go/v3). It manages the WebSocket long
// connection for inbound events and uses the OpenAPI client for outbound
// operations (SendMessage, GetChatInfo, GetUserInfo).
//
// Concurrency: all methods are safe to call from concurrent goroutines.
// The events channel is buffered (capacity 64) so the SDK's receive loop
// is not blocked by a slow adapter pump.
type RealClient struct {
	appID     string
	appSecret string

	wsClient  *larkws.Client
	apiClient *lark.Client
	events    chan RawEvent
	botUserID string
	botReady  chan struct{} // closed once botUserID is known
	startOnce sync.Once
	stopOnce  sync.Once
	started   bool
	startErr  error
	startDone chan struct{} // closed once Start completes
	runCancel context.CancelFunc
	runDone   chan struct{}
	eventMu   sync.RWMutex
	closed    bool
}

// NewRealClient constructs a RealClient. Call Start to open the WebSocket
// connection. The returned client is not yet connected.
func NewRealClient(appID, appSecret, encryptKey, verifyToken string) *RealClient {
	rc := &RealClient{
		appID:     appID,
		appSecret: appSecret,
		events:    make(chan RawEvent, 64),
		botReady:  make(chan struct{}),
		startDone: make(chan struct{}),
		runDone:   make(chan struct{}),
	}

	// Build the event dispatcher. We register a handler for the
	// im.message.receive_v1 event type that feeds RawEvents into our channel.
	eventDispatcher := dispatcher.NewEventDispatcher(verifyToken, encryptKey)
	eventDispatcher.OnP2MessageReceiveV1(func(ctx context.Context, ev *larkim.P2MessageReceiveV1) error {
		return rc.handleMessageReceive(ctx, ev)
	})
	eventDispatcher.OnP2MessageRecalledV1(func(ctx context.Context, ev *larkim.P2MessageRecalledV1) error {
		return rc.handleMessageRecalled(ctx, ev)
	})

	// Build the WebSocket client with auto-reconnect enabled.
	rc.wsClient = larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(eventDispatcher),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithAutoReconnect(true),
	)

	// Build the HTTP API client for outbound calls.
	rc.apiClient = lark.NewClient(appID, appSecret,
		lark.WithEnableTokenCache(true),
	)

	return rc
}

// Start opens the WebSocket connection and begins delivering events on
// Subscribe(). It blocks until the initial handshake succeeds or the
// context is cancelled.
//
// The SDK's Start() blocks forever (it runs the receive loop internally),
// so we launch it in a separate goroutine. To detect a successful
// handshake, we fetch the bot's own user id via the OpenAPI — if the
// credentials are valid, the connection is considered established.
func (rc *RealClient) Start(ctx context.Context) error {
	rc.startOnce.Do(func() {
		// First verify credentials by fetching bot info. This is a
		// synchronous HTTP call that fails fast on auth errors.
		botUserID, err := rc.fetchBotUserID(ctx)
		if err != nil {
			rc.startErr = fmt.Errorf("feishu: verify credentials: %w", err)
			close(rc.startDone)
			return
		}
		rc.botUserID = botUserID
		close(rc.botReady)

		// Start the WebSocket client in a background goroutine. The SDK
		// handles normal reconnects internally; this loop restarts the
		// client if the SDK receive loop exits anyway.
		runCtx, cancel := context.WithCancel(ctx)
		rc.runCancel = cancel
		go func() {
			defer close(rc.runDone)
			backoff := time.Second
			for {
				if runCtx.Err() != nil {
					return
				}
				slog.Info("feishu: ws client starting")
				err := rc.wsClient.Start(runCtx)
				if runCtx.Err() != nil {
					return
				}
				if err != nil {
					slog.Error("feishu: ws client stopped; restarting", "error", err)
				} else {
					slog.Warn("feishu: ws client stopped without error; restarting")
				}
				select {
				case <-runCtx.Done():
					return
				case <-time.After(backoff):
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
			}
		}()

		rc.started = true
		close(rc.startDone)
	})

	<-rc.startDone
	return rc.startErr
}

// Stop tears down the WebSocket receive path. The upstream SDK does not expose
// an explicit Stop method, so we cancel the context supplied to Start and wait
// briefly for its goroutine to return. Regardless of SDK behaviour, the local
// event channel is closed exactly once and late SDK callbacks are dropped.
func (rc *RealClient) Stop(ctx context.Context) error {
	rc.stopOnce.Do(func() {
		if rc.runCancel != nil {
			rc.runCancel()
		}
		if rc.started {
			select {
			case <-rc.runDone:
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				slog.Warn("feishu: ws client did not stop before timeout")
			}
		}
		rc.eventMu.Lock()
		rc.closed = true
		close(rc.events)
		rc.eventMu.Unlock()
	})
	return nil
}

// Subscribe returns the receive-only event stream.
func (rc *RealClient) Subscribe() <-chan RawEvent {
	return rc.events
}

func (rc *RealClient) APIClient() *lark.Client {
	return rc.apiClient
}

// BotUserID returns the open_id of the bot account. Blocks until the
// bot info has been fetched (i.e. until Start completes successfully).
func (rc *RealClient) BotUserID() string {
	<-rc.botReady
	return rc.botUserID
}

// SendMessage sends a text or interactive message via the Feishu OpenAPI.
// Transient errors (5xx, network) are wrapped with RetryableError; 4xx
// errors are returned unwrapped.
func (rc *RealClient) SendMessage(ctx context.Context, req SendRequest) (SendResponse, error) {
	msgReq := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(req.ReceiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(req.MsgType).
			ReceiveId(req.ReceiveID).
			Content(req.Content).
			Build()).
		Build()

	resp, err := rc.apiClient.Im.Message.Create(ctx, msgReq)
	if err != nil {
		return SendResponse{}, RetryableError(fmt.Errorf("feishu api: %w", err))
	}
	if !resp.Success() {
		if resp.Code >= 500 {
			return SendResponse{}, RetryableError(
				fmt.Errorf("feishu 5xx: code=%d msg=%s", resp.Code, resp.Msg))
		}
		return SendResponse{}, fmt.Errorf("feishu 4xx: code=%d msg=%s", resp.Code, resp.Msg)
	}

	msgID := ""
	if resp.Data != nil && resp.Data.MessageId != nil {
		msgID = *resp.Data.MessageId
	}
	return SendResponse{MessageID: msgID}, nil
}

// GetChatInfo fetches metadata for a chat room via the Feishu OpenAPI.
func (rc *RealClient) GetChatInfo(ctx context.Context, chatID string) (ChatInfoResponse, error) {
	req := larkim.NewGetChatReqBuilder().
		ChatId(chatID).
		Build()

	resp, err := rc.apiClient.Im.Chat.Get(ctx, req)
	if err != nil {
		return ChatInfoResponse{}, fmt.Errorf("feishu api: %w", err)
	}
	if !resp.Success() {
		return ChatInfoResponse{}, fmt.Errorf("feishu: get chat: code=%d msg=%s", resp.Code, resp.Msg)
	}

	result := ChatInfoResponse{ID: chatID}
	if resp.Data != nil {
		if resp.Data.Name != nil {
			result.Name = *resp.Data.Name
		}
		if resp.Data.ChatMode != nil {
			result.Type = *resp.Data.ChatMode
		}
	}
	return result, nil
}

// GetUserInfo fetches metadata for a user via the Feishu OpenAPI.
// The Feishu IM service does not expose a direct user lookup; we use
// the contact service API instead.
func (rc *RealClient) GetUserInfo(ctx context.Context, userID string) (UserInfoResponse, error) {
	// Use the contact/v3/users/:user_id endpoint with open_id.
	resp, err := rc.apiClient.Get(ctx,
		fmt.Sprintf("https://open.feishu.cn/open-apis/contact/v3/users/%s?user_id_type=open_id", userID),
		nil,
		larkcore.AccessTokenTypeTenant,
	)
	if err != nil {
		return UserInfoResponse{}, fmt.Errorf("feishu api: %w", err)
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User struct {
				OpenID string `json:"open_id"`
				Name   string `json:"name"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return UserInfoResponse{}, fmt.Errorf("feishu: parse user info: %w", err)
	}
	if result.Code != 0 {
		return UserInfoResponse{}, fmt.Errorf("feishu: get user: code=%d msg=%s", result.Code, result.Msg)
	}

	return UserInfoResponse{
		OpenID: result.Data.User.OpenID,
		Name:   result.Data.User.Name,
	}, nil
}

// handleMessageReceive bridges the SDK's typed event to our RawEvent
// envelope. We re-marshal the event to JSON so the adapter's normaliseEvent
// can parse it — this keeps the adapter SDK-agnostic.
func (rc *RealClient) handleMessageReceive(ctx context.Context, ev *larkim.P2MessageReceiveV1) error {
	if ev == nil {
		return nil
	}

	payload, err := json.Marshal(ev)
	if err != nil {
		slog.ErrorContext(ctx, "feishu: marshal event failed", "error", err)
		return fmt.Errorf("feishu: marshal message receive event: %w", err)
	}

	// Extract event ID from the event header. The P2MessageReceiveV1 struct
	// embeds *larkevent.EventV2Base which has a Header field of type
	// *larkevent.EventHeader. The EventHeader has EventID as a string.
	// We access it via the embedded EventV2Base to avoid ambiguity with
	// the EventReq's Header field.
	eventID := ""
	if ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
		eventID = ev.EventV2Base.Header.EventID
	}

	raw := RawEvent{
		EventID:   eventID,
		EventType: "im.message.receive_v1",
		Payload:   payload,
	}

	return rc.enqueueRawEvent(ctx, raw)
}

// handleMessageRecalled bridges the SDK recall event into the same RawEvent
// stream consumed by the adapter normalizer.
func (rc *RealClient) handleMessageRecalled(ctx context.Context, ev *larkim.P2MessageRecalledV1) error {
	if ev == nil {
		return nil
	}

	payload, err := json.Marshal(ev)
	if err != nil {
		slog.ErrorContext(ctx, "feishu: marshal recall event failed", "error", err)
		return fmt.Errorf("feishu: marshal message recalled event: %w", err)
	}

	eventID := ""
	if ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
		eventID = ev.EventV2Base.Header.EventID
	}

	raw := RawEvent{
		EventID:   eventID,
		EventType: "im.message.recalled_v1",
		Payload:   payload,
	}

	return rc.enqueueRawEvent(ctx, raw)
}

func (rc *RealClient) enqueueRawEvent(ctx context.Context, raw RawEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	enqueueCtx, cancel := context.WithTimeout(ctx, rawEventEnqueueTimeout)
	defer cancel()

	rc.eventMu.RLock()
	defer rc.eventMu.RUnlock()
	if rc.closed {
		channelmetrics.M.RecordAdapterDrop("feishu", "closed")
		slog.Warn("feishu: events channel closed, dropping event",
			"event_id", raw.EventID,
			"event_type", raw.EventType,
		)
		return nil
	}

	select {
	case rc.events <- raw:
		return nil
	case <-enqueueCtx.Done():
		err := enqueueCtx.Err()
		channelmetrics.M.RecordAdapterDrop("feishu", "buffer_full")
		slog.WarnContext(ctx, "feishu: events channel enqueue timed out",
			"event_id", raw.EventID,
			"event_type", raw.EventType,
			"error", err,
		)
		return fmt.Errorf("feishu: enqueue raw event %s (%s): %w", raw.EventID, raw.EventType, err)
	}
}

// fetchBotUserID fetches the bot's open_id via the bot info endpoint.
// This serves dual purpose: credential validation and bot id discovery
// for @mention stripping.
func (rc *RealClient) fetchBotUserID(ctx context.Context) (string, error) {
	resp, err := rc.apiClient.Get(ctx,
		"https://open.feishu.cn/open-apis/bot/v3/info",
		nil,
		larkcore.AccessTokenTypeTenant,
	)
	if err != nil {
		return "", fmt.Errorf("fetch bot info: %w", err)
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return "", fmt.Errorf("parse bot info: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("bot info api: code=%d msg=%s", result.Code, result.Msg)
	}
	if result.Bot.OpenID == "" {
		return "", fmt.Errorf("bot info: open_id is empty")
	}

	return result.Bot.OpenID, nil
}

// Compile-time assertion: RealClient satisfies Client.
var _ Client = (*RealClient)(nil)
