package channel

// This file defines the channel-agnostic vocabulary for ON-DEMAND history
// reads. Unlike the inbound push path (InboundMessage), history is PULLED by
// the agent through a single unified CLI (`multica chat history`): the agent
// asks for "the history of the conversation I'm in" and never sees a
// per-platform API. The server resolves the session's binding to a channel
// type and dispatches to that platform's reader, which returns these
// normalized shapes — so adding a platform is "implement a reader", and the
// agent-facing contract never changes (MUL-3871).

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

// HistoryScope selects which slice of a conversation to read. A chat platform
// has two nested histories: the surrounding CHANNEL and the agent's own THREAD
// within it (on Slack the bot's first reply opens a thread on the @mention, so
// every engaged conversation has one). The agent's primary read on a follow-up
// is its thread; the wider channel is pulled only when needed. On the first
// turn there is no thread yet, so the channel is the relevant context.
type HistoryScope string

const (
	// HistoryScopeAuto lets the server pick: the channel on the first turn (no
	// thread exists yet), the thread on follow-ups. This is the default.
	HistoryScopeAuto HistoryScope = "auto"
	// HistoryScopeThread reads the agent's own thread (Slack
	// conversations.replies). Falls back to the channel where the platform /
	// conversation has no threads (e.g. a DM).
	HistoryScopeThread HistoryScope = "thread"
	// HistoryScopeChannel reads the surrounding channel (Slack
	// conversations.history).
	HistoryScopeChannel HistoryScope = "channel"
)

// HistoryMessage is one normalized message from a conversation's history. It is
// the same shape regardless of platform so the agent reads a uniform list,
// exactly like `multica issue comment list --output json`.
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
}

// HistoryPage is one normalized page of history plus a cursor for paging
// further back. Messages are ordered OLDEST-FIRST so the transcript reads
// top-to-bottom like the chat does.
type HistoryPage struct {
	// ChannelType is the platform the history came from ("slack"). Empty when
	// the session is not bound to any channel (a web-only chat session).
	ChannelType string `json:"channel_type,omitempty"`
	// Scope is the scope actually read ("thread" or "channel") after resolving
	// "auto" and any platform fallback (e.g. a DM has no thread). It lets the
	// agent know what it got and decide whether to also pull the other scope.
	Scope HistoryScope `json:"scope,omitempty"`
	// Messages are the fetched messages, oldest-first.
	Messages []HistoryMessage `json:"messages"`
	// NextCursor, when non-empty, is an opaque cursor to pass as Before to
	// page to OLDER messages. Empty means no older messages were available.
	NextCursor string `json:"next_cursor,omitempty"`
}

// HistoryOptions tune a history read. They are platform-neutral; each reader
// maps them onto its own API's paging primitives.
type HistoryOptions struct {
	// Scope selects thread vs channel. The handler resolves "auto" to a
	// concrete scope before calling the reader (it knows whether this is a
	// first turn or a follow-up); the reader still degrades "thread" to channel
	// where the conversation has no thread. An empty value reads the channel.
	Scope HistoryScope
	// Limit caps how many messages to return. A reader clamps it to its
	// platform's per-page maximum and applies a sane default for <= 0.
	Limit int
	// Before is an opaque cursor (a NextCursor from a prior page); the reader
	// returns only messages strictly older than it. Empty starts at the most
	// recent messages.
	Before string
}
