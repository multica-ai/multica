package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Member limit (MUL-9488).
//
// Cloud caps the members of a workspace on the Free plan through the
// "members" gate. Every path that adds a member checks it:
//
//   - Creating an invitation or joining by share link counts members plus
//     live pending invitations, because each pending invitation already holds
//     a slot it can still turn into a member.
//   - Accepting an invitation counts members only: its own invitation is the
//     slot it holds.
//
// Guests take no slot and are never checked. Paid plans get the gate off and
// keep using purchased seats (seat_capacity.go). Off skips the check without
// reading anything, which is also the self-hosted behavior.
//
// A path checks twice. Before its side effects (Cloud seat calls, rate-limit
// budgets) it counts without a lock, so a full workspace is refused early,
// and re-reads Cloud's policy before refusing. Inside the transaction that
// writes the member or invitation it takes the workspace's admission lock
// first, before any row lock, and counts again; that second count is the one
// that holds when two admissions race for the last slot.

const memberLimitReachedCode = "member_limit_reached"

var errMemberLimitReached = errors.New("workspace has reached its member limit")

// guestRole takes no member slot (MUL-9420).
const guestRole = "guest"

func roleTakesMemberSlot(role string) bool { return role != guestRole }

// memberLimit is one admission's member-limit decision, read before its
// transaction so no Cloud call runs while the admission lock is held.
type memberLimit struct {
	overages    service.EntitlementOverages
	workspaceID pgtype.UUID
	// reserved is set when the member takes the slot its own pending
	// invitation already holds.
	reserved bool
	decision entitlement.Decision
}

// memberLimitFor reads the gate for one admission and refuses it early when
// it is enforced and already over the limit. A refusal is recorded and
// returned as errMemberLimitReached.
func (h *Handler) memberLimitFor(ctx context.Context, workspaceID pgtype.UUID, role string, reserved bool) (memberLimit, error) {
	m := memberLimit{
		overages:    service.EntitlementOverages{Queries: h.Queries},
		workspaceID: workspaceID,
		reserved:    reserved,
		decision:    entitlement.Decision{Gate: entitlement.Gate{Action: entitlement.ActionOff}},
	}
	if h.Entitlements == nil || !workspaceID.Valid || !roleTakesMemberSlot(role) {
		return m, nil
	}
	m.decision = h.Entitlements.Gate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateMembers)
	if !m.active() || m.decision.Gate.Action != entitlement.ActionEnforce {
		return m, nil
	}
	slots, err := h.Queries.CountWorkspaceMemberSlots(ctx, workspaceID)
	if err != nil {
		return m, err
	}
	if !m.exceeds(slots) {
		return m, nil
	}
	// About to refuse: re-read the policy so a workspace that has just
	// upgraded is not refused on a policy cached before the upgrade.
	if refresher, ok := h.Entitlements.(entitlement.Refresher); ok {
		m.decision = refresher.RefreshGate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateMembers)
		if !m.active() || m.decision.Gate.Action != entitlement.ActionEnforce || !m.exceeds(slots) {
			return m, nil
		}
	}
	m.overLimit(slots).record(ctx)
	return m, errMemberLimitReached
}

func (m memberLimit) active() bool {
	action := m.decision.Gate.Action
	return (action == entitlement.ActionObserve || action == entitlement.ActionEnforce) &&
		m.decision.Gate.Limit != nil
}

func (m memberLimit) limit() int64 { return int64(*m.decision.Gate.Limit) }

// attempted is the slot count once this admission is made, counting itself.
func (m memberLimit) attempted(slots db.CountWorkspaceMemberSlotsRow) int64 {
	if m.reserved {
		return slots.Members + 1
	}
	return slots.Members + slots.PendingInvitations + 1
}

func (m memberLimit) exceeds(slots db.CountWorkspaceMemberSlotsRow) bool {
	return m.attempted(slots) > m.limit()
}

// lock serializes the workspace's member admissions until tx ends. Take it
// first in the transaction, before any row lock, so every admission path
// acquires its locks in the same order.
func (m memberLimit) lock(ctx context.Context, q *db.Queries) error {
	if !m.active() {
		return nil
	}
	return q.LockWorkspaceMemberAdmission(ctx, m.workspaceID)
}

// admit counts under the lock taken by lock. Within the limit it returns
// nothing to record. Over it, observe admits and enforce returns
// errMemberLimitReached; either way the returned admission records the
// attempt once the transaction has ended.
func (m memberLimit) admit(ctx context.Context, q *db.Queries) (memberLimitAdmission, error) {
	if !m.active() {
		return memberLimitAdmission{}, nil
	}
	slots, err := q.CountWorkspaceMemberSlots(ctx, m.workspaceID)
	if err != nil {
		return memberLimitAdmission{}, err
	}
	if !m.exceeds(slots) {
		return memberLimitAdmission{}, nil
	}
	admission := m.overLimit(slots)
	if m.decision.Gate.Action == entitlement.ActionEnforce {
		return admission, errMemberLimitReached
	}
	return admission, nil
}

func (m memberLimit) overLimit(slots db.CountWorkspaceMemberSlotsRow) memberLimitAdmission {
	start, end := memberLimitPeriod(time.Now())
	return memberLimitAdmission{overages: m.overages, attempt: &service.LimitExceededAttempt{
		WorkspaceID:    m.workspaceID,
		Gate:           entitlement.GateMembers,
		Action:         m.decision.Gate.Action,
		PeriodStart:    start,
		PeriodEnd:      end,
		Limit:          m.limit(),
		AttemptedUsage: m.attempted(slots),
	}}
}

// memberLimitPeriod groups member-limit attempts by UTC calendar month. The
// gate itself has no period; the month only buckets the over-limit records,
// so rollout reach reads the same way as the monthly autopilot quota.
func memberLimitPeriod(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

// memberLimitAdmission is an over-limit attempt waiting to be recorded.
type memberLimitAdmission struct {
	overages service.EntitlementOverages
	attempt  *service.LimitExceededAttempt
}

// record stores the attempt, if any. Call it after the transaction has
// ended: committed for an observed admission, rolled back for a refusal.
// Recording is best-effort and never changes the outcome.
func (a memberLimitAdmission) record(ctx context.Context) {
	if a.attempt != nil {
		a.overages.Record(ctx, *a.attempt)
	}
}

func writeMemberLimitReached(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusForbidden, memberLimitReachedCode,
		"This workspace has reached its plan's member limit. Upgrade the plan to add more members.")
}

// MemberLimitResponse is the member limit as clients render it. action is off
// when no limit applies, including self-hosted deployments; limit, members and
// pending_invitations are present only while a limit applies. Guests are not
// counted.
type MemberLimitResponse struct {
	Action             string `json:"action"`
	Limit              *int64 `json:"limit,omitempty"`
	Members            *int64 `json:"members,omitempty"`
	PendingInvitations *int64 `json:"pending_invitations,omitempty"`
}

// GetMemberLimit — GET /api/workspaces/{id}/member-limit
//
// Member-readable, like the member and invitation lists it counts. Clients
// use it to show the limit before an invitation is refused; the server checks
// every admission regardless.
func (h *Handler) GetMemberLimit(w http.ResponseWriter, r *http.Request) {
	wsID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	resp := MemberLimitResponse{Action: string(entitlement.ActionOff)}
	if h.Entitlements != nil {
		decision := h.Entitlements.Gate(r.Context(), uuid.UUID(workspaceID.Bytes), entitlement.GateMembers)
		m := memberLimit{workspaceID: workspaceID, decision: decision}
		if m.active() {
			slots, err := h.Queries.CountWorkspaceMemberSlots(r.Context(), workspaceID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load member limit")
				return
			}
			// Before telling the client the workspace is full, re-read the
			// policy as an admission would: right after an upgrade the cached
			// one is still Free's, and the client would offer an upgrade the
			// workspace already has.
			if m.decision.Gate.Action == entitlement.ActionEnforce && m.exceeds(slots) {
				if refresher, ok := h.Entitlements.(entitlement.Refresher); ok {
					m.decision = refresher.RefreshGate(r.Context(), uuid.UUID(workspaceID.Bytes), entitlement.GateMembers)
					decision = m.decision
				}
			}
		}
		if m.active() {
			slots, err := h.Queries.CountWorkspaceMemberSlots(r.Context(), workspaceID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load member limit")
				return
			}
			limit := m.limit()
			resp = MemberLimitResponse{
				Action:             string(decision.Gate.Action),
				Limit:              &limit,
				Members:            &slots.Members,
				PendingInvitations: &slots.PendingInvitations,
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
