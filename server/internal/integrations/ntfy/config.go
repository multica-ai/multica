package ntfy

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTimeout       = 3 * time.Second
	defaultQueueCapacity = 64
	minTopicLength       = 32
	maxTopicLength       = 64
	minTopicUniqueChars  = 8
)

var topicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Config contains the deployment-wide ntfy mirror configuration. A single
// high-entropy topic is scoped to one Multica member so a shared deployment
// cannot copy other members' notifications to the operator's phone.
type Config struct {
	BaseURL       string
	Topic         string
	Token         string
	RecipientID   string
	AppURL        string
	Timeout       time.Duration
	QueueCapacity int
}

// ConfigFromEnv returns nil when the mirror is disabled. Enabling it is an
// explicit opt-in and requires every privacy-sensitive routing value.
func ConfigFromEnv() (*Config, error) {
	rawEnabled := strings.TrimSpace(os.Getenv("NTFY_ENABLED"))
	if rawEnabled == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return nil, fmt.Errorf("NTFY_ENABLED must be a boolean")
	}
	if !enabled {
		return nil, nil
	}

	baseURL, err := httpsURL("NTFY_BASE_URL", os.Getenv("NTFY_BASE_URL"), true, true)
	if err != nil {
		return nil, err
	}

	topic := strings.TrimSpace(os.Getenv("NTFY_TOPIC"))
	if topic == "" {
		return nil, fmt.Errorf("NTFY_TOPIC is required when NTFY_ENABLED=true")
	}
	if len(topic) < minTopicLength || len(topic) > maxTopicLength ||
		!topicPattern.MatchString(topic) || uniqueCharacters(topic) < minTopicUniqueChars {
		return nil, fmt.Errorf("NTFY_TOPIC must be a random %d-%d character value using letters, digits, underscore, or hyphen", minTopicLength, maxTopicLength)
	}

	recipientID := strings.TrimSpace(os.Getenv("NTFY_RECIPIENT_ID"))
	if recipientID == "" {
		return nil, fmt.Errorf("NTFY_RECIPIENT_ID is required when NTFY_ENABLED=true")
	}
	recipientUUID, err := uuid.Parse(recipientID)
	if err != nil {
		return nil, fmt.Errorf("NTFY_RECIPIENT_ID must be a UUID")
	}
	recipientID = recipientUUID.String()

	appURL := ""
	if raw := strings.TrimSpace(os.Getenv("NTFY_APP_URL")); raw != "" {
		appURL, err = httpsURL("NTFY_APP_URL", raw, false, false)
		if err != nil {
			return nil, err
		}
	} else {
		// Existing local deployments commonly expose only an HTTP app URL.
		// That must not disable publishing; it merely cannot be copied into a
		// notification traveling through a public service.
		for _, name := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
			raw := strings.TrimSpace(os.Getenv(name))
			if raw == "" {
				continue
			}
			if candidate, candidateErr := httpsURL(name, raw, false, false); candidateErr == nil {
				appURL = candidate
				break
			}
		}
	}

	timeout := defaultTimeout
	if raw := strings.TrimSpace(os.Getenv("NTFY_TIMEOUT")); raw != "" {
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout < 100*time.Millisecond || timeout > 10*time.Second {
			return nil, fmt.Errorf("NTFY_TIMEOUT must be between 100ms and 10s")
		}
	}

	return &Config{
		BaseURL:       baseURL,
		Topic:         topic,
		Token:         strings.TrimSpace(os.Getenv("NTFY_TOKEN")),
		RecipientID:   recipientID,
		AppURL:        appURL,
		Timeout:       timeout,
		QueueCapacity: defaultQueueCapacity,
	}, nil
}

func uniqueCharacters(value string) int {
	seen := make(map[rune]struct{})
	for _, char := range value {
		seen[char] = struct{}{}
	}
	return len(seen)
}

func httpsURL(name, raw string, required, originOnly bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return "", fmt.Errorf("%s is required when NTFY_ENABLED=true", name)
		}
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s must be an HTTPS URL without credentials, query, or fragment", name)
	}
	if originOnly && u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%s must be an HTTPS origin without a path", name)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}
