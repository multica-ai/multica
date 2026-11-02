package channel

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Notifier sends a message the platform did not prompt: the brief push is the
// first one. A Channel replies inside a conversation the member started; a
// Notifier addresses the member by their binding, so it needs no open session.
//
// Each platform adapter implements it with its own send API and loads the
// installation credentials itself from binding.InstallationID.
type Notifier interface {
	Send(ctx context.Context, binding Binding, msg Message) error
}

// Binding is the link between one Multica member and one platform user, through
// one installation. ChannelUserID is the platform's address for the member: for
// Telegram it is the user id, which is also the id of their private chat with
// the bot.
type Binding struct {
	ID             pgtype.UUID
	WorkspaceID    pgtype.UUID
	MulticaUserID  pgtype.UUID
	InstallationID pgtype.UUID
	ChannelType    Type
	ChannelUserID  string
}

// Message is platform-neutral text with optional action buttons. Each adapter
// renders the buttons with its own construct (a Telegram inline keyboard, for
// example) and rejects a button that sets neither or both of URL and
// CallbackData.
type Message struct {
	Text    string
	Buttons []Button
}

// Button opens a link (URL) or sends a callback to the server (CallbackData).
// Exactly one of the two is set.
type Button struct {
	Label        string
	URL          string
	CallbackData string
}

// DeliveryError marks a failed send for the retry logic. A Permanent error
// means the platform will not accept this message to this recipient however
// long we wait (the member blocked the bot, the chat is gone). RetryAfter is the
// backoff the platform asked for, when it asked for one. Any other error is
// retryable.
type DeliveryError struct {
	Err        error
	Permanent  bool
	RetryAfter time.Duration
}

func (e *DeliveryError) Error() string { return e.Err.Error() }

func (e *DeliveryError) Unwrap() error { return e.Err }

// PermanentDeliveryError wraps err as a failure that retrying cannot fix.
func PermanentDeliveryError(err error) error {
	return &DeliveryError{Err: err, Permanent: true}
}

// IsPermanentDeliveryError reports whether err is a permanent delivery failure.
func IsPermanentDeliveryError(err error) bool {
	var de *DeliveryError
	return errors.As(err, &de) && de.Permanent
}

// DeliveryRetryAfter returns the platform's requested backoff, or zero.
func DeliveryRetryAfter(err error) time.Duration {
	var de *DeliveryError
	if errors.As(err, &de) {
		return de.RetryAfter
	}
	return 0
}
