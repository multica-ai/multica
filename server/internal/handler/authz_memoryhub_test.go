package handler

import "testing"

func TestOwnerAdminFullMatrix(t *testing.T) {
	actor := MemoryHubActor{Kind: MemoryHubActorWorkspaceOwnerAdmin, SubjectInWorkspace: true}
	for _, st := range []MemoryHubSubjectType{MemoryHubSubjectProject, MemoryHubSubjectAgent, MemoryHubSubjectIssue} {
		perm := EvaluateMemoryHubOps(actor, st)
		for _, op := range allMemoryHubOperations {
			if !perm[op] {
				t.Fatalf("owner/admin denied %s for subject %s", op, st)
			}
		}
	}
	if len(allMemoryHubOperations) != 14 {
		t.Fatalf("operation count = %d, want 14", len(allMemoryHubOperations))
	}
}

func TestUnauthenticatedDenied(t *testing.T) {
	perm := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorUnauthenticated}, MemoryHubSubjectIssue)
	for _, op := range allMemoryHubOperations {
		if perm[op] {
			t.Fatalf("unauthenticated allowed %s", op)
		}
	}
}

func TestAgentActorLimited(t *testing.T) {
	// agent actor that is NOT subject owner: read access only when owner.
	perm := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorAgent}, MemoryHubSubjectIssue)
	if perm[OpBindingCreate] || perm[OpConfigWrite] || perm[OpBindingDeleteRemote] {
		t.Fatal("agent actor must not manage bindings/config/delete-remote")
	}
	// agent actor that IS subject owner: read ops allowed.
	owner := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorAgent, IsSubjectOwner: true}, MemoryHubSubjectIssue)
	if !owner[OpBindingRead] || !owner[OpDocketRead] {
		t.Fatal("agent subject owner should read binding/docket")
	}
	if owner[OpBindingCreate] {
		t.Fatal("agent subject owner must not create bindings")
	}
}

func TestMemberOnlyHealthRead(t *testing.T) {
	perm := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorWorkspaceMember}, MemoryHubSubjectIssue)
	if !perm[OpHealthRead] {
		t.Fatal("member should read health")
	}
	for _, op := range allMemoryHubOperations {
		if op != OpHealthRead && perm[op] {
			t.Fatalf("member allowed %s", op)
		}
	}
}

func TestProjectLeadForbiddenOnProjectless(t *testing.T) {
	perm := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorProjectLead, Projectless: true}, MemoryHubSubjectIssue)
	if perm[OpBindingCreate] || perm[OpDocketWithdraw] {
		t.Fatal("project_lead must be 403 on projectless subjects")
	}
}

func TestDeleteRemoteAndReuseAreOwnerAdminOnly(t *testing.T) {
	// subject owner cannot delete remote or reuse cross scope.
	perm := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorSubjectOwner, SubjectInWorkspace: true}, MemoryHubSubjectAgent)
	if perm[OpBindingDeleteRemote] || perm[OpBindingReuseCrossScope] {
		t.Fatal("subject owner must not delete-remote or reuse-cross-scope")
	}
}

func TestSubjectOutsideWorkspaceDenied(t *testing.T) {
	perm := EvaluateMemoryHubOps(MemoryHubActor{Kind: MemoryHubActorSubjectOwner, SubjectInWorkspace: false}, MemoryHubSubjectIssue)
	for _, op := range allMemoryHubOperations {
		if perm[op] {
			t.Fatalf("out-of-workspace subject allowed %s", op)
		}
	}
}

func TestCapabilitiesFromPerms(t *testing.T) {
	perm := map[MemoryHubOperation]bool{
		OpBindingCreate: true, OpBindingRebind: true,
		OpBindingDeleteRemote: false, OpDocketWithdraw: true,
		OpDocketRead: true, OpConfigWrite: false,
	}
	caps := CapabilitiesFromPerms(perm)
	if !caps.CanManage || caps.CanDeleteRemote || !caps.CanWithdrawMemory || !caps.CanReadDocket || caps.CanWriteConfig {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
	if caps.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", caps.SchemaVersion)
	}
}
