// JEH-1173: unit tests for issue / comment / attachment / artifact
// mask helpers. Stubs both PersonaMaskInvoker and PersonaMaskAuditWriter
// so the test asserts on both the response shape AND that a redaction
// landed in the audit ledger.

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type stubAuditWriter struct {
	events []PersonaMaskAuditEvent
}

func (s *stubAuditWriter) Write(_ context.Context, evt PersonaMaskAuditEvent) {
	s.events = append(s.events, evt)
}

func newReqWithUser(userID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if userID != "" {
		r.Header.Set("X-User-ID", userID)
	}
	return r
}

func textPtr(s string) *string { return &s }

// --------------------------------------------------------------------- issue

func TestMaskIssueForCaller_NoInvoker(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	resp := IssueResponse{ID: "i1", Title: "secret", Description: textPtr("body")}
	ok := h.maskIssueForCaller(w, newReqWithUser("u1"), db.Issue{Classification: "pii"}, &resp)
	if !ok {
		t.Fatalf("no invoker should always allow")
	}
	if resp.Title != "secret" || resp.Description == nil || *resp.Description != "body" {
		t.Errorf("no invoker should leave fields untouched, got %+v", resp)
	}
}

func TestMaskIssueForCaller_AllowWithMasking(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"description"}, DecisionID: "d-1"},
		}},
		PersonaMaskAudit: audit,
	}
	w := httptest.NewRecorder()
	resp := IssueResponse{ID: "i1", Title: "T", Description: textPtr("body")}
	ok := h.maskIssueForCaller(w, newReqWithUser("u1"), db.Issue{Classification: "pii"}, &resp)
	if !ok {
		t.Fatalf("allow-with-mask should return true")
	}
	if resp.Description != nil {
		t.Errorf("description should be nil after mask, got %v", resp.Description)
	}
	if resp.Title != "T" {
		t.Errorf("title should remain, got %q", resp.Title)
	}
	if len(audit.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(audit.events))
	}
	if audit.events[0].Decision != "mask" {
		t.Errorf("audit decision should be 'mask', got %q", audit.events[0].Decision)
	}
	if audit.events[0].DecisionID != "d-1" {
		t.Errorf("audit should carry persona decision_id, got %q", audit.events[0].DecisionID)
	}
}

func TestMaskIssueForCaller_DenyWrites404AndAudit(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false, Reason: "no grant"},
		}},
		PersonaMaskAudit: audit,
	}
	w := httptest.NewRecorder()
	resp := IssueResponse{ID: "i1"}
	ok := h.maskIssueForCaller(w, newReqWithUser("u1"), db.Issue{Classification: "pii"}, &resp)
	if ok {
		t.Fatalf("deny must return false")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("deny should write 404, got %d", w.Code)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "deny" {
		t.Errorf("deny must write one 'deny' audit row, got %+v", audit.events)
	}
}

func TestMaskIssueForCaller_AllowNoMaskSkipsAudit(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: true}}},
		PersonaMaskAudit: audit,
	}
	w := httptest.NewRecorder()
	resp := IssueResponse{ID: "i1"}
	_ = h.maskIssueForCaller(w, newReqWithUser("u1"), db.Issue{Classification: "internal"}, &resp)
	if len(audit.events) != 0 {
		t.Errorf("allow-without-mask must NOT write audit, got %+v", audit.events)
	}
}

func TestIssueFieldClasses_OnlyPiiOrAboveCarriesFieldMap(t *testing.T) {
	for _, c := range []string{"", "public", "internal"} {
		if got := issueFieldClasses(c); got != nil {
			t.Errorf("classification %q should yield nil map, got %+v", c, got)
		}
	}
	got := issueFieldClasses("pii")
	if got["description"] != "pii" || got["title"] != "pii" {
		t.Errorf("pii row should label content fields, got %+v", got)
	}
}

// ------------------------------------------------------------------- comment

func TestMaskCommentsForCaller_DenyDropsRowMaskBlanks(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false},                                  // c1: deny → dropped
			{Allowed: true, MaskedFields: []string{"content"}}, // c2: mask
			{Allowed: true},                                   // c3: pass-through
		}},
		PersonaMaskAudit: audit,
	}
	ws := pgtype.UUID{}
	comments := []db.Comment{
		{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, WorkspaceID: ws, Content: "C1", Classification: "pii"},
		{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, WorkspaceID: ws, Content: "C2", Classification: "pii"},
		{ID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, WorkspaceID: ws, Content: "C3", Classification: "internal"},
	}
	resp := []CommentResponse{
		{ID: "c1", Content: "C1"},
		{ID: "c2", Content: "C2"},
		{ID: "c3", Content: "C3"},
	}
	issue := db.Issue{WorkspaceID: ws}
	out := h.maskCommentsForCaller(newReqWithUser("u1"), issue, comments, resp)
	if len(out) != 2 {
		t.Fatalf("deny should drop the row, got %d remaining", len(out))
	}
	if out[0].ID != "c2" || out[0].Content != "" {
		t.Errorf("mask row should be blanked, got %+v", out[0])
	}
	if out[1].ID != "c3" || out[1].Content != "C3" {
		t.Errorf("pass-through row should be intact, got %+v", out[1])
	}
	if len(audit.events) != 2 {
		t.Errorf("expected 2 audit rows (deny + mask), got %d", len(audit.events))
	}
}

// ---------------------------------------------------------------- attachment

func TestMaskAttachmentForCaller_DenyWrites404(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: false}}},
		PersonaMaskAudit: audit,
	}
	w := httptest.NewRecorder()
	resp := AttachmentResponse{ID: "a1", URL: "https://x", DownloadURL: "https://x?sig=1", Filename: "secret.pdf"}
	ok := h.maskAttachmentForCaller(w, newReqWithUser("u1"), db.Attachment{Classification: "pii"}, &resp)
	if ok {
		t.Fatalf("deny must return false")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("deny should 404, got %d", w.Code)
	}
}

func TestMaskAttachmentForCaller_AllowWithMaskBlanksURLs(t *testing.T) {
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"url", "download_url", "filename"}},
		}},
		PersonaMaskAudit: &stubAuditWriter{},
	}
	w := httptest.NewRecorder()
	resp := AttachmentResponse{
		ID: "a1", URL: "https://x", DownloadURL: "https://x?sig=1", Filename: "secret.pdf",
	}
	ok := h.maskAttachmentForCaller(w, newReqWithUser("u1"), db.Attachment{Classification: "pii"}, &resp)
	if !ok {
		t.Fatalf("allow-with-mask should return true")
	}
	if resp.URL != "" || resp.DownloadURL != "" || resp.Filename != "" {
		t.Errorf("masked fields should be blanked, got %+v", resp)
	}
}

// ------------------------------------------------------------------ artifact

func TestMaskArtifactForCaller_DenyWrites404(t *testing.T) {
	h := &Handler{
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: false}}},
		PersonaMaskAudit: &stubAuditWriter{},
	}
	w := httptest.NewRecorder()
	resp := ArtifactResponse{ID: "art1", Title: "T", Body: "B"}
	ok := h.maskArtifactForCaller(w, newReqWithUser("u1"), db.Artifact{}, &resp)
	if ok {
		t.Fatalf("deny must return false")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("deny should 404, got %d", w.Code)
	}
}

func TestMaskArtifactForCaller_AllowWithBodyMasked(t *testing.T) {
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"body"}},
		}},
		PersonaMaskAudit: &stubAuditWriter{},
	}
	w := httptest.NewRecorder()
	resp := ArtifactResponse{ID: "art1", Title: "T", Body: "B"}
	ok := h.maskArtifactForCaller(w, newReqWithUser("u1"), db.Artifact{}, &resp)
	if !ok {
		t.Fatalf("expected allow")
	}
	if resp.Body != "" {
		t.Errorf("body should be blanked, got %q", resp.Body)
	}
	if resp.Title != "T" {
		t.Errorf("title should remain, got %q", resp.Title)
	}
}

// ------------------------------------------------ recordPersonaMaskRedaction

func TestRecordPersonaMaskRedaction_AllowNoMaskSkips(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{PersonaMaskAudit: audit}
	h.recordPersonaMaskRedaction(context.Background(),
		PersonaMaskDecision{Allowed: true},
		"ws", "member", "u1", "read", "multica.issue", "i1", "internal")
	if len(audit.events) != 0 {
		t.Errorf("allow-no-mask must not record, got %+v", audit.events)
	}
}

func TestRecordPersonaMaskRedaction_NilWriterNoop(t *testing.T) {
	h := &Handler{}
	// Just shouldn't panic.
	h.recordPersonaMaskRedaction(context.Background(),
		PersonaMaskDecision{Allowed: false},
		"ws", "member", "u1", "read", "multica.issue", "i1", "pii")
}
