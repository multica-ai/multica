package notifications

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Enabled        bool
	AppURL         string
	PollInterval   time.Duration
	BatchSize      int32
	MaxAttempts    int32
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:        envBool("LARK_NOTIFICATION_ENABLED"),
		AppURL:         appURLFromEnv(),
		PollInterval:   envDuration("NOTIFICATION_DELIVERY_POLL_INTERVAL", 5*time.Second),
		BatchSize:      envInt32("NOTIFICATION_DELIVERY_BATCH_SIZE", 20),
		MaxAttempts:    envInt32("NOTIFICATION_DELIVERY_MAX_ATTEMPTS", 5),
		InitialBackoff: envDuration("NOTIFICATION_DELIVERY_INITIAL_BACKOFF", 30*time.Second),
		MaxBackoff:     envDuration("NOTIFICATION_DELIVERY_MAX_BACKOFF", 30*time.Minute),
	}
}

func appURLFromEnv() string {
	for _, key := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); v != "" {
			return v
		}
	}
	return "http://localhost:3000"
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

func envInt32(key string, def int32) int32 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return int32(v)
}

func envDuration(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
