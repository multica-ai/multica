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

	// AppCapabilityChatDraftRestoreV1 is advertised (X-Client-Capabilities) by
	// app clients that understand the durable draft-restore recovery path:
	// chat:cancel_finalized as an invalidation hint plus the draft-restores
	// endpoint. Cancelling a started-but-empty chat task defers the
	// empty/non-empty judgment (#5219), so its cancel response carries no
	// synchronous restore — a client without this capability would silently
	// drop the user's prompt, and keeps the legacy synchronous restore instead.
	AppCapabilityChatDraftRestoreV1 = "chat-draft-restore-v1"
)

// ChatQuickAction is a server-validated follow-up attached to one assistant
// reply. Label is the concise chip text; Prompt is the full next user turn.
type ChatQuickAction struct {
	Label   string `json:"label"`
	Prompt  string `json:"prompt"`
	Primary bool   `json:"primary,omitempty"`
}

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

// PendingWorkKind values carried by PendingWorkPayload.Kind. The kind is
// advisory only — the daemon reacts identically to every kind (one immediate
// heartbeat, which claims whatever is queued) — so an unknown value from a
// newer server stays safe on an older daemon.
const (
	PendingWorkKindModelList = "model_list"
)

// PendingWorkPayload is sent from server to daemon as a wakeup hint when a
// heartbeat-carried request is enqueued for a runtime. The daemon responds by
// sending one immediate heartbeat for RuntimeID instead of waiting for its next
// scheduled tick; the request itself is still claimed through the normal
// heartbeat path, so this event carries no work and is safe to lose, duplicate,
// or ignore (MUL-5444).
type PendingWorkPayload struct {
	RuntimeID string `json:"runtime_id"`
	Kind      string `json:"kind,omitempty"`
}

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

// ChatQuickActionsPayload supplements one completed chat turn with the
// sanitized follow-up actions from the daemon's suggestion pass. An empty
// QuickActions list is a meaningful terminal state — it resolves the
// pending skeleton with "no suggestions this turn".
type ChatQuickActionsPayload struct {
	ChatSessionID string            `json:"chat_session_id"`
	TaskID        string            `json:"task_id"`
	MessageID     string            `json:"message_id"`
	QuickActions  []ChatQuickAction `json:"quick_actions"`
	// Failed marks a supplement that resolves the client's refresh spinner
	// because the regeneration FAILED (the provider pass or its delivery), not
	// because it produced new suggestions. QuickActions then carries the turn's
	// unchanged pills; the client shows a "couldn't refresh" notice instead of
	// treating unchanged content as a silent success (MUL-5149). Omitted (false)
	// on the normal success path and for the automatic best-effort pass.
	Failed bool `json:"failed,omitempty"`
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
	// ChatMessageKindOnboardingKickoff is the server-authored, hidden first
	// turn used to start Mika's onboarding conversation. It is persisted so
	// the runtime receives a normal immutable chat input batch. User-facing
	// APIs filter it out; clients also ignore the kind defensively.
	ChatMessageKindOnboardingKickoff = "onboarding_kickoff"
	// ChatMessageKindOnboardingOpening marks the assistant reply produced by
	// the onboarding kickoff. The kickoff row itself never reaches clients, so
	// the opening self-describes: chat renders the starter cards under this
	// kind instead of quick-action chips (MUL-5765).
	ChatMessageKindOnboardingOpening = "onboarding_opening"
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
	ChatSessionID string            `json:"chat_session_id"`
	TaskID        string            `json:"task_id"`
	MessageID     string            `json:"message_id,omitempty"`
	Content       string            `json:"content,omitempty"`
	ElapsedMs     int64             `json:"elapsed_ms,omitempty"`
	CreatedAt     string            `json:"created_at,omitempty"`
	MessageKind   string            `json:"message_kind,omitempty"`
	QuickActions  []ChatQuickAction `json:"quick_actions,omitempty"`
	// QuickActionsPending tells clients a chat:quick_actions supplement will
	// follow for this turn (render a placeholder). Never true when
	// QuickActions is already populated.
	QuickActionsPending bool `json:"quick_actions_pending,omitempty"`
}

// Outcome values carried by ChatCancelFinalizedPayload.
const (
	// ChatCancelOutcomeStopped: the transcript turned out non-empty, so a
	// "Stopped." assistant message was persisted.
	ChatCancelOutcomeStopped = "stopped"
	// ChatCancelOutcomeRestored: the transcript stayed empty, so the
	// triggering user message was deleted and its content should be
	// restored into the composer as a draft.
	ChatCancelOutcomeRestored = "restored"
)

// ChatCancelFinalizedPayload is broadcast when a cancelled chat task's
// deferred finalization settles (#5219). The cancel HTTP response cannot
// carry this outcome — it is only known after the daemon's transcript flush —
// so clients react to this event instead: outcome "stopped" inserts the
// assistant message (MessageID/Content/... describe the new row, shaped like
// ChatDonePayload), outcome "restored" removes the deleted user message from
// caches and prompts the initiator's client to fetch the durable draft
// restore from the creator-authorized endpoint. The restored prompt's content
// and attachments deliberately never ride this workspace-wide broadcast.
type ChatCancelFinalizedPayload struct {
	Outcome       string `json:"outcome"`
	ChatSessionID string `json:"chat_session_id"`
	TaskID        string `json:"task_id"`
	// InitiatorUserID is the human who triggered the cancelled task. Only
	// this user's client needs to fetch the draft restore (the endpoint is
	// creator-authorized regardless); clients treat a missing value as
	// "not me".
	InitiatorUserID string `json:"initiator_user_id,omitempty"`
	MessageID       string `json:"message_id,omitempty"`
	// Content/MessageKind/CreatedAt/ElapsedMs describe the persisted
	// "Stopped." assistant row and are set only for outcome "stopped" —
	// the same exposure surface as chat:done.
	Content     string `json:"content,omitempty"`
	MessageKind string `json:"message_kind,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	ElapsedMs   int64  `json:"elapsed_ms,omitempty"`
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
	// ProjectID is set only by the project-context update path. The double
	// pointer distinguishes an omitted field from an explicit JSON null.
	ProjectID **string `json:"project_id,omitempty"`
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
// ServerCapabilities is explicit server-to-daemon protocol negotiation. A
// daemon must not infer support from its own advertised client capabilities.
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
	ServerCapabilities      []string                                `json:"server_capabilities,omitempty"`
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

// ============================================================================
// MemoryHub wire contract (Plan v1.5 V5-1..V5-5 + v1.6 V6-1, sole active
// public wire source). Owner: ALL-16. These types are shared by the daemon
// claim path (T10) and the /api/memoryhub handler (T7). CredentialHandle and
// CredentialGrant are ephemeral broker transport objects with NO SQL
// representation (V6-3).
// ============================================================================

// MemoryHubSchemaVersion is the frozen wire schema version for every MemoryHub
// object, request, wrapper, and error envelope.
const MemoryHubSchemaVersion = 1

// MemoryHubDaemonCapabilityClaimV1 is the daemon capability that gates
// MemoryHub claim material in AgentTaskResponse.
const MemoryHubDaemonCapabilityClaimV1 = "memoryhub-claim-v1"

// String types use plain Go string for wire fields; the JSON shapes are
// frozen by the canonical fixture bundle under
// server/internal/handler/testdata/memoryhub/wire-v1/fixtures.json.

// MemoryHubConfig is the public config view. user_key is write-only and never
// appears in a response, log, error, event, or evidence record.
type MemoryHubConfig struct {
	SchemaVersion int     `json:"schema_version"`
	HasKey        bool    `json:"has_key"`
	KeyMasked     *string `json:"key_masked"`
	ServiceID     *string `json:"service_id"`
	UpdatedAt     *string `json:"updated_at"`
}

// MemoryHubCapabilities has exactly five capability booleans plus schema
// version; produced only by the single authorization evaluator.
type MemoryHubCapabilities struct {
	SchemaVersion    int  `json:"schema_version"`
	CanManage        bool `json:"can_manage"`
	CanDeleteRemote  bool `json:"can_delete_remote"`
	CanWithdrawMemory bool `json:"can_withdraw_memory"`
	CanReadDocket    bool `json:"can_read_docket"`
	CanWriteConfig   bool `json:"can_write_config"`
}

// RemoteRef carries reference values only; no endpoint or secret material.
type RemoteRef struct {
	SchemaVersion int     `json:"schema_version"`
	TeamID        *string `json:"team_id"`
	AgentID       *string `json:"agent_id"`
	TaskID        *string `json:"task_id"`
	RemoteName    *string `json:"remote_name"`
}

// MemoryHubBindingStatus is the frozen six-state binding enum.
type MemoryHubBindingStatus string

const (
	MemoryHubBindingUnbound      MemoryHubBindingStatus = "unbound"
	MemoryHubBindingBinding      MemoryHubBindingStatus = "binding"
	MemoryHubBindingBound        MemoryHubBindingStatus = "bound"
	MemoryHubBindingSyncFailed   MemoryHubBindingStatus = "sync_failed"
	MemoryHubBindingCompensating MemoryHubBindingStatus = "compensating"
	MemoryHubBindingBlocked      MemoryHubBindingStatus = "blocked"
)

// MemoryHubBinding is the public binding row.
type MemoryHubBinding struct {
	SchemaVersion int                    `json:"schema_version"`
	ID            string                 `json:"id"`
	WorkspaceID   string                 `json:"workspace_id"`
	ScopeKind     string                 `json:"scope_kind"`
	ScopeID       *string                `json:"scope_id"`
	SubjectType   string                 `json:"subject_type"`
	SubjectID     string                 `json:"subject_id"`
	Status        MemoryHubBindingStatus `json:"status"`
	Version       int                    `json:"version"`
	RemoteRef     RemoteRef              `json:"remote_ref"`
	EvidenceRef   *string                `json:"evidence_ref"`
	NextWakeup    *string                `json:"next_wakeup"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
}

// PageInfo is the keyset pagination trailer.
type PageInfo struct {
	SchemaVersion int     `json:"schema_version"`
	NextCursor    *string `json:"next_cursor"`
	HasMore       bool    `json:"has_more"`
}

// MemoryHubServiceHealth is one service health sample.
type MemoryHubServiceHealth struct {
	SchemaVersion int     `json:"schema_version"`
	OK            bool    `json:"ok"`
	LatencyMS     *int    `json:"latency_ms"`
	ErrorCode     *string `json:"error_code"`
	CheckedAt     string  `json:"checked_at"`
}

// HealthServices groups the four health samples.
type HealthServices struct {
	SchemaVersion int                     `json:"schema_version"`
	MemoryCore    MemoryHubServiceHealth  `json:"memory_core"`
	Proxy         MemoryHubServiceHealth  `json:"proxy"`
	Hub           MemoryHubServiceHealth  `json:"hub"`
	Knowledge     MemoryHubServiceHealth  `json:"knowledge"`
}

// MemoryHubCandidateAvailability is the candidate availability enum.
type MemoryHubCandidateAvailability string

const (
	MemoryHubCandidateAvailable MemoryHubCandidateAvailability = "available"
	MemoryHubCandidateBound     MemoryHubCandidateAvailability = "bound"
	MemoryHubCandidateConflict  MemoryHubCandidateAvailability = "conflict"
)

// MemoryHubCandidate is one remote candidate for binding.
type MemoryHubCandidate struct {
	SchemaVersion  int                            `json:"schema_version"`
	Kind           string                         `json:"kind"`
	RemoteID       string                         `json:"remote_id"`
	RemoteName     string                         `json:"remote_name"`
	ParentRemoteID *string                        `json:"parent_remote_id"`
	ScopeKind      string                         `json:"scope_kind"`
	ScopeID        *string                        `json:"scope_id"`
	Availability   MemoryHubCandidateAvailability `json:"availability"`
	EvidenceRef    *string                        `json:"evidence_ref"`
}

// MemoryItemState is the frozen five-state item enum.
type MemoryItemState string

const (
	MemoryItemActive      MemoryItemState = "active"
	MemoryItemWithdrawn   MemoryItemState = "withdrawn"
	MemoryItemExpired     MemoryItemState = "expired"
	MemoryItemSuperseded  MemoryItemState = "superseded"
	MemoryItemPurged      MemoryItemState = "purged"
)

// MemoryItem is one redacted docket item (summary/ref pointers only).
type MemoryItem struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id"`
	DocketID      string         `json:"docket_id"`
	State         MemoryItemState `json:"state"`
	Kind          string         `json:"kind"`
	Summary       string         `json:"summary"`
	SourceRef     string         `json:"source_ref"`
	EvidenceRef   *string        `json:"evidence_ref"`
	Priority      int            `json:"priority"`
	ExpiresAt     *string        `json:"expires_at"`
	WithdrawnAt   *string        `json:"withdrawn_at"`
	CreatedAt     string         `json:"created_at"`
}

// MemoryDocket is the durable docket view.
type MemoryDocket struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	WorkspaceID   string       `json:"workspace_id"`
	ScopeKind     string       `json:"scope_kind"`
	ScopeID       *string      `json:"scope_id"`
	SubjectType   string       `json:"subject_type"`
	SubjectID     string       `json:"subject_id"`
	Policy        string       `json:"policy"`
	Revision      int          `json:"revision"`
	GeneratedAt   string       `json:"generated_at"`
	ExpiresAt     *string      `json:"expires_at"`
	Items         []MemoryItem `json:"items"`
}

// PayloadRef points at an already-authorized Multica record.
type PayloadRef struct {
	SchemaVersion int     `json:"schema_version"`
	Kind          string  `json:"kind"`
	ID            string  `json:"id"`
	URI           *string `json:"uri"`
	SHA256        string  `json:"sha256"`
}

// EvidenceKind is the frozen evidence event kind enum.
type EvidenceKind string

const (
	EvidenceKindOutput      EvidenceKind = "output"
	EvidenceKindMessage     EvidenceKind = "message"
	EvidenceKindUsage       EvidenceKind = "usage"
	EvidenceKindArtifact    EvidenceKind = "artifact"
	EvidenceKindTest        EvidenceKind = "test"
	EvidenceKindReviewer    EvidenceKind = "reviewer"
	EvidenceKindCompletion  EvidenceKind = "completion"
	EvidenceKindGateFailure EvidenceKind = "gate_failure"
)

// EvidenceEvent is one ordered evidence event.
type EvidenceEvent struct {
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"id"`
	ExecutionID   string      `json:"execution_id"`
	RunID         string      `json:"run_id"`
	WorkspaceID   string      `json:"workspace_id"`
	ProjectID     *string     `json:"project_id"`
	AgentID       string      `json:"agent_id"`
	RuntimeID     string      `json:"runtime_id"`
	Model         string      `json:"model"`
	Sequence      int64       `json:"sequence"`
	OccurredAt    string      `json:"occurred_at"`
	Kind          EvidenceKind `json:"kind"`
	PayloadRef    PayloadRef  `json:"payload_ref"`
	SHA256        string      `json:"sha256"`
}

// RuntimeEvidenceState is the frozen runtime evidence state enum.
type RuntimeEvidenceState string

const (
	RuntimeEvidenceCollecting RuntimeEvidenceState = "collecting"
	RuntimeEvidenceComplete   RuntimeEvidenceState = "complete"
	RuntimeEvidenceFailed     RuntimeEvidenceState = "failed"
)

// ReviewPolicyMode is the frozen review policy enum.
type ReviewPolicyMode string

const (
	ReviewPolicyNone        ReviewPolicyMode = "none"
	ReviewPolicyIndependent ReviewPolicyMode = "independent"
)

// ReviewState is the frozen eight-state review enum (V5-7 sole authority).
type ReviewState string

const (
	ReviewStateNotRequired  ReviewState = "not_required"
	ReviewStatePending      ReviewState = "pending"
	ReviewStateDispatching  ReviewState = "dispatching"
	ReviewStateQueued       ReviewState = "queued"
	ReviewStateRunning      ReviewState = "running"
	ReviewStateRecorded     ReviewState = "recorded"
	ReviewStateRetryWait    ReviewState = "retry_wait"
	ReviewStateBlocked      ReviewState = "blocked"
)

// EvidenceRecord is the durable completion/review record (V5-2 + V5-7).
type EvidenceRecord struct {
	SchemaVersion        int                 `json:"schema_version"`
	ExecutionID          string              `json:"execution_id"`
	RuntimeEvidenceState RuntimeEvidenceState `json:"runtime_evidence_state"`
	OutputRef            *PayloadRef         `json:"output_ref"`
	MessageRefs          []PayloadRef        `json:"message_refs"`
	UsageRefs            []PayloadRef        `json:"usage_refs"`
	ArtifactRefs         []PayloadRef        `json:"artifact_refs"`
	TestRefs             []PayloadRef        `json:"test_refs"`
	ReviewPolicy         ReviewPolicyMode    `json:"review_policy"`
	ReviewState          ReviewState         `json:"review_state"`
	ReviewVersion        int                 `json:"review_version"`
	ReviewerAgentID      *string             `json:"reviewer_agent_id"`
	ReviewTaskID         *string             `json:"review_task_id"`
	ReviewOutputRef      *PayloadRef         `json:"review_output_ref"`
	ReviewAttempt        int                 `json:"review_attempt"`
	MaxReviewAttempts    int                 `json:"max_review_attempts"`
	ReviewNextWakeup     *string             `json:"review_next_wakeup"`
	ReviewFailureCode    *string             `json:"review_failure_code"`
	CreatedAt            string              `json:"created_at"`
	UpdatedAt            string              `json:"updated_at"`
}

// EvidenceScore is the six-dimension score and reproducibility snapshot.
type EvidenceScore struct {
	SchemaVersion    int      `json:"schema_version"`
	ExecutionID      string   `json:"execution_id"`
	AlgorithmVersion string   `json:"algorithm_version"`
	InputDigest      string   `json:"input_digest"`
	Availability     int      `json:"availability"`
	Isolation        int      `json:"isolation"`
	Security         int      `json:"security"`
	Recovery         int      `json:"recovery"`
	Performance      int      `json:"performance"`
	Observability    int      `json:"observability"`
	Overall          int      `json:"overall"`
	Eligible         bool     `json:"eligible"`
	ComputedAt       string   `json:"computed_at"`
	EvidenceRefs     []string `json:"evidence_refs"`
}

// MemoryHubErrorDetails carries only non-secret validation facts.
type MemoryHubErrorDetails struct {
	SchemaVersion  int      `json:"schema_version"`
	FieldPaths     []string `json:"field_paths"`
	AllowedValues  []string `json:"allowed_values"`
}

// MemoryHubError is the versioned inner error. Code is an open string to
// clients; the inner schema_version is not optional.
type MemoryHubError struct {
	SchemaVersion int                   `json:"schema_version"`
	Code          string                `json:"code"`
	Message       string                `json:"message"`
	Retryable     bool                  `json:"retryable"`
	EvidenceRef   *string               `json:"evidence_ref"`
	NextWakeup    *string               `json:"next_wakeup"`
	Details       *MemoryHubErrorDetails `json:"details"`
}

// ErrorResponse is every handler-produced non-2xx body.
type ErrorResponse struct {
	SchemaVersion int            `json:"schema_version"`
	Error         MemoryHubError `json:"error"`
}

// ---------------------------------------------------------------------------
// Mutation requests (V5-3). All nullable request properties must be sent
// explicitly as type or null; omission is invalid.
// ---------------------------------------------------------------------------

type ConfigUpdateRequest struct {
	SchemaVersion int     `json:"schema_version"`
	UserKey       string  `json:"user_key"`
	ServiceID     *string `json:"service_id"`
}

type BindingCreateRequest struct {
	SchemaVersion int         `json:"schema_version"`
	ScopeKind     string      `json:"scope_kind"`
	ScopeID       *string     `json:"scope_id"`
	SubjectType   string      `json:"subject_type"`
	SubjectID     string      `json:"subject_id"`
	Mode          string      `json:"mode"`
	Name          *string     `json:"name"`
	RemoteRef     *RemoteRef  `json:"remote_ref"`
	Confirm       bool        `json:"confirm"`
}

type BindingUpdateRequest struct {
	SchemaVersion   int        `json:"schema_version"`
	ExpectedVersion int        `json:"expected_version"`
	Name            *string    `json:"name"`
	RemoteRef       *RemoteRef `json:"remote_ref"`
	Confirm         bool       `json:"confirm"`
}

type BindingActionRequest struct {
	SchemaVersion   int `json:"schema_version"`
	ExpectedVersion int `json:"expected_version"`
}

type DeleteRemoteRequest struct {
	SchemaVersion   int  `json:"schema_version"`
	ExpectedVersion int  `json:"expected_version"`
	Confirm         bool `json:"confirm"`
}

type WithdrawMemoryItemRequest struct {
	SchemaVersion int    `json:"schema_version"`
	Reason        string `json:"reason"`
	ExpectedState string `json:"expected_state"`
}

// ---------------------------------------------------------------------------
// Success wrappers (V5-3).
// ---------------------------------------------------------------------------

type ConfigResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Config        MemoryHubConfig       `json:"config"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type BindingResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Binding       MemoryHubBinding      `json:"binding"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type BindingListResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Bindings      []MemoryHubBinding    `json:"bindings"`
	Page          PageInfo              `json:"page"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type DeleteRemoteResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	DeletedRemote bool                  `json:"deleted_remote"`
	LocalStatus   MemoryHubBindingStatus `json:"local_status"`
	EvidenceRef   *string               `json:"evidence_ref"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type HealthResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Services      HealthServices        `json:"services"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type CandidateListResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Candidates    []MemoryHubCandidate  `json:"candidates"`
	Page          PageInfo              `json:"page"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type MemoryDocketResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Docket        MemoryDocket          `json:"docket"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type MemoryItemResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Item          MemoryItem            `json:"item"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type ExecutionEvidenceResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Record        EvidenceRecord        `json:"record"`
	Events        []EvidenceEvent       `json:"events"`
	Page          PageInfo              `json:"page"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

type EvidenceScoreResponse struct {
	SchemaVersion int                   `json:"schema_version"`
	Score         EvidenceScore         `json:"score"`
	Capabilities  MemoryHubCapabilities `json:"capabilities"`
}

// ---------------------------------------------------------------------------
// Claim objects (v1.4 V4-3 + v1.5 V5-6 credential handle; supersedes earlier
// summaries). ExecutionIdentity is the immutable execution snapshot; it is a
// typed protocol object carried in the daemon claim response.
// ---------------------------------------------------------------------------

// ReviewPolicy is frozen at enqueue.
type ReviewPolicy struct {
	SchemaVersion   int               `json:"schema_version"`
	Mode            ReviewPolicyMode  `json:"mode"`
	ReviewerAgentID *string           `json:"reviewer_agent_id"`
	MaxAttempts     int               `json:"max_attempts"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
}

// ExecutionLineage records retry/rerun/delegation/handoff/merge provenance.
type ExecutionLineage struct {
	SchemaVersion int      `json:"schema_version"`
	RetryOf       *string  `json:"retry_of"`
	RerunOf       *string  `json:"rerun_of"`
	DelegatedFrom *string  `json:"delegated_from"`
	HandoffOf     *string  `json:"handoff_of"`
	MergedFrom    []string `json:"merged_from"`
}

// ExecutionIdentity is the immutable execution snapshot frozen at enqueue.
type ExecutionIdentity struct {
	SchemaVersion       int                `json:"schema_version"`
	ExecutionID         string             `json:"execution_id"`
	WorkspaceID         string             `json:"workspace_id"`
	ProjectID           *string            `json:"project_id"`
	ScopeKind           string             `json:"scope_kind"`
	IssueID             *string            `json:"issue_id"`
	TaskID              string             `json:"task_id"`
	RunID               string             `json:"run_id"`
	AgentID             string             `json:"agent_id"`
	RuntimeID           string             `json:"runtime_id"`
	Model               string             `json:"model"`
	Scopes              []string           `json:"scopes"`
	IssuedAt            string             `json:"issued_at"`
	ExpiresAt           string             `json:"expires_at"`
	CredentialRef       *string            `json:"credential_ref"`
	MemoryAttachmentRef *string            `json:"memory_attachment_ref"`
	ReviewPolicy        ReviewPolicy       `json:"review_policy"`
	Lineage             ExecutionLineage   `json:"lineage"`
}

// MemoryAttachmentItemRef is one already-selected docket item reference.
type MemoryAttachmentItemRef struct {
	SchemaVersion int     `json:"schema_version"`
	ItemID        string  `json:"item_id"`
	Kind          string  `json:"kind"`
	SourceRef     string  `json:"source_ref"`
	EvidenceRef   *string `json:"evidence_ref"`
	SHA256        string  `json:"sha256"`
	ExpiresAt     *string `json:"expires_at"`
	WithdrawnAt   *string `json:"withdrawn_at"`
}

// MemoryAttachment contains only selected refs; never raw memory content.
type MemoryAttachment struct {
	SchemaVersion    int                     `json:"schema_version"`
	AttachmentRef    string                  `json:"attachment_ref"`
	ExecutionID      string                  `json:"execution_id"`
	RunID            string                  `json:"run_id"`
	DocketID         string                  `json:"docket_id"`
	DocketRevision   int                     `json:"docket_revision"`
	ScopeKind        string                  `json:"scope_kind"`
	ScopeID          *string                 `json:"scope_id"`
	SubjectType      string                  `json:"subject_type"`
	SubjectID        string                  `json:"subject_id"`
	MemoryPolicy     string                  `json:"memory_policy"`
	PolicyVersion    string                  `json:"policy_version"`
	SelectedItemRefs []MemoryAttachmentItemRef `json:"selected_item_refs"`
	IssuedAt         string                  `json:"issued_at"`
	ExpiresAt        *string                 `json:"expires_at"`
}

// CredentialHandle is the ephemeral broker transport handle (V5-6 sole
// authority, supersedes the earlier single redeem_path shape). All fields are
// required and none is nullable. V6-3: NO SQL representation.
type CredentialHandle struct {
	SchemaVersion int    `json:"schema_version"`
	HandleID      string `json:"handle_id"`
	Audience      string `json:"audience"`
	TaskID        string `json:"task_id"`
	ExecutionID   string `json:"execution_id"`
	RuntimeID     string `json:"runtime_id"`
	ExpiresAt     string `json:"expires_at"`
	RedeemPath    string `json:"redeem_path"`
	AckPath       string `json:"ack_path"`
	ReleasePath   string `json:"release_path"`
}

// Degradation carries the only permitted optional-degradation facts.
type Degradation struct {
	SchemaVersion int     `json:"schema_version"`
	Code          string  `json:"code"`
	Reason        string  `json:"reason"`
	EvidenceRef   string  `json:"evidence_ref"`
	NextWakeup    string  `json:"next_wakeup"`
}

// MemoryHubClaimPreparation is the only MemoryHub field added to the daemon
// claim response. Blocked preparations never reach a daemon.
type MemoryHubClaimPreparation struct {
	SchemaVersion     int                `json:"schema_version"`
	State             string             `json:"state"`
	ExecutionIdentity ExecutionIdentity  `json:"execution_identity"`
	MemoryAttachment  *MemoryAttachment  `json:"memory_attachment"`
	CredentialHandle  *CredentialHandle  `json:"credential_handle"`
	Degradation       *Degradation       `json:"degradation"`
}

// ---------------------------------------------------------------------------
// Task-scoped credential redeem/ack/release (V5-6 internal wire types).
// These objects have no SQL representation (V6-3).
// ---------------------------------------------------------------------------

// CredentialRedeemRequest redeems a task-bound handle.
type CredentialRedeemRequest struct {
	SchemaVersion int    `json:"schema_version"`
	HandleID      string `json:"handle_id"`
	ExecutionID   string `json:"execution_id"`
	RuntimeID     string `json:"runtime_id"`
	DaemonID      string `json:"daemon_id"`
}

// CredentialGrant is the no-store redeem response body. No field is nullable.
type CredentialGrant struct {
	SchemaVersion int    `json:"schema_version"`
	GrantID       string `json:"grant_id"`
	ExecutionID   string `json:"execution_id"`
	Provider      string `json:"provider"`
	Placement     string `json:"placement"`
	BaseURL       string `json:"base_url"`
	Value         string `json:"value"`
	ExpiresAt     string `json:"expires_at"`
}

type CredentialRedeemResponse struct {
	SchemaVersion int             `json:"schema_version"`
	Grant         CredentialGrant `json:"grant"`
}

type CredentialAckRequest struct {
	SchemaVersion int    `json:"schema_version"`
	HandleID      string `json:"handle_id"`
	GrantID       string `json:"grant_id"`
	ExecutionID   string `json:"execution_id"`
}

type CredentialReleaseRequest struct {
	SchemaVersion int     `json:"schema_version"`
	HandleID      string  `json:"handle_id"`
	GrantID       *string `json:"grant_id"`
	ExecutionID   string  `json:"execution_id"`
	Reason        string  `json:"reason"`
}

// ---------------------------------------------------------------------------
// Review-repair surface (v1.6 V6-1). All properties are required; none is
// nullable. The only externally callable owner repair route is
// POST /api/memoryhub/evidence/{execution_id}/review-repair.
// ---------------------------------------------------------------------------

type ReviewRepairRequest struct {
	SchemaVersion        int    `json:"schema_version"`
	ExpectedReviewVersion int   `json:"expected_review_version"`
	ReviewerAgentID      string `json:"reviewer_agent_id"`
}

type ReviewRepairResponse struct {
	SchemaVersion int            `json:"schema_version"`
	Record        EvidenceRecord `json:"record"`
}
