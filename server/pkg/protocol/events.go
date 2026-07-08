package protocol

// Event types for WebSocket communication between server, web clients, and daemon.
const (
	// Issue events
	EventIssueCreated         = "issue:created"
	EventIssueUpdated         = "issue:updated"
	EventIssueDeleted         = "issue:deleted"
	EventIssueMetadataChanged = "issue_metadata:changed"

	// Comment events
	EventCommentCreated       = "comment:created"
	EventCommentUpdated       = "comment:updated"
	EventCommentDeleted       = "comment:deleted"
	EventCommentResolved      = "comment:resolved"
	EventCommentUnresolved    = "comment:unresolved"
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
	EventTaskQueued                = "task:queued"                  // ∅ → queued (enqueue / retry create)
	EventTaskDispatch              = "task:dispatch"                // queued → dispatched (daemon claim)
	EventTaskRunning               = "task:running"                 // dispatched → running (daemon started)
	EventTaskWaitingLocalDirectory = "task:waiting_local_directory" // dispatched → waiting_local_directory (daemon parked on a busy local_directory path)
	EventTaskProgress              = "task:progress"
	EventTaskCompleted             = "task:completed" // running → completed
	EventTaskFailed                = "task:failed"    // running → failed
	EventTaskMessage               = "task:message"
	EventTaskCancelled             = "task:cancelled" // * → cancelled

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
	EventChatSessionUpdated = "chat:session_updated"

	// Project events
	EventProjectCreated         = "project:created"
	EventProjectUpdated         = "project:updated"
	EventProjectDeleted         = "project:deleted"
	EventProjectResourceCreated = "project_resource:created"
	EventProjectResourceUpdated = "project_resource:updated"
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

	// Squad events
	EventSquadCreated = "squad:created"
	EventSquadUpdated = "squad:updated"
	EventSquadDeleted = "squad:deleted"

	// Daemon events
	EventDaemonHeartbeat              = "daemon:heartbeat"
	EventDaemonHeartbeatAck           = "daemon:heartbeat_ack"
	EventDaemonRegister               = "daemon:register"
	EventDaemonTaskAvailable          = "daemon:task_available"
	EventDaemonRuntimeProfilesChanged = "daemon:runtime_profiles_changed"

	// GitHub integration events
	EventGitHubInstallationCreated = "github_installation:created"
	EventGitHubInstallationDeleted = "github_installation:deleted"
	EventPullRequestLinked         = "pull_request:linked"
	EventPullRequestUpdated        = "pull_request:updated"
	EventPullRequestUnlinked       = "pull_request:unlinked"

	// Lark integration events. `created` covers both first-install
	// (UNIQUE on (workspace_id, agent_id) means at most one row per
	// agent) and re-install via UpsertLarkInstallation — front-ends
	// treat both as a single "installation appeared / refreshed"
	// notification. `revoked` flips status to 'revoked' without
	// deleting the row; the audit trail is preserved.
	EventLarkInstallationCreated = "lark_installation:created"
	EventLarkInstallationRevoked = "lark_installation:revoked"

	// Slack installation lifecycle (MUL-3666). Same semantics as the Lark
	// events: `created` covers both first install and OAuth re-install (the
	// UNIQUE on (workspace_id, agent_id, channel_type) means at most one row
	// per agent), `revoked` flips status without deleting the row. Front-ends
	// invalidate the Slack installations query on either.
	EventSlackInstallationCreated = "slack_installation:created"
	EventSlackInstallationRevoked = "slack_installation:revoked"
)
