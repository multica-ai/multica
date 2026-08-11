package handler

// MemoryHub authorization evaluator (Plan v1.2 section 7). This is the SINGLE
// evaluator used both for endpoint admission and for the response
// `capabilities` field. 14 operations x 6 actor classes x 3 subject types.
//
// Actor classes:
//   - workspace_owner_admin  (workspace owner or admin)
//   - project_lead           (member lead of the subject's project)
//   - subject_owner          (agent.owner_id / issue creator or member
//     assignee / project lead)
//   - workspace_member
//   - agent_actor
//   - unauthenticated
//
// Squad subjects are rejected (422) before this evaluator runs; they never
// reach any actor branch.
type MemoryHubActorKind int

const (
	MemoryHubActorUnauthenticated MemoryHubActorKind = iota
	MemoryHubActorWorkspaceOwnerAdmin
	MemoryHubActorProjectLead
	MemoryHubActorSubjectOwner
	MemoryHubActorWorkspaceMember
	MemoryHubActorAgent
)

// MemoryHubSubjectType mirrors the binding subject_type enum.
type MemoryHubSubjectType string

const (
	MemoryHubSubjectProject MemoryHubSubjectType = "project"
	MemoryHubSubjectAgent   MemoryHubSubjectType = "agent"
	MemoryHubSubjectIssue   MemoryHubSubjectType = "issue"
)

// MemoryHubOperation is one of the frozen 14 operations.
type MemoryHubOperation string

const (
	OpConfigRead          MemoryHubOperation = "config.read"
	OpConfigWrite         MemoryHubOperation = "config.write"
	OpBindingRead         MemoryHubOperation = "binding.read"
	OpBindingCreate       MemoryHubOperation = "binding.create"
	OpBindingRebind       MemoryHubOperation = "binding.rebind"
	OpBindingSync         MemoryHubOperation = "binding.sync"
	OpBindingRetry        MemoryHubOperation = "binding.retry"
	OpBindingUnbind       MemoryHubOperation = "binding.unbind"
	OpBindingDeleteRemote MemoryHubOperation = "binding.delete_remote"
	OpHealthRead          MemoryHubOperation = "health.read"
	OpCandidateRead       MemoryHubOperation = "candidate.read"
	OpDocketRead          MemoryHubOperation = "docket.read"
	OpDocketWithdraw      MemoryHubOperation = "docket.withdraw"
	OpBindingReuseCrossScope MemoryHubOperation = "binding.reuse_cross_scope"
)

// allMemoryHubOperations is the frozen set of exactly 14 operations.
var allMemoryHubOperations = []MemoryHubOperation{
	OpConfigRead, OpConfigWrite,
	OpBindingRead, OpBindingCreate, OpBindingRebind, OpBindingSync,
	OpBindingRetry, OpBindingUnbind, OpBindingDeleteRemote,
	OpHealthRead, OpCandidateRead, OpDocketRead, OpDocketWithdraw,
	OpBindingReuseCrossScope,
}

// MemoryHubActor describes the resolved actor for authorization.
type MemoryHubActor struct {
	Kind MemoryHubActorKind
	// Projectless marks a subject scope with no project; project_lead is
	// always forbidden for projectless subjects.
	Projectless bool
	// IsSubjectOwner is set when the actor is the subject owner (agent owner,
	// issue creator/assignee, or project lead) of the evaluated subject.
	IsSubjectOwner bool
	// SubjectInWorkspace reports whether the subject exists in the actor's
	// workspace; false yields 404 semantics at the handler, not 403.
	SubjectInWorkspace bool
}

// EvaluateMemoryHubOps computes the permission set for one actor/subject pair.
// A subject_id outside the actor's workspace must be rejected as 404 by the
// caller BEFORE evaluating (SubjectInWorkspace=false); this evaluator then
// denies everything except config/health reads.
func EvaluateMemoryHubOps(actor MemoryHubActor, subjectType MemoryHubSubjectType) map[MemoryHubOperation]bool {
	perm := make(map[MemoryHubOperation]bool, len(allMemoryHubOperations))

	// Unauthenticated gets nothing; the HTTP layer returns 401.
	if actor.Kind == MemoryHubActorUnauthenticated {
		return perm
	}

	// Agent actors get nothing beyond read-only docket/config introspection;
	// they are denied destructive and management operations. Per the matrix,
	// agent_actor is 403 for every management op and 401 for config.
	if actor.Kind == MemoryHubActorAgent {
		perm[OpBindingRead] = actor.IsSubjectOwner
		perm[OpDocketRead] = actor.IsSubjectOwner
		return perm
	}

	switch actor.Kind {
	case MemoryHubActorWorkspaceOwnerAdmin:
		// owner/admin: full matrix, all subject types.
		for _, op := range allMemoryHubOperations {
			perm[op] = true
		}
		// projectless: project_lead row is 403, but owner/admin is unaffected.
		return perm

	case MemoryHubActorProjectLead:
		if actor.Projectless || !actor.SubjectInWorkspace {
			return perm // 403 for projectless / 404 handled upstream
		}
		// project_lead is scoped to the project's subjects only.
		if !actor.IsSubjectOwner {
			return perm
		}
		perm[OpBindingRead] = true
		perm[OpBindingCreate] = true
		perm[OpBindingRebind] = true
		perm[OpBindingSync] = true
		perm[OpBindingRetry] = true
		perm[OpBindingUnbind] = true
		perm[OpHealthRead] = true
		perm[OpCandidateRead] = true
		perm[OpDocketRead] = true
		perm[OpDocketWithdraw] = true
		// delete_remote and reuse_cross_scope are owner/admin only.
		return perm

	case MemoryHubActorSubjectOwner:
		if !actor.SubjectInWorkspace {
			return perm
		}
		perm[OpBindingRead] = true
		perm[OpBindingCreate] = true
		perm[OpBindingRebind] = true
		perm[OpBindingSync] = true
		perm[OpBindingRetry] = true
		perm[OpBindingUnbind] = true
		perm[OpCandidateRead] = true
		perm[OpDocketRead] = true
		perm[OpDocketWithdraw] = true
		return perm

	case MemoryHubActorWorkspaceMember:
		// ordinary member: read-only health only.
		perm[OpHealthRead] = true
		return perm
	}
	return perm
}

// CapabilitiesFromPerms maps the permission set to the five capability
// booleans (Plan v1.2 section 7.2). Keys are frozen.
func CapabilitiesFromPerms(perm map[MemoryHubOperation]bool) MemoryHubCapabilities {
	return MemoryHubCapabilities{
		SchemaVersion:    1,
		CanManage:        perm[OpBindingCreate] && perm[OpBindingRebind],
		CanDeleteRemote:  perm[OpBindingDeleteRemote],
		CanWithdrawMemory: perm[OpDocketWithdraw],
		CanReadDocket:    perm[OpDocketRead],
		CanWriteConfig:   perm[OpConfigWrite],
	}
}

// MemoryHubCapabilities is the response capabilities object. It lives here to
// avoid an import cycle with pkg/protocol; the JSON shape matches the
// protocol type byte-for-byte.
type MemoryHubCapabilities struct {
	SchemaVersion     int  `json:"schema_version"`
	CanManage         bool `json:"can_manage"`
	CanDeleteRemote   bool `json:"can_delete_remote"`
	CanWithdrawMemory bool `json:"can_withdraw_memory"`
	CanReadDocket     bool `json:"can_read_docket"`
	CanWriteConfig    bool `json:"can_write_config"`
}
