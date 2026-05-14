package outbound

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

const (
	// Backoff schedule: attempt 0 -> 30s, attempt 1 -> 2min, attempt 2+ -> 10min.
	backoff0 = 30 * time.Second
	backoff1 = 2 * time.Minute
	backoff2 = 10 * time.Minute
)

// RetryableError marks transient outbound send failures so the durable outbox
// can schedule a later attempt instead of dead-lettering the notification.
type RetryableError struct{ Inner error }

func (e *RetryableError) Error() string { return e.Inner.Error() }
func (e *RetryableError) Unwrap() error { return e.Inner }

func WrapRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Inner: err}
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var re *RetryableError
	return errors.As(err, &re)
}

type RetryPayload struct {
	Title      string                 `json:"title"`
	Body       string                 `json:"body"`
	TargetType string                 `json:"target_type,omitempty"`
	Mentions   []port.OutboundMention `json:"mentions,omitempty"`
}

type RetrySender interface {
	SendCard(ctx context.Context, connectionID string, target port.OutboundTarget, card RetryPayload) error
}

func backoffForAttempt(attempt int) time.Duration {
	switch attempt {
	case 0:
		return backoff0
	case 1:
		return backoff1
	default:
		return backoff2
	}
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return "<nil>"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}

func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{
		Microseconds: d.Microseconds(),
		Valid:        true,
	}
}
