package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SubscriberResponse is the JSON shape returned for each issue subscriber.
type SubscriberResponse struct {
	IssueID   string `json:"issue_id"`
	UserType  string `json:"user_type"`
	UserID    string `json:"user_id"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

func subscriberToResponse(s db.IssueSubscriber) SubscriberResponse {
	return SubscriberResponse{
		IssueID:   uuidToString(s.IssueID),
		UserType:  s.UserType,
		UserID:    uuidToString(s.UserID),
		Reason:    s.Reason,
		CreatedAt: timestampToString(s.CreatedAt),
	}
}

// ListIssueSubscribers returns all subscribers for an issue.
func (h *Handler) ListIssueSubscribers(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	subscribers, err := h.Queries.ListIssueSubscribers(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscribers")
		return
	}

	resp := make([]SubscriberResponse, len(subscribers))
	for i, s := range subscribers {
		resp[i] = subscriberToResponse(s)
	}

	writeJSON(w, http.StatusOK, resp)
}

// SubscribeToIssue subscribes a user to an issue with reason "manual".
// If request body contains user_id, subscribes that user; otherwise subscribes the caller.
func (h *Handler) SubscribeToIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	// Default target: the caller, derived via resolveActor so an agent caller
	// (X-Agent-ID set) subscribes itself rather than the underlying member.
	callerActorType, callerActorID := h.resolveActor(r, requestUserID(r), workspaceID)
	targetUserType := callerActorType
	targetUserID := callerActorID
	var req struct {
		UserID   *string `json:"user_id"`
		UserType *string `json:"user_type"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	if !h.isWorkspaceEntity(r.Context(), targetUserType, targetUserID, workspaceID) {
		writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
		return
	}

	// Explicit action, so this CLEARS any earlier opt-out tombstone — the user
	// is overriding their own unsubscribe. Rule-driven subscribes deliberately
	// cannot do that (see AddIssueSubscriber).
	err := h.Queries.SubscribeToIssueExplicitly(r.Context(), db.SubscribeToIssueExplicitlyParams{
		IssueID:  issue.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
		Reason:   "manual",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	h.publish(protocol.EventSubscriberAdded, workspaceID, callerActorType, callerActorID, map[string]any{
		"issue_id":  issueID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
		"reason":    "manual",
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

// UnsubscribeFromIssue removes a user's subscription from an issue.
// If request body contains user_id, unsubscribes that user; otherwise unsubscribes the caller.
func (h *Handler) UnsubscribeFromIssue(w http.ResponseWriter, r *http.Request) {
	h.unsubscribeFromIssue(w, r, false)
}

// UnsubscribeFromIssueSubtree leaves this issue AND every descendant, and —
// via the ancestor opt-out check the delegated rule runs — keeps future
// children of this tree from re-subscribing the user (MUL-5483).
//
// This is a SEPARATE ROUTE rather than a flag on /unsubscribe on purpose.
// Frontend staging deploys automatically on merge while backend staging is
// deployed by hand, so a new client routinely talks to an older server. A
// server that predates this feature decodes an unknown "subtree": true into
// nothing, unsubscribes the root only, and answers 200 — the user is told the
// whole tree is muted while every existing and future child keeps notifying.
// An unknown ROUTE 404s instead, so the client fails loudly and the user can
// retry rather than trusting a silent no-op.
func (h *Handler) UnsubscribeFromIssueSubtree(w http.ResponseWriter, r *http.Request) {
	h.unsubscribeFromIssue(w, r, true)
}

func (h *Handler) unsubscribeFromIssue(w http.ResponseWriter, r *http.Request, subtree bool) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	// Default target: the caller, derived via resolveActor so an agent caller
	// (X-Agent-ID set) unsubscribes itself rather than the underlying member.
	callerActorType, callerActorID := h.resolveActor(r, requestUserID(r), workspaceID)
	targetUserType := callerActorType
	targetUserID := callerActorID
	var req struct {
		UserID   *string `json:"user_id"`
		UserType *string `json:"user_type"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	if !h.isWorkspaceEntity(r.Context(), targetUserType, targetUserID, workspaceID) {
		writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
		return
	}

	// Canonicalize before the target reaches either the advisory lock or a
	// broadcast. The lock keys on the UUID value, and clients dedupe on the
	// user_id string, so an uppercase request must not spell either one
	// differently from the delegated rule's canonical form
	// (MUL-5483 review round 8).
	targetUserID = uuidToString(parseUUID(targetUserID))

	// Every issue this call actually left. The subtree variant reports the whole
	// set so each one gets its own broadcast: a client with a CHILD issue open
	// never sees the root's event, and would otherwise keep showing a
	// subscription the server has already retired.
	removed := []string{issueID}
	if subtree {
		// A subtree opt-out is only meaningful if it also covers children the
		// agent files a moment later, so it must not interleave with the
		// delegated rule's eligibility check. Both take the same
		// (workspace, user) lock for the length of their transaction; without
		// it a child created between the tombstone write and the rule's read
		// comes back as an active watcher (MUL-5483 review round 7).
		ids, err := h.unsubscribeSubtreeSerialized(r.Context(), workspaceID, issue.ID, targetUserType, targetUserID)
		if errors.Is(err, errTargetNoLongerMember) {
			writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unsubscribe")
			return
		}
		removed = removed[:0]
		for _, id := range ids {
			removed = append(removed, uuidToString(id))
		}
	} else if err := h.Queries.RemoveIssueSubscriber(r.Context(), db.RemoveIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsubscribe")
		return
	}

	for _, id := range removed {
		h.publish(protocol.EventSubscriberRemoved, workspaceID, callerActorType, callerActorID, map[string]any{
			"issue_id":  id,
			"user_type": targetUserType,
			"user_id":   targetUserID,
		})
	}

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}

// errTargetNoLongerMember reports that the target left the workspace between
// the handler's pre-check and the serialized write, so the caller can answer
// 403 exactly as the pre-check would have.
var errTargetNoLongerMember = errors.New("target user is no longer a workspace member")

// unsubscribeSubtreeSerialized tombstones the tree inside the same
// (workspace, user) serialization boundary the delegated auto-subscribe rule
// uses, and returns every issue it actually retired.
func (h *Handler) unsubscribeSubtreeSerialized(
	ctx context.Context, workspaceID string, rootID pgtype.UUID, userType, userID string,
) ([]pgtype.UUID, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		return nil, err
	}

	// The membership pre-check in the caller ran against its own snapshot,
	// before this transaction existed — it describes the past. A revoke that
	// commits in between clears this user's subscriptions and deletes their
	// member row, and writing a tombstone behind it would leave an opt-out that
	// outlives the membership and is inherited on re-invite. Re-assert it here,
	// holding the row, now that the lock guarantees no revoke can interleave
	// (MUL-5483 review round 8).
	//
	// Members only: an agent target has no member row, and revoke's subscription
	// cleanup is member-scoped, so the outlives-membership problem does not
	// arise for agents.
	if userType == "member" {
		if _, err := qtx.LockActiveMember(ctx, db.LockActiveMemberParams{
			UserID:      parseUUID(userID),
			WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errTargetNoLongerMember
			}
			return nil, err
		}
	}

	ids, err := qtx.UnsubscribeFromIssueSubtree(ctx, db.UnsubscribeFromIssueSubtreeParams{
		ID:       rootID,
		UserType: userType,
		UserID:   parseUUID(userID),
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}
