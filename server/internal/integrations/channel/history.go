package channel

// This file defines the channel-agnostic vocabulary for ON-DEMAND history
// reads. History is PULLED by the agent through two unified CLI commands —
// `multica chat history` (the channel OVERVIEW: top-level messages + thread
// metadata, not thread contents) and `multica chat thread [id]` (one thread's
// messages). The agent never sees a per-platform API: the server resolves the
// session's binding to a channel type and dispatches to that platform's reader,
// which returns these normalized shapes. Adding a platform is "implement a
// reader"; the agent-facing contract never changes (MUL-3871).

// HistoryRole is the normalized author kind of a fetched message, mirroring the
// chat_message.role domain the agent already reasons about.
type HistoryRole string

const (
	// HistoryRoleUser is a human (or a third-party bot, e.g. an alerting bot)
	// message — context the agent should read.
	HistoryRoleUser HistoryRole = "user"
	// HistoryRoleAssistant is one of THIS bot's own prior messages in the
	// conversation.
	HistoryRoleAssistant HistoryRole = "assistant"
)

// HistoryMessage is one normalized message. It is the same shape regardless of
// platform so the agent reads a uniform list, like `multica issue comment list
// --output json`.
type HistoryMessage struct {
	// ID is the platform message identifier (Slack ts, Feishu message_id).
	ID string `json:"id"`
	// Author is a human-readable display label for the sender ("Alice",
	// "Bot", or a positional "User 2" fallback when the name is unresolved).
	Author string `json:"author"`
	// AuthorID is the platform-native sender id, when available. Empty for
	// some platform/bot messages.
	AuthorID string `json:"author_id,omitempty"`
	// Role distinguishes the bot's own turns from everyone else's.
	Role HistoryRole `json:"role"`
	// Text is the message body, flattened to plain text by the adapter.
	Text string `json:"text"`
	// TS is the platform timestamp string, sortable lexicographically within a
	// platform (Slack "1700000000.000100"). It doubles as the paging cursor.
	TS string `json:"ts"`

	// The following are set only on a CHANNEL-OVERVIEW row that heads a thread,
	// so the agent can `multica chat thread <thread_id>` to read its contents.
	// They are absent on a plain message and on thread-read rows.

	// ThreadID is the identifier to pass to `multica chat thread <id>` to read
	// this thread's messages. Set only when this overview row has a thread.
	ThreadID string `json:"thread_id,omitempty"`
	// ReplyCount is how many replies the thread has (0/omitted when none).
	ReplyCount int `json:"reply_count,omitempty"`
	// LatestReply is the platform timestamp of the most recent reply, when known.
	LatestReply string `json:"latest_reply,omitempty"`
}

// HistoryPage is one normalized page. Messages are ordered OLDEST-FIRST so the
// transcript reads top-to-bottom like the chat does.
type HistoryPage struct {
	// ChannelType is the platform the history came from ("slack"). Empty when
	// the session is not bound to any channel (a web-only chat session).
	ChannelType string `json:"channel_type,omitempty"`
	// ThreadID is set on a THREAD read: which thread these messages belong to.
	// Empty on a channel overview.
	ThreadID string `json:"thread_id,omitempty"`
	// Messages are the fetched messages, oldest-first.
	Messages []HistoryMessage `json:"messages"`
	// NextCursor, when non-empty, is an opaque cursor to pass as Before to
	// page to OLDER messages. Empty means no older messages were available.
	NextCursor string `json:"next_cursor,omitempty"`
}

// HistoryOptions tune a read. They are platform-neutral; each reader maps them
// onto its own API's paging primitives.
type HistoryOptions struct {
	// Limit caps how many messages to return. A reader clamps it to its
	// platform's per-page maximum and applies a sane default for <= 0.
	Limit int
	// Before is an opaque cursor (a NextCursor from a prior page); the reader
	// returns only messages strictly older than it. Empty starts at the most
	// recent messages.
	Before string
}
