package agent

import (
	"context"
	"time"
)

// MemoryHubCredential is the non-secret-safe transport wrapper for the derived
// MemoryHub credential. The wrapped value is the only plaintext secret in the
// MemoryHub design (Plan v1.4 V4-3.2): it exists only in this field, the
// no-store redeem response, and daemon memory.
//
// The type deliberately has NO String method, NO MarshalJSON method, and NO
// logging method, so a stray fmt/log/JSON call cannot serialize the value.
// It is carried in ExecOptions.MemoryHubCredential (a non-JSON field) and is
// never merged into McpConfig, TaskAgentData, persistent agent env, or an
// execenv sidecar file.
type MemoryHubCredential struct {
	// Value is the secret material (an auth token or Bearer-eligible value).
	Value string
	// Placement selects the child-environment injection shape.
	Placement string // "anthropic_auth_token" | "mcp_authorization_env"
	// BaseURL is the non-secret provider base URL carried alongside the
	// secret in the child environment (ANTHROPIC_BASE_URL or its
	// equivalent). It is not secret and may be logged.
	BaseURL string
}

// Valid reports whether the credential is populated and well-formed.
func (c MemoryHubCredential) Valid() bool {
	return c.Value != "" && (c.Placement == "anthropic_auth_token" || c.Placement == "mcp_authorization_env")
}

// RuntimeAdapter is the unified 7-method interface every provider adapter
// implements (Plan v1.4 section 13.2; the earlier "five-method interface" is
// void). ALL-16 owns this interface definition, its type constraints, and the
// core assembly; the per-provider adapter implementations and real-Run
// verification belong to ALL-18.
//
// Each adapter is bound to one runtime (claude, codebuddy, codex, kimi,
// hermes, openclaw in v1).
type RuntimeAdapter interface {
	// Dispatch starts a run for the task bound to the given identity. It
	// returns a run handle the caller uses with Poll/Collect.
	Dispatch(ctx context.Context, req DispatchRequest) (RunHandle, error)

	// Poll reports whether the run has produced terminal output and whether
	// it has failed. It must not block for the full run lifetime.
	Poll(ctx context.Context, handle RunHandle) (PollStatus, error)

	// Collect gathers the run's persisted evidence (output, messages, usage,
	// artifacts, tests) once it is complete.
	Collect(ctx context.Context, handle RunHandle) (CollectResult, error)

	// Cancel requests cancellation of an in-flight run. Managed cleanup is
	// reversible and idempotent.
	Cancel(ctx context.Context, handle RunHandle) error

	// Health probes the runtime's liveness and returns a typed health sample.
	Health(ctx context.Context) (HealthSample, error)

	// Bind validates and stores the remote binding for this runtime's
	// workspace. It returns the resolved remote identity reference.
	Bind(ctx context.Context, req BindRequest) (BindResult, error)

	// Budget returns the runtime's current budget posture (remaining turns,
	// cost, or a degradation signal).
	Budget(ctx context.Context, handle RunHandle) (BudgetStatus, error)
}

// DispatchRequest carries the typed inputs a Dispatch needs.
type DispatchRequest struct {
	// ExecutionIdentity is the immutable frozen execution snapshot.
	ExecutionIdentity any
	// MemoryAttachment carries the already-selected docket refs; the adapter
	// never reselects, merges, drops, or reorders them.
	MemoryAttachment any
	// Credential is the ephemeral derived credential for the child env.
	Credential MemoryHubCredential
	// Prompt is the runtime brief text.
	Prompt string
	// Timeout bounds the whole run.
	Timeout time.Duration
}

// RunHandle identifies one in-flight run for the polling/collection cycle.
type RunHandle struct {
	ID      string
	Runtime string
	TaskID  string
}

// PollStatus reports whether a run reached terminal state.
type PollStatus struct {
	Done    bool
	Failed  bool
	Failure string // typed failure code when Failed
}

// CollectResult carries the run's persisted evidence categories.
type CollectResult struct {
	Output    string
	Messages  []string
	Usage     map[string]TokenUsage
	Artifacts []string
	Tests     []string
}

// HealthSample is one runtime health probe result.
type HealthSample struct {
	OK        bool
	LatencyMS int64
	ErrorCode string
}

// BindRequest carries the data a Bind needs.
type BindRequest struct {
	WorkspaceID string
	ScopeKind   string
	ScopeID     string
	SubjectType string
	SubjectID   string
	Name        string
}

// BindResult is the resolved remote identity for a binding.
type BindResult struct {
	RemoteTeamID  string
	RemoteAgentID string
	RemoteTaskID  string
	RemoteName    string
}

// BudgetStatus reports the runtime budget posture.
type BudgetStatus struct {
	RemainingTurns int
	Degraded       bool
	Reason         string
}
