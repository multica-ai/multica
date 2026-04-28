package channel

import (
	"context"
	"net/http"
)

// Message represents a normalized incoming message from any channel.
type Message struct {
	// Source channel name (e.g. "telegram", "slack").
	Source string
	// ThreadID identifies the conversation thread in the source channel.
	// For Telegram: the message_id of the first message in the thread.
	ThreadID string
	// UserID identifies the sender in the source channel.
	UserID string
	// UserName is the display name of the sender.
	UserName string
	// Text is the message body.
	Text string
	// IsReply indicates this is a reply to a previous bot message.
	IsReply bool
	// ReplyToThreadID is set when IsReply=true, pointing to the thread being continued.
	ReplyToThreadID string
	// URLs extracted from the message.
	URLs []string
}

// Reply represents a response to send back to the channel.
type Reply struct {
	Text      string
	ThreadID  string
	ParseMode string // "Markdown", "HTML", or empty
}

// Channel is the interface that each platform adapter must implement.
type Channel interface {
	// Name returns the channel identifier (e.g. "telegram").
	Name() string
	// WebhookHandler returns an HTTP handler that receives webhook events
	// from the platform and dispatches them via the provided callback.
	WebhookHandler(onMessage func(ctx context.Context, msg Message)) http.Handler
	// Reply sends a response back to the channel.
	Reply(ctx context.Context, reply Reply) error
}
