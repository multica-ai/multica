package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// DingTalk Stream Mode is the platform's outbound long-connection
// transport — the analog of Lark's WS long-conn and Slack's Socket
// Mode, but with a much simpler wire format:
//
//  1. POST {openAPIBase}/v1.0/gateway/connections/open with the app's
//     clientId/clientSecret and the topics to subscribe. The response
//     carries a one-shot {endpoint, ticket} pair.
//  2. Dial "{endpoint}?ticket={ticket}" as a WebSocket. Every frame in
//     both directions is a JSON text message.
//  3. Inbound frames are discriminated by `type`:
//     - SYSTEM  topic "ping"        → reply pong (mirror the data)
//     - SYSTEM  topic "disconnect"  → server asks us to reconnect
//     - CALLBACK topic /v1.0/im/bot/messages/get → a bot message; ACK
//       with code 200 and hand the payload to the inbound pipeline
//     Every frame carries headers.messageId which the ACK must echo.
//
// Shapes are pinned to the official dingtalk-stream-sdk-go payload
// package (DataFrame / DataFrameResponse / ConnectionEndpointRequest);
// we inline them rather than depend on the SDK for the same reason the
// lark package inlines its long-conn client.

const (
	streamGatewayPath = "/v1.0/gateway/connections/open"

	streamFrameTypeSystem   = "SYSTEM"
	streamFrameTypeEvent    = "EVENT"
	streamFrameTypeCallback = "CALLBACK"

	streamTopicPing       = "ping"
	streamTopicDisconnect = "disconnect"
	// streamTopicBotMessage is the unified bot-message callback topic.
	streamTopicBotMessage = "/v1.0/im/bot/messages/get"

	streamHeaderTopic       = "topic"
	streamHeaderMessageID   = "messageId"
	streamHeaderContentType = "contentType"
	streamContentTypeJSON   = "application/json"

	streamAckCodeOK = 200

	// streamUserAgent labels the connection in DingTalk's diagnostics.
	streamUserAgent = "multica/1.0"

	// streamReadIdleTimeout bounds how long the read loop waits without
	// ANY traffic before declaring the link dead. DingTalk's server pings
	// every ~30-60s, so 3 minutes of silence means the connection is
	// gone even if the TCP session has not noticed.
	streamReadIdleTimeout = 3 * time.Minute

	// streamWriteTimeout bounds a single ACK write.
	streamWriteTimeout = 10 * time.Second
)

// streamFrame is the inbound wire shape (SDK payload.DataFrame).
type streamFrame struct {
	SpecVersion string            `json:"specVersion"`
	Type        string            `json:"type"`
	Time        int64             `json:"time"`
	Headers     map[string]string `json:"headers"`
	Data        string            `json:"data"`
}

func (f *streamFrame) topic() string     { return f.Headers[streamHeaderTopic] }
func (f *streamFrame) messageID() string { return f.Headers[streamHeaderMessageID] }

// streamFrameResponse is the outbound ACK shape (SDK payload.DataFrameResponse).
type streamFrameResponse struct {
	Code    int               `json:"code"`
	Headers map[string]string `json:"headers"`
	Message string            `json:"message"`
	Data    string            `json:"data"`
}

func newStreamAck(frame *streamFrame, data string) *streamFrameResponse {
	return &streamFrameResponse{
		Code: streamAckCodeOK,
		Headers: map[string]string{
			streamHeaderContentType: streamContentTypeJSON,
			streamHeaderMessageID:   frame.messageID(),
			streamHeaderTopic:       frame.topic(),
		},
		Message: "OK",
		Data:    data,
	}
}

// streamEndpoint is the gateway's one-shot connection grant.
type streamEndpoint struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

// openStreamEndpoint exchanges the app credentials for a WS endpoint +
// ticket, subscribing to the bot-message callback topic.
func openStreamEndpoint(ctx context.Context, httpClient *http.Client, openAPIBase, clientID, clientSecret string) (streamEndpoint, error) {
	body, err := json.Marshal(map[string]any{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"ua":           streamUserAgent,
		"subscriptions": []map[string]string{
			{"type": streamFrameTypeCallback, "topic": streamTopicBotMessage},
		},
	})
	if err != nil {
		return streamEndpoint{}, fmt.Errorf("dingtalk stream: marshal gateway request: %w", err)
	}
	endpoint := strings.TrimRight(openAPIBase, "/") + streamGatewayPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return streamEndpoint{}, fmt.Errorf("dingtalk stream: new gateway request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return streamEndpoint{}, fmt.Errorf("dingtalk stream: gateway request: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return streamEndpoint{}, &APIError{
			Status:  resp.StatusCode,
			Code:    "stream_gateway_failed",
			Message: strings.TrimSpace(truncate(string(payload), 256)),
		}
	}
	var ep streamEndpoint
	if err := json.Unmarshal(payload, &ep); err != nil {
		return streamEndpoint{}, fmt.Errorf("dingtalk stream: decode gateway response: %w", err)
	}
	if ep.Endpoint == "" || ep.Ticket == "" {
		return streamEndpoint{}, &APIError{Code: "stream_gateway_empty", Message: "gateway response missing endpoint/ticket"}
	}
	return ep, nil
}

// dialStream connects the WebSocket for a granted endpoint. The caller
// owns the returned conn.
func dialStream(ctx context.Context, ep streamEndpoint) (*websocket.Conn, error) {
	wssURL := ep.Endpoint + "?ticket=" + ep.Ticket
	// DefaultDialer honors HTTP(S)_PROXY from the environment, matching
	// the rest of the backend's outbound HTTP behavior.
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wssURL, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dingtalk stream: dial (HTTP %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("dingtalk stream: dial: %w", err)
	}
	return conn, nil
}

// writeStreamResponse sends one ACK frame under the write deadline.
// gorilla/websocket allows at most one concurrent writer; the channel's
// read loop is the only ACK writer, so no extra locking is needed here.
func writeStreamResponse(conn *websocket.Conn, resp *streamFrameResponse) error {
	_ = conn.SetWriteDeadline(time.Now().Add(streamWriteTimeout))
	return conn.WriteJSON(resp)
}
