package notify

import (
	"log/slog"
	"strings"
)

// FromEnv constructs a Bridge wired up from environment variables.
//
//	MULTICA_CASCADE_SLACK_WEBHOOK_URL    — Slack incoming webhook
//	MULTICA_CASCADE_TELEGRAM_BOT_TOKEN   — Telegram bot token
//	MULTICA_CASCADE_TELEGRAM_CHAT_ID     — destination chat id
//
// Channels with missing env vars are silently dropped from the list
// — having only Slack configured is a valid deployment shape, as is
// having neither (in which case every event goes straight to the
// fallback comment).
//
// PR4's worker calls this once at startup. CommentPoster is plumbed
// separately because it depends on multica's own service layer,
// which only exists alongside the worker.
func FromEnv(getenv func(string) string, commentPoster CommentPoster, logger *slog.Logger) *Bridge {
	var channels []Channel

	slackURL := strings.TrimSpace(getenv("MULTICA_CASCADE_SLACK_WEBHOOK_URL"))
	if slackURL != "" {
		channels = append(channels, NewSlackChannel(slackURL, nil))
	}

	tgToken := strings.TrimSpace(getenv("MULTICA_CASCADE_TELEGRAM_BOT_TOKEN"))
	tgChatID := strings.TrimSpace(getenv("MULTICA_CASCADE_TELEGRAM_CHAT_ID"))
	if tgToken != "" && tgChatID != "" {
		channels = append(channels, NewTelegramChannel("", tgToken, tgChatID, nil))
	}

	return New(channels, commentPoster, DefaultRetryDelays, logger)
}
