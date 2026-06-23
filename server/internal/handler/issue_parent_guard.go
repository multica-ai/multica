package handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Squad-parent ownership + active-dependency guards (UMC-288).
//
// A squad-owned issue keeps its delegation in-place on the squad issue:
// downstream work is routed through the squad leader / tasks / comments, never
// by minting a role-owned child issue as a handoff. The incidents this guards
// against:
//   - UMC-307: a squad-owned parent created a role-owned (agent/member) child
//     as an architecture/review/handoff shortcut.
//   - UMC-251 / UMC-293: a role-owned backlog follow-up under a squad parent
//     with no launched/deferred lifecycle.
//   - UMC-305: a parent recorded an active dependency (`linked_child`) pointing
//     at a ghost child — backlog, unassigned, no run, no comment.
//
// These run server-side in the create/update/link path so an API caller cannot
// bypass them by skipping the CLI. They never auto-correct: on violation the
// caller gets a stable 400 naming the broken invariant.

// Stable validation messages. Kept as constants so tests and API clients can
// assert on a fixed substring instead of brittle inline strings.
const (
	errChildMustBeSquadOwned           = "child of a squad-owned parent must be owned by the same squad as the parent; explicit agent/member or a different squad is not allowed (omit the assignee to inherit the parent squad)"
	errChildUnassignedUnderSquadParent = "child of a squad-owned parent cannot be left unassigned; it must stay owned by the same squad (omit the assignee on create to inherit it, or assign the parent squad explicitly)"
	errLinkedChildMustBeRef            = "active dependency metadata must be an issue id or identifier string"
	errLinkedChildNotFound             = "active dependency metadata does not refer to an issue in this workspace"
	errLinkedChildOwnership            = "active dependency child must be owned by the same squad as this issue"
	errLinkedChildInert                = "active dependency child is inert: it must be LAUNCHED (active or completed run, or a comment) or explicitly DEFERRED (deferred_until / blocked_by / waiting_on / deferred_reason)"
)

// activeDependencyMetadataKeys are the metadata keys that record a parent->child
// active dependency. Writing one must point at a coherent child, not a ghost.
// v1 guards the canonical key only; widening this set is a deliberate decision,
// not an inference from prose.
var activeDependencyMetadataKeys = map[string]bool{
	"linked_child": true,
}

// deferredMetadataKeys are the explicit signals that a child is parked on
// purpose (DEFERRED), so a parent may depend on it without it being active.
var deferredMetadataKeys = []string{"deferred_until", "blocked_by", "waiting_on", "deferred_reason"}

// squadParentChildOwnershipError enforces that a child of a squad-owned parent
// stays owned by the same squad. It returns "" when the (parent, child)
// ownership pair is allowed, or a stable error string when it must be rejected.
//
// Only squad-owned parents constrain their children. A child of a squad-owned
// parent must NOT be left unassigned: an unset child assignee is a ghost surface
// (the parent-child link records an active dependency while the child has no
// owner). Create handles its own default — it inherits the parent squad before
// calling this helper, so create never reaches the unset branch here. Every
// other caller (update / batch reparent, link, explicit unassign) that would
// leave the child unassigned under a squad parent is rejected, so the only way
// to keep such a child is same-squad ownership.
func squadParentChildOwnershipError(parentType pgtype.Text, parentID pgtype.UUID, childType pgtype.Text, childID pgtype.UUID) string {
	if !(parentType.Valid && parentType.String == "squad") {
		return ""
	}
	if !childType.Valid && !childID.Valid {
		return errChildUnassignedUnderSquadParent
	}
	if childType.String != "squad" || childID != parentID {
		return errChildMustBeSquadOwned
	}
	return ""
}

// activeDependencyMetadataError validates an active-dependency metadata write
// (e.g. `linked_child`) before it lands. It returns "" when the write is
// allowed, or a stable error string to reject with — the handler returns 400
// and writes nothing, so prior metadata is preserved.
//
// parent is the issue whose metadata is being set; rawValue is the JSON value
// for key. Ownership is enforced only when the parent is squad-owned; the
// lifecycle (LAUNCHED or DEFERRED, never inert) is always enforced because a
// ghost active dependency is the core UMC-305 failure regardless of owner.
func (h *Handler) activeDependencyMetadataError(ctx context.Context, parent db.Issue, key string, rawValue []byte) string {
	if !activeDependencyMetadataKeys[key] {
		return ""
	}
	var childRef string
	if err := json.Unmarshal(rawValue, &childRef); err != nil || strings.TrimSpace(childRef) == "" {
		return errLinkedChildMustBeRef
	}
	child, ok := h.resolveIssueRef(ctx, childRef, uuidToString(parent.WorkspaceID))
	if !ok {
		return errLinkedChildNotFound
	}
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "squad" {
		if !(child.AssigneeType.Valid && child.AssigneeType.String == "squad" && child.AssigneeID == parent.AssigneeID) {
			return errLinkedChildOwnership
		}
	}
	if h.issueIsLaunched(ctx, child) || issueIsDeferred(child) {
		return ""
	}
	return errLinkedChildInert
}

// issueIsLaunched reports whether a child shows a non-inert execution/readback
// signal: an active or completed agent run, or at least one comment. A ghost
// child (no run, no comment) returns false.
func (h *Handler) issueIsLaunched(ctx context.Context, child db.Issue) bool {
	if tasks, err := h.Queries.ListTasksByIssue(ctx, child.ID); err == nil {
		for _, t := range tasks {
			switch t.Status {
			case "queued", "dispatched", "running", "waiting_local_directory", "completed":
				return true
			}
		}
	}
	if n, err := h.Queries.CountComments(ctx, db.CountCommentsParams{
		IssueID:     child.ID,
		WorkspaceID: child.WorkspaceID,
	}); err == nil && n > 0 {
		return true
	}
	return false
}

// issueIsDeferred reports whether a child is explicitly parked. v1 contract: a
// deferral signal is a non-empty (trimmed) STRING value on one of the deferred
// metadata keys. A bool or number — e.g. waiting_on=false, deferred_reason=0 —
// is NOT a valid deferral; accepting it would let an inert child masquerade as
// DEFERRED through metadata type sniffing, which is the prose-bypass this guard
// must close.
func issueIsDeferred(child db.Issue) bool {
	md := parseIssueMetadata(child.Metadata)
	for _, k := range deferredMetadataKeys {
		if s, isStr := md[k].(string); isStr && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// resolveIssueRef looks up an issue by "PREFIX-NUMBER" identifier or by UUID
// within a workspace, without writing to a response. Used by the active-
// dependency guard to resolve a `linked_child` reference.
func (h *Handler) resolveIssueRef(ctx context.Context, ref, workspaceID string) (db.Issue, bool) {
	if issue, ok := h.resolveIssueByIdentifier(ctx, ref, workspaceID); ok {
		return issue, true
	}
	issueUUID, err := util.ParseUUID(ref)
	if err != nil {
		return db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return db.Issue{}, false
	}
	return issue, true
}
