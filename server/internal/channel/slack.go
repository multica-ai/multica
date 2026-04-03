package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SlackProvider implements Provider for Slack.
type SlackProvider struct{}

// slackConfig is the expected JSON shape of channel.config for Slack.
type slackConfig struct {
	BotToken  string `json:"bot_token"`
	ChannelID string `json:"channel_id"`
}

func parseSlackConfig(raw []byte) (slackConfig, error) {
	var cfg slackConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid slack config: %w", err)
	}
	if cfg.BotToken == "" {
		return cfg, fmt.Errorf("slack config: bot_token is required")
	}
	if cfg.ChannelID == "" {
		return cfg, fmt.Errorf("slack config: channel_id is required")
	}
	return cfg, nil
}

func (s *SlackProvider) SendFirstMessage(ctx context.Context, config []byte, question string, issue IssueContext) (SendResult, error) {
	cfg, err := parseSlackConfig(config)
	if err != nil {
		return SendResult{}, err
	}

	// Build Block Kit message with issue context + question.
	blocks := []map[string]any{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf(":link: *<%s|%s: %s>*\n*Priority:* %s | *Status:* %s",
					issue.URL, issue.Identifier, issue.Title, issue.Priority, issue.Status),
			},
		},
		{"type": "divider"},
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf(":robot_face: *Agent:*\n%s", question),
			},
		},
	}

	body := map[string]any{
		"channel": cfg.ChannelID,
		"blocks":  blocks,
		"text":    fmt.Sprintf("[%s] %s — %s", issue.Identifier, issue.Title, question),
	}

	return postSlackMessage(ctx, cfg.BotToken, body)
}

func (s *SlackProvider) SendMessage(ctx context.Context, config []byte, msg Message) (SendResult, error) {
	cfg, err := parseSlackConfig(config)
	if err != nil {
		return SendResult{}, err
	}

	body := map[string]any{
		"channel":   cfg.ChannelID,
		"thread_ts": msg.ThreadRef,
		"text":      fmt.Sprintf(":robot_face: *Agent:*\n%s", msg.Text),
	}

	return postSlackMessage(ctx, cfg.BotToken, body)
}

func (s *SlackProvider) ValidateConfig(ctx context.Context, config []byte) error {
	cfg, err := parseSlackConfig(config)
	if err != nil {
		return err
	}

	// Call auth.test to verify the bot token.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/auth.test", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack auth.test failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("slack auth.test: decode error: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("slack auth.test: %s", result.Error)
	}
	return nil
}

func (s *SlackProvider) FetchReplies(ctx context.Context, config []byte, threadRef string, afterTS string) ([]Reply, error) {
	cfg, err := parseSlackConfig(config)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://slack.com/api/conversations.replies?channel=%s&ts=%s&oldest=%s&limit=10",
		cfg.ChannelID, threadRef, afterTS)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack conversations.replies: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK       bool `json:"ok"`
		Messages []struct {
			TS    string `json:"ts"`
			Text  string `json:"text"`
			User  string `json:"user"`
			BotID string `json:"bot_id"`
		} `json:"messages"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("slack conversations.replies: decode: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("slack conversations.replies: %s", result.Error)
	}

	var replies []Reply
	for _, m := range result.Messages {
		// Skip bot messages and the thread parent itself.
		if m.BotID != "" || m.TS == threadRef {
			continue
		}
		// Only include messages strictly after afterTS.
		if m.TS <= afterTS {
			continue
		}
		replies = append(replies, Reply{
			ExternalID: m.TS,
			Text:       m.Text,
			SenderRef:  m.User,
		})
	}
	return replies, nil
}

func postSlackMessage(ctx context.Context, botToken string, body map[string]any) (SendResult, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return SendResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/chat.postMessage", bytes.NewReader(data))
	if err != nil {
		return SendResult{}, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("slack chat.postMessage: %w", err)
	}
	defer resp.Body.Close()

	respData, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		TS    string `json:"ts"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return SendResult{}, fmt.Errorf("slack chat.postMessage: decode error: %w", err)
	}
	if !result.OK {
		return SendResult{}, fmt.Errorf("slack chat.postMessage: %s", result.Error)
	}

	threadTS := result.TS
	// If we posted as a reply, thread_ts is already the parent.
	if ts, ok := body["thread_ts"].(string); ok && ts != "" {
		threadTS = ts
	}

	return SendResult{
		ExternalID: result.TS,
		ThreadRef:  threadTS,
	}, nil
}
