package lark

import "github.com/jackc/pgx/v5/pgtype"

// This file holds the Feishu adapter's native-ish inbound/outbound value
// types. The WS connector decodes a raw Lark event into an InboundMessage;
// feishuChannel translates that into a channel.InboundMessage for the
// channel-agnostic engine.Router, and the feishu resolvers / OutcomeReplier
// translate back at the adapter boundary. (Formerly defined on dispatcher.go,
// which the MUL-3620 Router cutover removed.)

// InboundMessage is the Feishu connector's decoded, enriched event. It is the
// adapter's internal shape: feishuChannel maps it to a channel.InboundMessage
// (stashing this struct in Raw) so the resolvers can read the platform-
// specific fields the normalized envelope does not carry.
type InboundMessage struct {
	EventType    string
	EventID      string
	AppID        string
	ChatID       ChatID
	ChatType     ChatType
	MessageID    string
	SenderOpenID OpenID
	Body         string
	// ForceFreshSession marks this dispatch as a one-off fresh start: the
	// daemon should skip prior session resume when it claims the resulting
	// chat task.
	ForceFreshSession bool
	AddressedToBot    bool

	// MessageType is the raw Lark msg_type ("text", "post", "merge_forward",
	// "image", "interactive", …). The decoder populates it so the inbound
	// enricher can decide whether a message needs an HTTP round-trip to
	// expand while the core stays msg_type-agnostic and only reads Body.
	MessageType string

	// CreateTime is the trigger message's creation time (epoch milliseconds).
	// The enricher anchors the group recent-context window to it; the typing
	// indicator uses it to skip stale reactions.
	CreateTime string

	// ParentID is the message_id this one quote-replies to (verbatim
	// parent_id); RootID is the thread/root anchor. The enricher expands
	// quoted replies off ParentID.
	ParentID string
	RootID   string

	// ThreadID is the Lark topic (话题) id, populated only for messages posted
	// inside a thread, so a non-empty value signals an in-thread @-mention.
	// Persisted on the chat binding so the outbound patcher threads its reply.
	ThreadID string

	// CommandBody is the user's OWN typed text (the decoded Body before the
	// enricher prepends quoted/forwarded context). `/issue` is parsed from
	// THIS, not the enriched Body.
	CommandBody string
}

// Outcome categorizes what the inbound pipeline decided. The OutcomeReplier
// inspects it to choose the Lark-side reply. Values mirror engine.Outcome.
type Outcome string

const (
	// OutcomeDropped — not ingested (identity, dedup, group filter, …).
	OutcomeDropped Outcome = "dropped"
	// OutcomeNeedsBinding — the open_id is unbound; send the binding card.
	OutcomeNeedsBinding Outcome = "needs_binding"
	// OutcomeIngested — the message landed and a run was (or will be) enqueued.
	OutcomeIngested Outcome = "ingested"
	// OutcomeAgentOffline — landed, but the agent has no runtime bound.
	OutcomeAgentOffline Outcome = "agent_offline"
	// OutcomeAgentArchived — landed, but the agent is archived.
	OutcomeAgentArchived Outcome = "agent_archived"
)

// DispatchResult is the Feishu-side verdict the OutcomeReplier consumes to
// drive its outbound reply. The engine produces an engine.Result; the feishu
// OutboundReplier adapter translates it into this shape.
type DispatchResult struct {
	Outcome        Outcome
	DropReason     DropReason
	InstallationID pgtype.UUID
	ChatSessionID  pgtype.UUID
	SenderOpenID   OpenID
	TaskID         pgtype.UUID
	IssueID        pgtype.UUID
	IssueNumber    int32
	// IssueIdentifier is the workspace-qualified key ("MUL-42") for the
	// created issue, used verbatim in the confirmation message.
	IssueIdentifier string
	// IssueTitle is the title supplied on /issue, echoed in the confirmation.
	IssueTitle string
}
