// Package handler: MemoryHub claim preparation for the daemon claim response
// (T10, Plan v1.4 V4-3). Owner: ALL-16.
//
// For a MemoryHub execution (a task carrying a stamped execution snapshot) the
// claim response carries a refs-only MemoryHubClaimPreparation: the frozen
// ExecutionIdentity, the already-selected MemoryAttachment refs (never raw
// memory content), and the credential handle issued by the broker. Blocked
// preparations never reach a daemon because the claim gate keeps those rows
// queued.
package handler

import (
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// memoryHubPreparationForTask builds the refs-only MemoryHubClaimPreparation
// for a claimed MemoryHub execution. It returns nil when the task has no
// execution snapshot (not a MemoryHub run).
func memoryHubPreparationForTask(task *db.AgentTaskQueue, workspaceID string) *protocol.MemoryHubClaimPreparation {
	if task == nil || !task.ExecutionID.Valid {
		return nil
	}

	now := time.Now().UTC()
	policy := protocol.ReviewPolicy{
		SchemaVersion:  protocol.MemoryHubSchemaVersion,
		Mode:           protocol.ReviewPolicyNone,
		MaxAttempts:    3,
		TimeoutSeconds: 3600,
	}
	if task.ReviewPolicy.Valid {
		policy.Mode = protocol.ReviewPolicyMode(task.ReviewPolicy.String)
	}
	if task.ReviewerAgentID.Valid {
		reviewer := util.UUIDToString(task.ReviewerAgentID)
		policy.ReviewerAgentID = &reviewer
	}

	identity := protocol.ExecutionIdentity{
		SchemaVersion: protocol.MemoryHubSchemaVersion,
		ExecutionID:   util.UUIDToString(task.ExecutionID),
		WorkspaceID:   workspaceID,
		ScopeKind:     "workspace",
		TaskID:        util.UUIDToString(task.ID),
		RunID:         task.MemoryhubRunID.String,
		AgentID:       util.UUIDToString(task.AgentID),
		RuntimeID:     util.UUIDToString(task.RuntimeID),
		IssuedAt:      now.Format(time.RFC3339),
		ExpiresAt:     now.Add(24 * time.Hour).Format(time.RFC3339),
		Scopes:        []string{},
		ReviewPolicy:  policy,
		Lineage: protocol.ExecutionLineage{
			SchemaVersion: protocol.MemoryHubSchemaVersion,
			MergedFrom:    []string{},
		},
	}
	if task.IssueID.Valid {
		issue := util.UUIDToString(task.IssueID)
		identity.IssueID = &issue
	}

	prep := &protocol.MemoryHubClaimPreparation{
		SchemaVersion:     protocol.MemoryHubSchemaVersion,
		State:             string(protocol.ReviewStateNotRequired),
		ExecutionIdentity: identity,
	}

	// Attachment: refs-only, from the stamped memory_attachment_ref.
	if task.MemoryAttachmentRef.Valid {
		ref := task.MemoryAttachmentRef.String
		prep.MemoryAttachment = &protocol.MemoryAttachment{
			SchemaVersion:    protocol.MemoryHubSchemaVersion,
			AttachmentRef:    ref,
			ExecutionID:      identity.ExecutionID,
			RunID:            identity.RunID,
			ScopeKind:        "workspace",
			SubjectType:      "task",
			SubjectID:        identity.TaskID,
			MemoryPolicy:     task.MemoryPolicy,
			PolicyVersion:    "v1",
			SelectedItemRefs: []protocol.MemoryAttachmentItemRef{},
			IssuedAt:         identity.IssuedAt,
		}
	}

	// Credential handle: the broker-issued transport handle is attached at
	// claim time by the handler when a credential was prepared. This file
	// leaves it nil; the claim handler assigns it when available.
	return prep
}
