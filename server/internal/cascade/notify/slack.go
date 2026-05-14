package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackChannel posts cascade events to a Slack incoming-webhook URL.
// The URL is workspace-scoped on the Slack side and treated as a
// shared secret here — store in 1Password (Pulse-env / MULTICA_CASCADE_SLACK_WEBHOOK_URL)
// and inject as env var, never commit.
type SlackChannel struct {
	webhookURL string
	httpClient *http.Client
}

// NewSlackChannel returns a Slack channel that POSTs to the given
// webhook URL. Pass nil http.Client to use a default with a 10-second
// timeout — long enough for slow Slack edges, short enough that a
// stuck request does not block the retry loop forever.
func NewSlackChannel(webhookURL string, client *http.Client) *SlackChannel {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SlackChannel{webhookURL: webhookURL, httpClient: client}
}

// Name implements Channel.
func (s *SlackChannel) Name() string { return "slack" }

// slackPayload mirrors the minimal incoming-webhook contract. We use
// the legacy `text` field plus a single `blocks` element for the
// quoted human context — keeping the shape narrow means the payload
// renders identically in Slack clients regardless of workspace theme
// or app version.
type slackPayload struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks,omitempty"`
}

type slackBlock struct {
	Type string         `json:"type"`
	Text *slackTextNode `json:"text,omitempty"`
}

type slackTextNode struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Send formats the event as a Slack message and posts it. The text
// field carries the headline (mobile clients show only this); the
// blocks section adds the optional human context. Returns a wrapped
// ErrChannelTransient on 5xx for retry observability.
func (s *SlackChannel) Send(ctx context.Context, e Event) error {
	headline := buildSlackHeadline(e)
	payload := slackPayload{Text: headline}
	if e.HumanContext != "" || e.PRURL != "" {
		body := e.HumanContext
		if e.PRURL != "" {
			if body != "" {
				body += "\n"
			}
			body += fmt.Sprintf("<%s|View PR>", e.PRURL)
		}
		payload.Blocks = []slackBlock{
			{
				Type: "section",
				Text: &slackTextNode{Type: "mrkdwn", Text: body},
			},
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Network-level failures (DNS, TCP, TLS) are transient by
		// default — the Bridge retry path covers them.
		return fmt.Errorf("slack: post: %w (%w)", err, ErrChannelTransient)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 500:
		return fmt.Errorf("slack: status %d (%w)", resp.StatusCode, ErrChannelTransient)
	default:
		// 4xx — wrong URL, payload rejected. Retrying will not help.
		return fmt.Errorf("slack: permanent status %d", resp.StatusCode)
	}
}

// buildSlackHeadline produces the text field. Mobile push surfaces
// only this string, so keep it dense and self-contained.
func buildSlackHeadline(e Event) string {
	verb := slackVerbFor(e.Type)
	id := e.IssuePUL
	if id == "" {
		id = e.IssueID
	}
	if id == "" {
		return fmt.Sprintf("%s on cascade", verb)
	}
	if e.PRNumber > 0 {
		return fmt.Sprintf("%s — %s PR #%d", verb, id, e.PRNumber)
	}
	if e.IssueTitle != "" {
		return fmt.Sprintf("%s — %s: %s", verb, id, e.IssueTitle)
	}
	return fmt.Sprintf("%s — %s", verb, id)
}

func slackVerbFor(t EventType) string {
	switch t {
	case EventLoopGuardTripped:
		return "🔁 Loop guard tripped"
	case EventPlanAmended:
		return "✏️ Plan amended mid-cascade"
	case EventPlanCompleted:
		return "✅ Cascade completed"
	case EventStuck24h:
		return "⏱️ Cascade stuck > 24h"
	case EventRebaseConflict:
		return "⚠️ Rebase conflict"
	case EventMissingAssignee:
		return "🛑 Cascade has no assignee"
	default:
		return "Cascade event: " + string(t)
	}
}
