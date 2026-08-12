package handler

// MemoryHub activity_log `action` constants (Plan v1.2 section 3 T8 + v1.6
// V6-1.4). Audit write failure is fail-closed for destructive actions.
const (
	MemoryHubActivityKeyUpdated             = "memoryhub_key_updated"
	MemoryHubActivityBindingCreated         = "memoryhub_binding_created"
	MemoryHubActivityBindingReusedCrossScope = "memoryhub_binding_reused_cross_scope"
	MemoryHubActivityBindingUnbound         = "memoryhub_binding_unbound"
	MemoryHubActivityBindingRebound         = "memoryhub_binding_rebound"
	MemoryHubActivityBindingDeletedRemote   = "memoryhub_binding_deleted_remote"
	MemoryHubActivityMemoryWithdrawn        = "memoryhub_memory_withdrawn"
	MemoryHubActivityMemoryPurged           = "memoryhub_memory_purged"
	MemoryHubActivityInjectionFailedRequired = "memoryhub_injection_failed_required"
	MemoryHubActivityInjectionDegraded      = "memoryhub_injection_degraded"
	// MemoryHubActivityReviewRepaired is the V6-1.4 audit action written inside
	// the owner repair transaction. Payload (redacted, exact keys):
	//   schema_version, action, actor_id, actor_type, workspace_id,
	//   execution_id, previous_review_state, previous_review_version,
	//   next_review_state, next_review_version, reviewer_agent_id, occurred_at
	MemoryHubActivityReviewRepaired = "memoryhub_review_repaired"
)
