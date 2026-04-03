package channel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// SlackEvent represents the outer envelope of a Slack Events API payload.
type SlackEvent struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	Event     json.RawMessage `json:"event"`
}

// SlackMessageEvent represents a message event from Slack.
type SlackMessageEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	ChannelID string `json:"channel"`
	ThreadTS  string `json:"thread_ts"`
	Text      string `json:"text"`
	User      string `json:"user"`
	BotID     string `json:"bot_id"`
	TS        string `json:"ts"`
}

// InboundMessage is the parsed result of a webhook event that represents
// a user response in a channel thread.
type InboundMessage struct {
	ChannelID  string
	ThreadRef  string
	Text       string
	SenderRef  string
	ExternalID string
}

// ParseSlackWebhook reads and validates a Slack Events API request.
// Returns the event type, and for message events, the parsed inbound message.
// For url_verification, returns the challenge string.
func ParseSlackWebhook(r *http.Request, signingSecret string) (eventType string, msg *InboundMessage, challenge string, err error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		return "", nil, "", fmt.Errorf("read body: %w", err)
	}

	if signingSecret != "" {
		if err := verifySlackSignature(r, body, signingSecret); err != nil {
			return "", nil, "", err
		}
	}

	var envelope SlackEvent
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", nil, "", fmt.Errorf("decode envelope: %w", err)
	}

	switch envelope.Type {
	case "url_verification":
		return "url_verification", nil, envelope.Challenge, nil

	case "event_callback":
		var event SlackMessageEvent
		if err := json.Unmarshal(envelope.Event, &event); err != nil {
			return "event_callback", nil, "", nil // unknown event, skip
		}

		// Only process thread replies from real users (not bots).
		if event.Type != "message" || event.Subtype != "" || event.BotID != "" || event.ThreadTS == "" {
			return "event_callback", nil, "", nil
		}

		return "event_callback", &InboundMessage{
			ChannelID:  event.ChannelID,
			ThreadRef:  event.ThreadTS,
			Text:       event.Text,
			SenderRef:  event.User,
			ExternalID: event.TS,
		}, "", nil

	default:
		return envelope.Type, nil, "", nil
	}
}

func verifySlackSignature(r *http.Request, body []byte, signingSecret string) error {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	signature := r.Header.Get("X-Slack-Signature")

	if timestamp == "" || signature == "" {
		return fmt.Errorf("missing slack signature headers")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return fmt.Errorf("slack request too old")
	}

	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("invalid slack signature")
	}

	return nil
}
