// JEH-1187: unit tests for the per-row invitation mask helpers.
//
// In-package so the stub PersonaMaskInvoker can drop a canned decision
// without dragging in persona / cerebro packages — keeps the test honest
// about the seam between upstream handler and cerebro mask layer.

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// invitationStubInvoker records the kind/id/field-classes it was called
// with and returns canned decisions in order. Tests that only need a
// single decision still get a deterministic answer if the helper happens
// to call Decide more than once.
type invitationStubInvoker struct {
	decisions  []PersonaMaskDecision
	calls      int
	gotKinds   []string
	gotIDs     []string
	gotClasses []map[string]string
}

func (s *invitationStubInvoker) Decide(
	_ context.Context,
	_, _, kind, resourceID, _ string,
	fieldClasses map[string]string,
) PersonaMaskDecision {
	s.gotKinds = append(s.gotKinds, kind)
	s.gotIDs = append(s.gotIDs, resourceID)
	s.gotClasses = append(s.gotClasses, fieldClasses)
	if s.calls >= len(s.decisions) {
		s.calls++
		return PersonaMaskDecision{Allowed: true}
	}
	d := s.decisions[s.calls]
	s.calls++
	return d
}

func TestApplyInvitationMask_RedactsEmails(t *testing.T) {
	row := InvitationResponse{
		ID:           "inv-1",
		InviteeEmail: "mia@example.com",
		InviterEmail: "admin@example.com",
		Role:         "member",
	}
	applyInvitationMask(&row, []string{personaFieldInviteeEmail, personaFieldInviterEmail})
	if row.InviteeEmail != "" {
		t.Errorf("invitee_email should be redacted to empty, got %q", row.InviteeEmail)
	}
	if row.InviterEmail != "" {
		t.Errorf("inviter_email should be redacted to empty, got %q", row.InviterEmail)
	}
	if row.Role != "member" {
		t.Errorf("non-masked fields must remain intact, got role=%q", row.Role)
	}
}

func TestApplyInvitationMask_UnknownFieldIsNoop(t *testing.T) {
	row := InvitationResponse{ID: "inv-1", InviteeEmail: "mia@example.com"}
	applyInvitationMask(&row, []string{"some_future_field"})
	if row.InviteeEmail != "mia@example.com" {
		t.Errorf("unknown field name must be a no-op, got invitee_email=%q", row.InviteeEmail)
	}
}

func TestInvitationFieldClasses_MarksBothEmailsAsPII(t *testing.T) {
	m := invitationFieldClasses()
	if m[personaFieldInviteeEmail] != "pii" {
		t.Errorf("invitee_email must be tagged pii, got %q", m[personaFieldInviteeEmail])
	}
	if m[personaFieldInviterEmail] != "pii" {
		t.Errorf("inviter_email must be tagged pii, got %q", m[personaFieldInviterEmail])
	}
}

func TestMaskInvitationForCaller_NoInvoker_PassesThrough(t *testing.T) {
	h := &Handler{}
	resp := InvitationResponse{ID: "inv-1", InviteeEmail: "mia@example.com", InviterEmail: "admin@example.com"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/invitations/inv-1", nil)
	if !h.maskInvitationForCaller(w, r, "ws-1", "inv-1", &resp) {
		t.Fatalf("no invoker should return ok=true")
	}
	if resp.InviteeEmail != "mia@example.com" || resp.InviterEmail != "admin@example.com" {
		t.Errorf("no invoker must leave emails intact, got %+v", resp)
	}
}

// Scenario 1 — actor IS the invitee (self-fetch on the invite accept
// page). Persona policy returns Allowed=true with no masked fields; the
// full payload renders. The handler must not code-hardcode the
// "actor == invitee" shortcut, so this test only asserts the wire-level
// outcome: the stub mimics what persona would return for a self read.
func TestMaskInvitationForCaller_Self_FullPayload(t *testing.T) {
	stub := &invitationStubInvoker{decisions: []PersonaMaskDecision{{Allowed: true}}}
	h := &Handler{PersonaMask: stub}

	resp := InvitationResponse{
		ID:           "inv-1",
		InviteeEmail: "mia@example.com",
		InviterEmail: "admin@example.com",
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/invitations/inv-1", nil)

	if !h.maskInvitationForCaller(w, r, "ws-1", "inv-1", &resp) {
		t.Fatalf("self should yield ok=true")
	}
	if resp.InviteeEmail != "mia@example.com" {
		t.Errorf("self must see own invitee_email, got %q", resp.InviteeEmail)
	}
	if resp.InviterEmail != "admin@example.com" {
		t.Errorf("self must see inviter_email, got %q", resp.InviterEmail)
	}
	if len(stub.gotKinds) != 1 || stub.gotKinds[0] != personaKindInvitation {
		t.Errorf("kind passed to persona must be %q, got %v", personaKindInvitation, stub.gotKinds)
	}
	if len(stub.gotIDs) != 1 || stub.gotIDs[0] != "inv-1" {
		t.Errorf("resource id must be the invitation id, got %v", stub.gotIDs)
	}
	if classes := stub.gotClasses[0]; classes[personaFieldInviteeEmail] != "pii" || classes[personaFieldInviterEmail] != "pii" {
		t.Errorf("field classifications must mark both emails as pii, got %v", classes)
	}
}

// Scenario 2 — workspace admin with pii-grant. Persona returns Allowed=true
// with no masked fields; full payload renders.
func TestMaskInvitationForCaller_AdminWithPII_FullPayload(t *testing.T) {
	stub := &invitationStubInvoker{decisions: []PersonaMaskDecision{{Allowed: true}}}
	h := &Handler{PersonaMask: stub}

	resp := InvitationResponse{
		ID:           "inv-1",
		InviteeEmail: "mia@example.com",
		InviterEmail: "admin@example.com",
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-1/invitations", nil)

	if !h.maskInvitationForCaller(w, r, "ws-1", "inv-1", &resp) {
		t.Fatalf("admin should yield ok=true")
	}
	if resp.InviteeEmail != "mia@example.com" || resp.InviterEmail != "admin@example.com" {
		t.Errorf("admin with pii-grant must see full payload, got %+v", resp)
	}
}

// Scenario 3 — workspace member without pii-grant. Persona returns
// Allowed=true but lists both email fields as masked; both must become
// empty strings in the rendered response.
func TestMaskInvitationForCaller_MemberWithoutPII_EmailsRedacted(t *testing.T) {
	stub := &invitationStubInvoker{
		decisions: []PersonaMaskDecision{{
			Allowed:      true,
			MaskedFields: []string{personaFieldInviteeEmail, personaFieldInviterEmail},
		}},
	}
	h := &Handler{PersonaMask: stub}

	resp := InvitationResponse{
		ID:           "inv-1",
		InviteeEmail: "mia@example.com",
		InviterEmail: "admin@example.com",
		Role:         "member",
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-1/invitations", nil)

	if !h.maskInvitationForCaller(w, r, "ws-1", "inv-1", &resp) {
		t.Fatalf("mask-decision should still yield ok=true (only the fields are zeroed)")
	}
	if resp.InviteeEmail != "" {
		t.Errorf("invitee_email must be redacted for member without pii grant, got %q", resp.InviteeEmail)
	}
	if resp.InviterEmail != "" {
		t.Errorf("inviter_email must be redacted for member without pii grant, got %q", resp.InviterEmail)
	}
	if resp.Role != "member" {
		t.Errorf("non-PII fields must survive the mask, got role=%q", resp.Role)
	}
}

// Scenario 3 (list variant) — workspace member without pii-grant calling
// the workspace listing. The row stays in the slice so the UI can render
// a redacted placeholder; only the two email fields are zeroed.
func TestMaskInvitationsListForCaller_MemberWithoutPII_BlankPerRow(t *testing.T) {
	stub := &invitationStubInvoker{
		decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{personaFieldInviteeEmail, personaFieldInviterEmail}},
			{Allowed: true}, // second row: self-invitee, no mask
		},
	}
	h := &Handler{PersonaMask: stub}

	rows := []InvitationResponse{
		{ID: "inv-1", WorkspaceID: "ws-1", InviteeEmail: "alice@example.com", InviterEmail: "admin@example.com"},
		{ID: "inv-2", WorkspaceID: "ws-1", InviteeEmail: "self@example.com", InviterEmail: "admin@example.com"},
	}
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-1/invitations", nil)

	out := h.maskInvitationsListForCaller(r, "ws-1", rows)
	if len(out) != 2 {
		t.Fatalf("list mask must keep row count (presence-leak guard), got %d", len(out))
	}
	if out[0].InviteeEmail != "" || out[0].InviterEmail != "" {
		t.Errorf("row 0 emails should be redacted, got %+v", out[0])
	}
	if out[0].ID != "inv-1" {
		t.Errorf("row 0 id must survive the mask so the UI can render a placeholder, got %q", out[0].ID)
	}
	if out[1].InviteeEmail != "self@example.com" {
		t.Errorf("row 1 (self-invitee) must keep its email, got %q", out[1].InviteeEmail)
	}
}

// Deny scenario (defense-in-depth) — persona denies the row entirely.
// The list helper still keeps the row but blanks both PII fields, so the
// UI cannot infer whose invitation this is.
func TestMaskInvitationsListForCaller_DenyBlanksEmails(t *testing.T) {
	stub := &invitationStubInvoker{decisions: []PersonaMaskDecision{{Allowed: false, Reason: "no grant"}}}
	h := &Handler{PersonaMask: stub}

	rows := []InvitationResponse{{
		ID:           "inv-1",
		WorkspaceID:  "ws-1",
		InviteeEmail: "mia@example.com",
		InviterEmail: "admin@example.com",
	}}
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-1/invitations", nil)
	out := h.maskInvitationsListForCaller(r, "ws-1", rows)
	if len(out) != 1 {
		t.Fatalf("deny on list row must keep the row (presence-leak guard), got %d", len(out))
	}
	if out[0].InviteeEmail != "" || out[0].InviterEmail != "" {
		t.Errorf("deny must blank both PII fields, got %+v", out[0])
	}
	if out[0].ID == "" {
		t.Errorf("deny must preserve the row identifier, got empty id")
	}
}

// Single-render deny path: 404 + audit, caller MUST stop.
func TestMaskInvitationForCaller_Deny404(t *testing.T) {
	stub := &invitationStubInvoker{decisions: []PersonaMaskDecision{{Allowed: false, Reason: "no grant"}}}
	h := &Handler{PersonaMask: stub}

	resp := InvitationResponse{ID: "inv-1", InviteeEmail: "mia@example.com"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/invitations/inv-1", nil)

	if h.maskInvitationForCaller(w, r, "ws-1", "inv-1", &resp) {
		t.Fatalf("deny must return ok=false so caller stops before writeJSON")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("deny must write 404, got %d", w.Code)
	}
}
