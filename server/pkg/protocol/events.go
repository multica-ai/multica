package protocol

// Event types for WebSocket communication between server, web clients, and daemon.
const (
	// Issue events
	EventIssueCreated = "issue:created"
	EventIssueUpdated = "issue:updated"
	EventIssueDeleted = "issue:deleted"

	// Comment events
	EventCommentCreated       = "comment:created"
	EventCommentUpdated       = "comment:updated"
	EventCommentDeleted       = "comment:deleted"
	EventReactionAdded        = "reaction:added"
	EventReactionRemoved      = "reaction:removed"
	EventIssueReactionAdded   = "issue_reaction:added"
	EventIssueReactionRemoved = "issue_reaction:removed"

	// Agent events
	EventAgentStatus   = "agent:status"
	EventAgentCreated  = "agent:created"
	EventAgentArchived = "agent:archived"
	EventAgentRestored = "agent:restored"

	// Task events (server <-> daemon).
	// Each event maps to a status transition on agent_task_queue. Front-end
	// subscribes by `task:` prefix and invalidates the workspace task
	// snapshot, so the granularity here is "what does the user want to see
	// change" — not "every internal status flip".
	EventTaskQueued    = "task:queued"    // ∅ → queued (enqueue / retry create)
	EventTaskDispatch  = "task:dispatch"  // queued → dispatched (daemon claim)
	EventTaskProgress  = "task:progress"
	EventTaskCompleted = "task:completed" // running → completed
	EventTaskFailed    = "task:failed"    // running → failed
	EventTaskMessage   = "task:message"
	EventTaskCancelled = "task:cancelled" // * → cancelled

	// Inbox events
	EventInboxNew           = "inbox:new"
	EventInboxRead          = "inbox:read"
	EventInboxArchived      = "inbox:archived"
	EventInboxBatchRead     = "inbox:batch-read"
	EventInboxBatchArchived = "inbox:batch-archived"

	// Workspace events
	EventWorkspaceUpdated = "workspace:updated"
	EventWorkspaceDeleted = "workspace:deleted"

	// Member events
	EventMemberAdded   = "member:added"
	EventMemberUpdated = "member:updated"
	EventMemberRemoved = "member:removed"

	// Subscriber events
	EventSubscriberAdded   = "subscriber:added"
	EventSubscriberRemoved = "subscriber:removed"

	// Activity events
	EventActivityCreated = "activity:created"

	// Skill events
	EventSkillCreated = "skill:created"
	EventSkillUpdated = "skill:updated"
	EventSkillDeleted = "skill:deleted"

	// Chat events
	EventChatMessage        = "chat:message"
	EventChatDone           = "chat:done"
	EventChatSessionRead    = "chat:session_read"
	EventChatSessionDeleted = "chat:session_deleted"

	// Channel events. Channels is the multi-participant chat surface (distinct
	// from the 1:1 agent Chat above) — see migration 065 and the channels
	// feature spec. Frontend subscribes by `channel:` prefix and invalidates
	// the per-channel message cache.
	EventChannelCreated       = "channel:created"
	EventChannelUpdated       = "channel:updated"
	EventChannelArchived      = "channel:archived"
	EventChannelMessage       = "channel:message"
	EventChannelMemberAdded   = "channel:member_added"
	EventChannelMemberRemoved = "channel:member_removed"
	EventChannelRead          = "channel:read"
	// Phase 5 — author-driven edits / soft-deletes propagate via these
	// per-message events. Distinct from channel:message (the new-message
	// signal) so the frontend can patch the cache surgically rather than
	// invalidate the whole timeline.
	EventChannelMessageUpdated = "channel:message_updated"
	EventChannelMessageDeleted = "channel:message_deleted"
	// Channel-message reactions (Phase 4). Separate from EventReactionAdded
	// (which targets issue comments) and EventIssueReactionAdded (issues)
	// because the payload shape differs — channel reactions carry channel_id
	// + channel_message_id rather than issue_id + comment_id, and grouping
	// the three under one event would force the frontend to discriminate
	// on payload keys.
	EventChannelReactionAdded   = "channel_reaction:added"
	EventChannelReactionRemoved = "channel_reaction:removed"

	// Project events
	EventProjectCreated         = "project:created"
	EventProjectUpdated         = "project:updated"
	EventProjectDeleted         = "project:deleted"
	EventProjectResourceCreated = "project_resource:created"
	EventProjectResourceDeleted = "project_resource:deleted"

	// Memory artifact events — workspace-scoped knowledge primitives
	// (wiki pages, agent notes, runbooks, decision logs).
	// Archive / restore reuse the Updated event so clients can re-render
	// based on the artifact's archived_at field instead of needing a
	// dedicated wire signal.
	EventMemoryArtifactCreated = "memory_artifact:created"
	EventMemoryArtifactUpdated = "memory_artifact:updated"
	EventMemoryArtifactDeleted = "memory_artifact:deleted"

	// Label events
	EventLabelCreated       = "label:created"
	EventLabelUpdated       = "label:updated"
	EventLabelDeleted       = "label:deleted"
	EventIssueLabelsChanged = "issue_labels:changed"

	// Pin events
	EventPinCreated   = "pin:created"
	EventPinDeleted   = "pin:deleted"
	EventPinReordered = "pin:reordered"

	// Invitation events
	EventInvitationCreated  = "invitation:created"
	EventInvitationAccepted = "invitation:accepted"
	EventInvitationDeclined = "invitation:declined"
	EventInvitationRevoked  = "invitation:revoked"

	// Autopilot events
	EventAutopilotCreated  = "autopilot:created"
	EventAutopilotUpdated  = "autopilot:updated"
	EventAutopilotDeleted  = "autopilot:deleted"
	EventAutopilotRunStart = "autopilot:run_start"
	EventAutopilotRunDone  = "autopilot:run_done"

	// Ship Hub events. Phase 1 publishes at the workspace scope; the
	// frontend invalidates the per-project PR/deploy queries on receipt.
	// Phase 2 adds the granular webhook-driven signals — frontends that
	// only care about coarse refreshes can keep listening to
	// pull_request:synced; those that want surgical cache patches subscribe
	// to the per-PR / per-deploy events.
	EventPullRequestSynced       = "pull_request:synced"
	EventPullRequestStateChanged = "pull_request:state_changed"
	EventDeployStarted           = "deploy:started"
	EventDeployProgress          = "deploy:progress"
	EventDeployCompleted         = "deploy:completed"
	// Phase 3 — card-action audit signal. Fired after every chip press
	// regardless of synchronous/async outcome so the frontend can update
	// the card's "recent actions" footer in real time.
	EventCardAction = "ship:card_action"

	// Phase 7a — Release lifecycle signals. created/updated/cancelled
	// mirror the three CRUD verbs the dialog and detail page exercise;
	// later phases (7b/c/d) reuse `:updated` for stage transitions
	// rather than minting a separate event per stage. Payload always
	// carries `release_id`; `created` also carries `project_id` and
	// `stage` so the rail can decide whether to insert; `updated`
	// carries `stage` so the cache can patch in place; `cancelled`
	// carries only `release_id` (the receiver removes from the rail).
	EventReleaseCreated   = "release:created"
	EventReleaseUpdated   = "release:updated"
	EventReleaseCancelled = "release:cancelled"

	// Daemon events
	EventDaemonHeartbeat     = "daemon:heartbeat"
	EventDaemonHeartbeatAck  = "daemon:heartbeat_ack"
	EventDaemonRegister      = "daemon:register"
	EventDaemonTaskAvailable = "daemon:task_available"
)
