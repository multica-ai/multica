package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SendUserMessageParams is one message the bot sends to a user by open_id, in
// their direct message with the bot. MsgType is Lark's msg_type ("text" or
// "interactive"); Content is the JSON-encoded body for that type.
type SendUserMessageParams struct {
	OpenID  string
	MsgType string
	Content string
}

// SendUserMessage posts one message to a user's direct message via
// POST /open-apis/im/v1/messages?receive_id_type=open_id and returns Lark's
// message_id. It goes through the tenant-token path, so a token the cache holds
// is reused and a rejected one is refreshed once. A business error comes back
// as *APIError; a non-2xx reply as *larkAPIStatusError.
func (c *httpAPIClient) SendUserMessage(ctx context.Context, creds InstallationCredentials, p SendUserMessageParams) (string, error) {
	if p.OpenID == "" {
		return "", errors.New("lark http client: missing open_id")
	}
	if p.MsgType == "" || p.Content == "" {
		return "", errors.New("lark http client: missing message type or content")
	}
	body := map[string]string{
		"receive_id": p.OpenID,
		"msg_type":   p.MsgType,
		"content":    p.Content,
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	const path = "/open-apis/im/v1/messages?receive_id_type=open_id"
	if err := c.doAuthedJSON(ctx, creds, http.MethodPost, path, body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: send user message: %w", err)
	}
	if resp.Code != 0 {
		return "", &APIError{Op: "send user message", Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.MessageID, nil
}

// parseRetryAfter reads a Retry-After header in whole seconds. Anything else
// (an HTTP date, a negative or empty value) is zero, so the caller's own backoff
// applies.
func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// jsonBytes marshals v for a message body. The inputs here are fixed shapes, so
// a failure is a programming error and is returned rather than panicking.
func jsonBytes(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
