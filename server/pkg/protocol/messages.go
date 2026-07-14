package protocol

import "encoding/json"

const (
	DaemonCapabilitySkillBundlesV1      = "skill-bundles-v1"
	DaemonCapabilityCoalescedCommentsV1 = "coalesced-comments-v1"
	// DaemonCapabilityRPCV1 advertises that the daemon can carry
	// request/response RPCs over the WebSocket control connection (MUL-4257).
	// Gated so only daemons+servers that both support it route claim over WS;
	// everyone else keeps using the HTTP claim endpoint.
	DaemonCapabilityRPCV1 = "rpc-v1"
)

// RPCRequestPayload is the generic daemon→server request envelope carried in a
// protocol.Message of type EventDaemonRPCRequest. RequestID correlates the
// response; Method selects the server-side handler (e.g. "tasks.claim"); Body
// is the method-specific request JSON.
type RPCRequestPayload struct {
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Body      json.RawMessage `json:"body,omitempty"`
	// TimeoutMs is the server-side execution budget in milliseconds. The server
	// bounds the handler's context by it so a slow RPC is cancelled (its work
	// rolled back) rather than committing after the daemon has already timed
	// out waiting and fallen back to HTTP (MUL-4257). 0 means no server-side
	// bound (connection-lifetime only).
	TimeoutMs int64 `json:"timeout_ms,omitempty"`
}

// RPCResponsePayload is the server→daemon reply, carried in a
// protocol.Message of type EventDaemonRPCResponse. RequestID echoes the
// request. Status mirrors an HTTP status so the daemon can treat WS and HTTP
// outcomes uniformly. Exactly one of Body / Error is meaningful: Body on
// success (2xx), Error on failure.
type RPCResponsePayload struct {
	RequestID string          `json:"request_id"`
	Status    int             `json:"status"`
	Body      json.RawMessage `json:"body,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// Message is the envelope for all WebSocket messages.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// TaskDispatchPayload is sent from server to daemon when a task is assigned.
type TaskDispatchPayload struct {
	TaskID      string `json:"task_id"`
	IssueID     string `json:"issue_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// TaskAvailablePayload is sent from server to daemon as a wakeup hint. The
// daemon still claims work through the existing HTTP claim endpoint.
type TaskAvailablePayload struct {
	RuntimeID string `json:"runtime_id"`
	TaskID    string `json:"task_id,omitempty"`
}

// RuntimeProfilesChangedPayload is sent from server to daemon as a wakeup hint
// when a workspace custom runtime profile is created, edited, disabled, or
// deleted. The daemon still fetches profiles and registers runtimes through the
// existing HTTP endpoints.
type RuntimeProfilesChangedPayload struct {
	WorkspaceID      string `json:"workspace_id"`
	RuntimeProfileID string `json:"runtime_profile_id,omitempty"`
}

// WorkspacesChangedPayload is an account-scoped hint that asks a daemon to
// reconcile its workspace membership set. The server remains authoritative;
// no workspace data is embedded in the event.
type WorkspacesChangedPayload struct{}

// TaskProgressPayload is sent from daemon to server during task execution.
type TaskProgressPayload struct {
	TaskID  string `json:"task_id"`
	Summary string `json:"summary"`
	Step    int    `json:"step,omitempty"`
	Total   int    `json:"total,omitempty"`
}

// TaskCompletedPayload is sent from daemon to server when a task finishes.
type TaskCompletedPayload struct {
	TaskID string `json:"task_id"`
	PRURL  string `json:"pr_url,omitempty"`
	Output string `json:"output,omitempty"`
}

// TaskMessagePayload represents a single agent execution message (tool call, text, etc.)
type TaskMessagePayload struct {
	TaskID    string         `json:"task_id"`
	IssueID   string         `json:"issue_id,omitempty"`
	Seq       int            `json:"seq"`
	Type      string         `json:"type"`              // "text", "tool_use", "tool_result", "error"
	Tool      string         `json:"tool,omitempty"`    // tool name for tool_use/tool_result
	Content   string         `json:"content,omitempty"` // text content
	Input     map[string]any `json:"input,omitempty"`   // tool input (tool_use only)
	Output    string         `json:"output,omitempty"`  // tool output (tool_result only)
	CreatedAt string         `json:"created_at,omitempty"`
}

// DaemonRegisterPayload is sent from daemon to server on connection.
type DaemonRegisterPayload struct {
	DaemonID string        `json:"daemon_id"`
	AgentID  string        `json:"agent_id"`
	Runtimes []RuntimeInfo `json:"runtimes"`
}

// RuntimeInfo describes an available agent runtime on the daemon's machine.
type RuntimeInfo struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

// ChatMessagePayload is broadcast when a new chat message is created.
type ChatMessagePayload struct {
	ChatSessionID string `json:"chat_session_id"`
	MessageID     string `json:"message_id"`
	Role          string `json:"role"`
	Content       string `json:"content"`
	TaskID        string `json:"task_id,omitempty"`
	CreatedAt     string `json:"created_at"`
}

// Chat message kinds (chat_message.message_kind). Additive: unknown values
// degrade to ChatMessageKindMessage on older readers.
const (
	// ChatMessageKindMessage is an ordinary user/assistant message.
	ChatMessageKindMessage = "message"
	// ChatMessageKindNoResponse marks a direct-chat turn the agent completed
	// without any text reply — a visible, deliberate terminal outcome rather
	// than a silently-dropped turn (MUL-4351).
	ChatMessageKindNoResponse = "no_response"
)

// ChatDonePayload is broadcast when an agent finishes responding to a chat
// message. Carries the freshly-persisted assistant ChatMessage so the client
// can write it into the messages cache inline — avoids a refetch round-trip
// during the live-timeline → AssistantMessage handoff that previously caused
// a visible flicker (#2123).
//
// MessageKind is additive (MUL-4351): older clients ignore it and fall back to
// the non-empty Content the server always sends, so a no_response turn still
// renders a real bubble instead of an empty one. Because direct-chat completion
// now always writes exactly one assistant row (message or no_response),
// MessageID/Content/CreatedAt/ElapsedMs are always populated for direct chat —
// the omitempty tags only elide fields for the legacy paths that broadcast
// without a row.
type ChatDonePayload struct {
	ChatSessionID string `json:"chat_session_id"`
	TaskID        string `json:"task_id"`
	MessageID     string `json:"message_id,omitempty"`
	Content       string `json:"content,omitempty"`
	ElapsedMs     int64  `json:"elapsed_ms,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	MessageKind   string `json:"message_kind,omitempty"`
}

// ChatSessionReadPayload is broadcast when the creator marks a session as read.
// Fires to other devices so their unread counts stay in sync.
type ChatSessionReadPayload struct {
	ChatSessionID string `json:"chat_session_id"`
}

// ChatSessionDeletedPayload is broadcast when a chat session is hard-deleted
// so other tabs/devices drop it from their session lists and reset the active
// pointer if it referenced the deleted session.
type ChatSessionDeletedPayload struct {
	ChatSessionID string `json:"chat_session_id"`
}

// ChatSessionUpdatedPayload is broadcast when a user-editable field on a
// chat session changes (today: title via inline rename). Other tabs/devices
// patch the session row in their cached list so the dropdown stays in sync
// without a full refetch.
type ChatSessionUpdatedPayload struct {
	ChatSessionID string `json:"chat_session_id"`
	Title         string `json:"title"`
	// Pinned is set only by the pin/unpin path; nil on a plain rename so a
	// receiver leaves the existing pin state untouched.
	Pinned *bool `json:"pinned,omitempty"`
	// Status is set only by the archive/unarchive path ("active"/"archived");
	// nil on rename/pin so a receiver leaves the existing status untouched.
	Status    *string `json:"status,omitempty"`
	UpdatedAt string  `json:"updated_at"`
}

// DaemonHeartbeatRequestPayload is sent from daemon to server over WebSocket
// to update last_seen_at and pull pending actions for a single runtime.
// Mirrors the body of POST /api/daemon/heartbeat so both transports share
// identical semantics.
type DaemonHeartbeatRequestPayload struct {
	RuntimeID           string `json:"runtime_id"`
	SupportsBatchImport bool   `json:"supports_batch_import,omitempty"`
}

// DaemonHeartbeatAckPayload is the server's reply to DaemonHeartbeatRequestPayload.
// JSON shape mirrors the HTTP heartbeat response so daemon code can decode either.
//
// RuntimeGone is the WebSocket replacement for the HTTP 404 "runtime not found"
// response. When the server discovers the runtime row was deleted (UI delete,
// 7-day offline GC), it sends back an ack with Status=HeartbeatStatusRuntimeGone
// and RuntimeGone=true rather than tearing down the connection with an error.
// The daemon reads this signal, prunes the stale runtime from its local state
// and re-registers; without it the dead UUID would keep heartbeating until the
// daemon process restarts.
type DaemonHeartbeatAckPayload struct {
	RuntimeID               string                                  `json:"runtime_id"`
	Status                  string                                  `json:"status"`
	RuntimeGone             bool                                    `json:"runtime_gone,omitempty"`
	PendingUpdate           *DaemonHeartbeatPendingUpdate           `json:"pending_update,omitempty"`
	PendingModelList        *DaemonHeartbeatPendingModelList        `json:"pending_model_list,omitempty"`
	PendingLocalSkills      *DaemonHeartbeatPendingLocalSkills      `json:"pending_local_skills,omitempty"`
	PendingLocalSkillImport *DaemonHeartbeatPendingLocalSkillImport `json:"pending_local_skill_import,omitempty"`
	// PendingLocalSkillImports carries multiple import requests in a single
	// heartbeat so the daemon can process them concurrently. Old daemons
	// that don't know this field silently ignore it (standard JSON behavior)
	// and fall back to the singular PendingLocalSkillImport above.
	PendingLocalSkillImports []DaemonHeartbeatPendingLocalSkillImport `json:"pending_local_skill_imports,omitempty"`
}

// HeartbeatStatusRuntimeGone is the ack Status used when the runtime row no
// longer exists server-side. Companion to DaemonHeartbeatAckPayload.RuntimeGone.
const HeartbeatStatusRuntimeGone = "runtime_gone"

// DaemonHeartbeatPendingUpdate describes a CLI-update action the daemon
// should run for the runtime.
type DaemonHeartbeatPendingUpdate struct {
	ID            string `json:"id"`
	TargetVersion string `json:"target_version"`
}

// DaemonHeartbeatPendingModelList describes a request for the daemon to
// enumerate the runtime's supported models.
type DaemonHeartbeatPendingModelList struct {
	ID string `json:"id"`
}

// DaemonHeartbeatPendingLocalSkills describes a request for the runtime's
// local-skill inventory.
type DaemonHeartbeatPendingLocalSkills struct {
	ID string `json:"id"`
}

// DaemonHeartbeatPendingLocalSkillImport describes a request to import a
// specific runtime local skill.
type DaemonHeartbeatPendingLocalSkillImport struct {
	ID       string `json:"id"`
	SkillKey string `json:"skill_key"`
}
