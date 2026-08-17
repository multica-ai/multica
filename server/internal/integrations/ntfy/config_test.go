package ntfy

import (
	"strings"
	"testing"
)

func clearNtfyEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"NTFY_ENABLED", "NTFY_BASE_URL", "NTFY_TOPIC", "NTFY_TOKEN",
		"NTFY_RECIPIENT_ID", "NTFY_APP_URL", "NTFY_TIMEOUT",
		"MULTICA_APP_URL", "FRONTEND_ORIGIN",
	} {
		t.Setenv(name, "")
	}
}

func TestConfigFromEnvDisabled(t *testing.T) {
	clearNtfyEnv(t)

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config != nil {
		t.Fatalf("ConfigFromEnv() = %+v, want nil while disabled", config)
	}

	t.Setenv("NTFY_ENABLED", "false")
	config, err = ConfigFromEnv()
	if err != nil || config != nil {
		t.Fatalf("explicitly disabled config = %+v, err = %v; want nil, nil", config, err)
	}
}

func TestConfigFromEnvRequiresCompleteSafeConfiguration(t *testing.T) {
	clearNtfyEnv(t)
	t.Setenv("NTFY_ENABLED", "true")

	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "NTFY_BASE_URL") {
		t.Fatalf("missing base URL error = %v", err)
	}

	t.Setenv("NTFY_BASE_URL", "http://ntfy.example")
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure base URL error = %v", err)
	}

	t.Setenv("NTFY_BASE_URL", "https://ntfy.example")
	t.Setenv("NTFY_TOPIC", "guessable-topic")
	_, err := ConfigFromEnv()
	if err == nil || !strings.Contains(err.Error(), "NTFY_TOPIC") {
		t.Fatalf("short topic error = %v", err)
	}
	if strings.Contains(err.Error(), "guessable-topic") {
		t.Fatalf("configuration error leaked the topic: %v", err)
	}

	repetitiveTopic := strings.Repeat("x", 40)
	t.Setenv("NTFY_TOPIC", repetitiveTopic)
	_, err = ConfigFromEnv()
	if err == nil || strings.Contains(err.Error(), repetitiveTopic) {
		t.Fatalf("low-variety topic error = %v", err)
	}
}

func TestConfigFromEnvLoadsValidConfiguration(t *testing.T) {
	clearNtfyEnv(t)
	topic := strings.Repeat("a1b2c3d4", 6)
	t.Setenv("NTFY_ENABLED", "true")
	t.Setenv("NTFY_BASE_URL", "https://ntfy.sh/")
	t.Setenv("NTFY_TOPIC", topic)
	t.Setenv("NTFY_TOKEN", "publisher-token")
	t.Setenv("NTFY_RECIPIENT_ID", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	t.Setenv("MULTICA_APP_URL", "https://multica.example/app/")
	t.Setenv("NTFY_TIMEOUT", "750ms")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config == nil {
		t.Fatal("ConfigFromEnv() returned nil")
	}
	if config.BaseURL != "https://ntfy.sh" {
		t.Fatalf("BaseURL = %q", config.BaseURL)
	}
	if config.Topic != topic || config.Token != "publisher-token" {
		t.Fatalf("topic/token were not loaded")
	}
	if config.RecipientID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("RecipientID was not canonicalized: %q", config.RecipientID)
	}
	if config.AppURL != "https://multica.example/app" {
		t.Fatalf("AppURL = %q", config.AppURL)
	}
	if config.Timeout.String() != "750ms" || config.QueueCapacity != defaultQueueCapacity {
		t.Fatalf("timeout/queue = %s/%d", config.Timeout, config.QueueCapacity)
	}
}

func TestConfigFromEnvDropsInsecureFallbackAppURL(t *testing.T) {
	clearNtfyEnv(t)
	t.Setenv("NTFY_ENABLED", "true")
	t.Setenv("NTFY_BASE_URL", "https://ntfy.example")
	t.Setenv("NTFY_TOPIC", strings.Repeat("a1b2c3d4", 5))
	t.Setenv("NTFY_RECIPIENT_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("MULTICA_APP_URL", "http://multica.internal")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config.AppURL != "" {
		t.Fatalf("insecure fallback AppURL = %q, want empty", config.AppURL)
	}
}

func TestConfigFromEnvRejectsInvalidRecipientAndTimeout(t *testing.T) {
	clearNtfyEnv(t)
	t.Setenv("NTFY_ENABLED", "true")
	t.Setenv("NTFY_BASE_URL", "https://ntfy.example")
	t.Setenv("NTFY_TOPIC", strings.Repeat("a1b2c3d4", 5))
	t.Setenv("NTFY_RECIPIENT_ID", "not-a-uuid")

	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "NTFY_RECIPIENT_ID") {
		t.Fatalf("invalid recipient error = %v", err)
	}

	t.Setenv("NTFY_RECIPIENT_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("NTFY_TIMEOUT", "1m")
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "NTFY_TIMEOUT") {
		t.Fatalf("invalid timeout error = %v", err)
	}
}
