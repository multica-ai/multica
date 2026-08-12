package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// This file is the OUTBOUND send path shared by the EventChatDone subscriber
// (outbound.go) and the OutboundReplier (replier.go). It turns a reply body into
// one or more DingTalk sampleMarkdown messages and posts them with the
// installation's access_token, routing 1:1 vs group to the right robot endpoint.

const (
	// msgKeyMarkdown renders a {title, text} sampleMarkdown card.
	msgKeyMarkdown = "sampleMarkdown"

	// p2p (1:1) proactive send; group send.
	pathSendP2P   = "/v1.0/robot/oToMessages/batchSend"
	pathSendGroup = "/v1.0/robot/groupMessages/send"
)

// sendTarget is the resolved DingTalk destination for a reply. ConversationType
// selects the endpoint; StaffID is the recipient for a 1:1 send; ConversationID
// is the group's openConversationId for a group send.
type sendTarget struct {
	ConversationType string
	ConversationID   string
	StaffID          string
}

// sender posts replies for one installation. The robotCode + credentials come
// from the installation; the shared Client owns the token cache and transport.
type sender struct {
	client    *Client
	robotCode string
	appKey    string
	appSecret string
}

// markdownParam is the msgParam payload for a sampleMarkdown message.
type markdownParam struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// send delivers text to target as one or more sampleMarkdown messages (chunked
// under DingTalk's per-message byte cap). It returns the last message's send
// key. A 401 triggers one token refresh + retry, covering a server-side token
// revocation between cache fill and use.
func (s *sender) send(ctx context.Context, target sendTarget, text string) (string, error) {
	if text == "" {
		return "", nil
	}
	title := markdownTitle(text)
	var lastKey string
	for _, chunk := range chunkMarkdown(text) {
		param, err := json.Marshal(markdownParam{Title: title, Text: chunk})
		if err != nil {
			return "", fmt.Errorf("marshal msgParam: %w", err)
		}
		key, err := s.sendOne(ctx, target, string(param))
		if err != nil {
			return "", err
		}
		lastKey = key
	}
	return lastKey, nil
}

// sendOne posts a single rendered message, refreshing the token once on 401.
func (s *sender) sendOne(ctx context.Context, target sendTarget, msgParam string) (string, error) {
	path, body, err := s.request(target, msgParam)
	if err != nil {
		return "", err
	}
	var resp struct {
		ProcessQueryKey string `json:"processQueryKey"`
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.client.accessToken(ctx, s.appKey, s.appSecret)
		if err != nil {
			return "", fmt.Errorf("access token: %w", err)
		}
		err = s.client.postJSON(ctx, path, token, body, &resp)
		if err == nil {
			return resp.ProcessQueryKey, nil
		}
		if errors.Is(err, errUnauthorized) && attempt == 0 {
			s.client.invalidate(s.appKey)
			continue
		}
		return "", err
	}
	return "", errUnauthorized
}

// request builds the endpoint + body for a target. A 1:1 send needs a recipient
// staff id; a group send needs the group's openConversationId.
func (s *sender) request(target sendTarget, msgParam string) (string, map[string]any, error) {
	if target.ConversationType == convTypeP2P {
		if target.StaffID == "" {
			return "", nil, errors.New("dingtalk: 1:1 send missing recipient staff id")
		}
		return pathSendP2P, map[string]any{
			"robotCode": s.robotCode,
			"userIds":   []string{target.StaffID},
			"msgKey":    msgKeyMarkdown,
			"msgParam":  msgParam,
		}, nil
	}
	if target.ConversationID == "" {
		return "", nil, errors.New("dingtalk: group send missing conversation id")
	}
	return pathSendGroup, map[string]any{
		"robotCode":          s.robotCode,
		"openConversationId": target.ConversationID,
		"msgKey":             msgKeyMarkdown,
		"msgParam":           msgParam,
	}, nil
}
