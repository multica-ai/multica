package notify

import "testing"

// envFn returns a getenv-shaped function backed by a map.
func envFn(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestFromEnv_EmptyConfig(t *testing.T) {
	// No env vars → bridge has zero channels. Sending then goes
	// straight to fallback comment, which is the documented behavior
	// for unconfigured deployments.
	b := FromEnv(envFn(nil), nil, nil)
	if len(b.channels) != 0 {
		t.Fatalf("expected 0 channels with empty env, got %d", len(b.channels))
	}
}

func TestFromEnv_SlackOnly(t *testing.T) {
	b := FromEnv(envFn(map[string]string{
		"MULTICA_CASCADE_SLACK_WEBHOOK_URL": "https://hooks.slack.com/x/y/z",
	}), nil, nil)
	if len(b.channels) != 1 || b.channels[0].Name() != "slack" {
		t.Fatalf("expected 1 slack channel, got %d (%v)", len(b.channels), names(b.channels))
	}
}

func TestFromEnv_TelegramOnly(t *testing.T) {
	b := FromEnv(envFn(map[string]string{
		"MULTICA_CASCADE_TELEGRAM_BOT_TOKEN": "bot",
		"MULTICA_CASCADE_TELEGRAM_CHAT_ID":   "chat",
	}), nil, nil)
	if len(b.channels) != 1 || b.channels[0].Name() != "telegram" {
		t.Fatalf("expected 1 telegram channel, got %d (%v)", len(b.channels), names(b.channels))
	}
}

func TestFromEnv_TelegramRequiresBothTokenAndChat(t *testing.T) {
	// Token without chat id (or vice versa) is incomplete config —
	// skip the channel rather than failing loud, so a half-configured
	// deployment still notifies via whatever IS configured.
	b := FromEnv(envFn(map[string]string{
		"MULTICA_CASCADE_TELEGRAM_BOT_TOKEN": "bot",
		// chat id missing
	}), nil, nil)
	if len(b.channels) != 0 {
		t.Fatalf("expected telegram to be skipped, got %d channels", len(b.channels))
	}
}

func TestFromEnv_Both(t *testing.T) {
	b := FromEnv(envFn(map[string]string{
		"MULTICA_CASCADE_SLACK_WEBHOOK_URL":  "https://hooks.slack.com/x/y/z",
		"MULTICA_CASCADE_TELEGRAM_BOT_TOKEN": "bot",
		"MULTICA_CASCADE_TELEGRAM_CHAT_ID":   "chat",
	}), nil, nil)
	if len(b.channels) != 2 {
		t.Fatalf("expected 2 channels, got %d (%v)", len(b.channels), names(b.channels))
	}
}

func names(chs []Channel) []string {
	out := make([]string, 0, len(chs))
	for _, c := range chs {
		out = append(out, c.Name())
	}
	return out
}
