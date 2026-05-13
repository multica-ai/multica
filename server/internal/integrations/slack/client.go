package slack

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
)

const webhookURLPrefix = "https://hooks.slack.com/services/"

// PostWebhook sends a JSON payload to a Slack incoming webhook URL.
// Returns an error if the URL is not a valid Slack webhook, the request fails,
// or Slack returns a non-2xx status.
func PostWebhook(ctx context.Context, webhookURL, body string) error {
	if !strings.HasPrefix(webhookURL, webhookURLPrefix) {
		return fmt.Errorf("invalid Slack webhook URL: must start with %s", webhookURLPrefix)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}
