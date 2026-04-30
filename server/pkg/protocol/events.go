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
	EventChatMessage     = "chat:message"
	EventChatDone        = "chat:done"
	EventChatSessionRead = "chat:session_read"

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

	// Daemon events
	EventDaemonHeartbeat     = "daemon:heartbeat"
	EventDaemonHeartbeatAck  = "daemon:heartbeat_ack"
	EventDaemonRegister      = "daemon:register"
	EventDaemonTaskAvailable = "daemon:task_available"
)
