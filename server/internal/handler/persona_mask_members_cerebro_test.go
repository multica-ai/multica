// JEH-1079: unit tests for the per-row member mask helper.
//
// Stays in-package (handler) and uses a stub PersonaMaskInvoker so we
// don't drag in persona / cerebro packages — keeps the test honest
// about the seam between upstream and cerebro.

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubMaskInvoker struct {
	decisions []PersonaMaskDecision
	calls     int
}

func (s *stubMaskInvoker) Decide(
	_ context.Context,
	_, _, _, _, _ string,
	_ map[string]string,
) PersonaMaskDecision {
	if s.calls >= len(s.decisions) {
		// Default to allow + no mask when the test forgot to specify
		// enough decisions — that fail-loud-style would be nicer, but
		// the helper is intentionally tolerant of len mismatch.
		s.calls++
		return PersonaMaskDecision{Allowed: true}
	}
	d := s.decisions[s.calls]
	s.calls++
	return d
}

func TestMaskMembersListForCaller_NoInvoker(t *testing.T) {
	h := &Handler{}
	rows := []MemberWithUserResponse{{ID: "m1", Email: "mia@example.com", Name: "Mia"}}
	out := h.maskMembersListForCaller(httptest.NewRequest(http.MethodGet, "/x", nil), "ws-1", rows)
	if out[0].Email != "mia@example.com" {
		t.Fatalf("no invoker should preserve email, got %q", out[0].Email)
	}
}

func TestMaskMembersListForCaller_AllowedWithEmailMasked(t *testing.T) {
	h := &Handler{PersonaMask: &stubMaskInvoker{
		decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"email"}},
		},
	}}
	rows := []MemberWithUserResponse{{ID: "m1", Email: "mia@example.com", Name: "Mia"}}
	out := h.maskMembersListForCaller(httptest.NewRequest(http.MethodGet, "/x", nil), "ws-1", rows)
	if out[0].Email != "" {
		t.Errorf("email should be redacted to empty, got %q", out[0].Email)
	}
	if out[0].Name != "Mia" {
		t.Errorf("name should remain when not in MaskedFields, got %q", out[0].Name)
	}
}

func TestMaskMembersListForCaller_DenyBlanksContactFields(t *testing.T) {
	h := &Handler{PersonaMask: &stubMaskInvoker{
		decisions: []PersonaMaskDecision{
			{Allowed: false, Reason: "no read grant"},
		},
	}}
	avatar := "https://example.com/m.png"
	rows := []MemberWithUserResponse{{
		ID:        "m1",
		Email:     "mia@example.com",
		Name:      "Mia",
		AvatarURL: &avatar,
	}}
	out := h.maskMembersListForCaller(httptest.NewRequest(http.MethodGet, "/x", nil), "ws-1", rows)
	if out[0].Email != "" || out[0].Name != "" || out[0].AvatarURL != nil {
		t.Errorf("deny should blank name/email/avatar, got %+v", out[0])
	}
	if out[0].ID == "" {
		t.Errorf("deny must preserve the row identifier so the UI can render a placeholder")
	}
}

func TestMaskMembersListForCaller_MultipleRowsCheckedIndependently(t *testing.T) {
	h := &Handler{PersonaMask: &stubMaskInvoker{
		decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"email"}},
			{Allowed: true}, // second row: no mask
		},
	}}
	rows := []MemberWithUserResponse{
		{ID: "m1", Email: "mia@example.com", Name: "Mia"},
		{ID: "m2", Email: "nour@example.com", Name: "Nour"},
	}
	out := h.maskMembersListForCaller(httptest.NewRequest(http.MethodGet, "/x", nil), "ws-1", rows)
	if out[0].Email != "" {
		t.Errorf("row 0 email should be redacted, got %q", out[0].Email)
	}
	if out[1].Email != "nour@example.com" {
		t.Errorf("row 1 email should remain, got %q", out[1].Email)
	}
}
