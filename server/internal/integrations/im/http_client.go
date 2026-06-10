package im

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const defaultHTTPTimeout = 30 * time.Second

// HTTPClient calls the Octo bot REST API. All requests authenticate with the
// raw bot token (bf_*) as a Bearer credential — NOT the im_token, which is only
// for the WebSocket handshake.
type HTTPClient struct {
	apiURL   string
	botToken string
	hc       *http.Client
}

// NewHTTPClient creates a REST client. apiURL is the base (e.g. the value from
// register's api_url); trailing slashes are trimmed per request.
func NewHTTPClient(apiURL, botToken string) *HTTPClient {
	return &HTTPClient{
		apiURL:   apiURL,
		botToken: botToken,
		hc:       &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// SetAPIURL updates the base URL (register may return a different api_url).
func (c *HTTPClient) SetAPIURL(apiURL string) {
	if apiURL != "" {
		c.apiURL = apiURL
	}
}

var bearerRe = regexp.MustCompile(`(?i)Bearer\s+\S+`)

// sanitizeError caps an error body and strips any echoed bearer token so it is
// safe to log.
func sanitizeError(body string) string {
	if len(body) > 200 {
		body = body[:200]
	}
	return bearerRe.ReplaceAllString(body, "Bearer ***")
}

// doJSON performs a request and decodes the JSON response into out (which may be
// nil). It uses json.Number so 16+ digit message IDs survive without precision
// loss when decoded into string/Number fields.
func (c *HTTPClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	url := strings.TrimRight(c.apiURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("octo API %s failed (%d): %s", path, resp.StatusCode, sanitizeError(string(respBody)))
	}
	if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(respBody))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("octo API %s returned invalid JSON: %s", path, sanitizeError(string(respBody)))
	}
	return nil
}

// Register calls POST /v1/bot/register to obtain the im_token and ws_url for the
// WebSocket connection. Pass forceRefresh to rotate a stale im_token.
func (c *HTTPClient) Register(ctx context.Context, forceRefresh bool, agentPlatform, agentVersion string) (*BotRegisterResp, error) {
	path := "/v1/bot/register"
	if forceRefresh {
		path += "?force_refresh=true"
	}
	body := map[string]string{}
	if agentPlatform != "" {
		body["agent_platform"] = agentPlatform
	}
	if agentVersion != "" {
		body["agent_version"] = agentVersion
	}
	var out BotRegisterResp
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	if out.RobotID == "" {
		return nil, fmt.Errorf("octo register returned empty robot_id")
	}
	return &out, nil
}

// SendMessageParams describes an outbound message.
type SendMessageParams struct {
	ChannelID   string
	ChannelType ChannelType
	Content     string
	MentionUIDs []string
	MentionAll  bool
	ReplyMsgID  string
	// ClientMsgNo is an idempotency key; generated if empty.
	ClientMsgNo string
}

// SendMessage posts a text message to a channel. Returns the server-assigned
// message id/seq.
func (c *HTTPClient) SendMessage(ctx context.Context, p SendMessageParams) (*SendMessageResult, error) {
	payload := map[string]any{
		"type":    MsgText,
		"content": p.Content,
	}
	if len(p.MentionUIDs) > 0 || p.MentionAll {
		mention := map[string]any{}
		if len(p.MentionUIDs) > 0 {
			mention["uids"] = p.MentionUIDs
		}
		if p.MentionAll {
			mention["all"] = 1
		}
		payload["mention"] = mention
	}
	if p.ReplyMsgID != "" {
		payload["reply"] = map[string]any{"message_id": p.ReplyMsgID}
	}
	clientMsgNo := p.ClientMsgNo
	if clientMsgNo == "" {
		clientMsgNo = uuid.NewString()
	}
	var out SendMessageResult
	err := c.doJSON(ctx, http.MethodPost, "/v1/bot/sendMessage", map[string]any{
		"channel_id":    p.ChannelID,
		"channel_type":  p.ChannelType,
		"payload":       payload,
		"client_msg_no": clientMsgNo,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// EditMessage replaces the content of a previously sent message. Used for
// streaming agent output (send once, then edit repeatedly). content is a
// RichText payload string; the server replaces the message's content blocks.
func (c *HTTPClient) EditMessage(ctx context.Context, channelID string, channelType ChannelType, messageID string, messageSeq uint32, contentEdit string) error {
	body := map[string]any{
		"message_id":   messageID,
		"channel_id":   channelID,
		"channel_type": channelType,
		"content_edit": contentEdit,
	}
	if messageSeq > 0 {
		body["message_seq"] = messageSeq
	}
	return c.doJSON(ctx, http.MethodPost, "/v1/bot/message/edit", body, nil)
}

// SendTyping shows a typing indicator in a channel.
func (c *HTTPClient) SendTyping(ctx context.Context, channelID string, channelType ChannelType) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/bot/typing", map[string]any{
		"channel_id":   channelID,
		"channel_type": channelType,
	}, nil)
}

// SendHeartbeat reports the bot as alive (server caches a 60s TTL).
func (c *HTTPClient) SendHeartbeat(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/bot/heartbeat", map[string]any{}, nil)
}

// GroupMember is a member of a group, used to tell humans from bots when
// deciding whether to respond to a group message.
type GroupMember struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Role int    `json:"role"`
	// Robot is non-zero when this member is a bot.
	Robot int `json:"robot"`
}

// GetGroupMembers lists members of a group.
func (c *HTTPClient) GetGroupMembers(ctx context.Context, groupNo string) ([]GroupMember, error) {
	var out struct {
		Members []GroupMember `json:"members"`
	}
	// groupNo originates from inbound message data (an untrusted boundary), so it
	// is escaped before being interpolated into the path — an unescaped value
	// containing "?", "/", or ".." would otherwise rewrite the request (query
	// injection / path traversal toward other bot endpoints).
	path := "/v1/bot/groups/" + url.PathEscape(groupNo) + "/members"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Members, nil
}
