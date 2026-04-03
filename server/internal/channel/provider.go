// Package channel defines the provider interface for external messaging channels
// (Slack, Discord, Teams, etc.) and a registry for looking up implementations.
package channel

import "context"

// IssueContext provides issue metadata for the first message in a thread.
type IssueContext struct {
	Title      string
	Identifier string // e.g., "MUL-42"
	Status     string
	Priority   string
	URL        string // link back to the issue in Multica UI
}

// Message represents a message to send in an existing thread.
type Message struct {
	Text      string
	ThreadRef string // existing thread to reply to
}

// SendResult contains the result of sending a message.
type SendResult struct {
	ExternalID string // provider message ID (Slack: message ts)
	ThreadRef  string // thread reference (Slack: thread_ts)
}

// Reply represents an inbound reply fetched from the channel provider.
type Reply struct {
	ExternalID string
	Text       string
	SenderRef  string // user ID in the provider
}

// Provider is the interface all channel implementations must satisfy.
type Provider interface {
	// SendFirstMessage sends the initial message in a new thread, including issue context.
	SendFirstMessage(ctx context.Context, config []byte, question string, issue IssueContext) (SendResult, error)

	// SendMessage sends a follow-up message in an existing thread.
	SendMessage(ctx context.Context, config []byte, msg Message) (SendResult, error)

	// FetchReplies fetches thread replies after the given message timestamp.
	// Only returns non-bot messages. Used as an alternative to webhooks.
	FetchReplies(ctx context.Context, config []byte, threadRef string, afterTS string) ([]Reply, error)

	// ValidateConfig checks that the provider-specific config JSON is valid.
	ValidateConfig(ctx context.Context, config []byte) error
}
