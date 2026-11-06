package notifydebounce

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	defaultMaxItemsPerRecipient = 100
	defaultDeliveryTimeout      = 15 * time.Second
)

// Config controls one process-local inbox delivery buffer.
type Config struct {
	Delay    time.Duration
	MaxItems int
	Timeout  time.Duration
	Logger   *slog.Logger
}

// ConfigFromEnv reads the opt-in delivery delay. Unset means immediate delivery.
func ConfigFromEnv(logger *slog.Logger) Config {
	if logger == nil {
		logger = slog.Default()
	}
	delay := time.Duration(0)
	raw := strings.TrimSpace(os.Getenv("NOTIFICATION_DELIVERY_DELAY"))
	if raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			logger.Warn("invalid NOTIFICATION_DELIVERY_DELAY; using immediate delivery",
				"value", raw)
		} else {
			delay = parsed
		}
	}
	return Config{
		Delay:    delay,
		MaxItems: defaultMaxItemsPerRecipient,
		Timeout:  defaultDeliveryTimeout,
		Logger:   logger,
	}
}
