package notifydebounce

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	t.Run("defaults to immediate delivery", func(t *testing.T) {
		t.Setenv("NOTIFICATION_DELIVERY_DELAY", "")
		config := ConfigFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
		if config.Delay != 0 {
			t.Fatalf("Delay = %s, want 0", config.Delay)
		}
		if config.MaxItems != defaultMaxItemsPerRecipient {
			t.Fatalf("MaxItems = %d, want %d", config.MaxItems, defaultMaxItemsPerRecipient)
		}
		if config.Timeout != defaultDeliveryTimeout {
			t.Fatalf("Timeout = %s, want %s", config.Timeout, defaultDeliveryTimeout)
		}
	})

	t.Run("accepts a Go duration", func(t *testing.T) {
		t.Setenv("NOTIFICATION_DELIVERY_DELAY", "90s")
		config := ConfigFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
		if config.Delay != 90*time.Second {
			t.Fatalf("Delay = %s, want 90s", config.Delay)
		}
	})

	t.Run("invalid and negative values fail open to immediate", func(t *testing.T) {
		for _, raw := range []string{"later", "-1s"} {
			t.Run(raw, func(t *testing.T) {
				t.Setenv("NOTIFICATION_DELIVERY_DELAY", raw)
				config := ConfigFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
				if config.Delay != 0 {
					t.Fatalf("Delay = %s, want 0", config.Delay)
				}
			})
		}
	})
}
