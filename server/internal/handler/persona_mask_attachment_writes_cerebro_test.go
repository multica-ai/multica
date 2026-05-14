// JEH-1189: unit tests for the attachment list/write redaction paths.
// GetAttachmentByID single-row wiring is already covered by
// persona_mask_resources_cerebro_test.go (JEH-1173). These tests cover
// the list helper used by ListAttachments / ListChatMessageAttachments /
// UploadFile response — all of which now go through
// maskAttachmentsForCaller.

package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newAttachmentRow(id byte, classification, filename string) db.Attachment {
	var raw [16]byte
	for i := range raw {
		raw[i] = id
	}
	return db.Attachment{
		ID:             pgtype.UUID{Bytes: raw, Valid: true},
		WorkspaceID:    pgtype.UUID{Bytes: [16]byte{0xee}, Valid: true},
		Filename:       filename,
		Url:            "https://cdn.example.com/" + filename,
		Classification: classification,
	}
}

func TestMaskAttachmentsForCaller_InternalRow_NoMask(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask:      &stubMaskInvoker{decisions: []PersonaMaskDecision{{Allowed: true}}},
		PersonaMaskAudit: audit,
	}
	atts := []db.Attachment{newAttachmentRow(1, "internal", "report.pdf")}
	resp := []AttachmentResponse{{ID: "a1", Filename: "report.pdf", URL: "u", DownloadURL: "d"}}
	out := h.maskAttachmentsForCaller(newReqWithUser("u-member"), "ws-1", atts, resp)
	if len(out) != 1 || out[0].Filename != "report.pdf" || out[0].URL != "u" || out[0].DownloadURL != "d" {
		t.Errorf("internal row + no mask should preserve payload, got %+v", out)
	}
	if len(audit.events) != 0 {
		t.Errorf("allow-without-mask should not write audit row, got %d events", len(audit.events))
	}
}

func TestMaskAttachmentsForCaller_PiiRow_FieldsBlanked(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: true, MaskedFields: []string{"filename", "url", "download_url"}, DecisionID: "d-1"},
		}},
		PersonaMaskAudit: audit,
	}
	atts := []db.Attachment{newAttachmentRow(1, "pii", "leak.pdf")}
	resp := []AttachmentResponse{{ID: "a1", Filename: "leak.pdf", URL: "u", DownloadURL: "d"}}
	out := h.maskAttachmentsForCaller(newReqWithUser("u-member"), "ws-1", atts, resp)
	if len(out) != 1 || out[0].Filename != "" || out[0].URL != "" || out[0].DownloadURL != "" {
		t.Errorf("pii row + internal-grant actor should see all three blanked, got %+v", out)
	}
	if out[0].ID == "" {
		t.Errorf("ID must survive masking so client can still reference the row, got %+v", out[0])
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "mask" || audit.events[0].ResourceKind != personaKindAttachment {
		t.Errorf("mask outcome must write one audit row tagged attachment, got %+v", audit.events)
	}
}

func TestMaskAttachmentsForCaller_DenyDropsRow(t *testing.T) {
	audit := &stubAuditWriter{}
	h := &Handler{
		PersonaMask: &stubMaskInvoker{decisions: []PersonaMaskDecision{
			{Allowed: false, Reason: "no read grant"},
			{Allowed: true},
		}},
		PersonaMaskAudit: audit,
	}
	atts := []db.Attachment{
		newAttachmentRow(1, "pii", "private.pdf"),
		newAttachmentRow(2, "internal", "public.pdf"),
	}
	resp := []AttachmentResponse{
		{ID: "a1", Filename: "private.pdf"},
		{ID: "a2", Filename: "public.pdf"},
	}
	out := h.maskAttachmentsForCaller(newReqWithUser("u-member"), "ws-1", atts, resp)
	if len(out) != 1 || out[0].ID != "a2" {
		t.Errorf("deny row must be dropped, internal row should remain, got %+v", out)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != "deny" {
		t.Errorf("deny outcome must write one audit row, got %+v", audit.events)
	}
}

func TestMaskAttachmentsForCaller_NoInvoker_PreservesPayload(t *testing.T) {
	h := &Handler{}
	atts := []db.Attachment{newAttachmentRow(1, "pii", "leak.pdf")}
	resp := []AttachmentResponse{{ID: "a1", Filename: "leak.pdf", URL: "u", DownloadURL: "d"}}
	out := h.maskAttachmentsForCaller(newReqWithUser("u-member"), "ws-1", atts, resp)
	if len(out) != 1 || out[0].Filename != "leak.pdf" || out[0].URL != "u" || out[0].DownloadURL != "d" {
		t.Errorf("no invoker should preserve full payload, got %+v", out)
	}
}

// TestUploadFile_DenyDoesNotLeakResponse is a regression test for Rasp's
// review feedback on PR #272: the original code wrote `uploadResp` directly
// after `maskAttachmentsForCaller` returned an empty slice, leaking
// filename/url/download_url on a deny decision. The fix returns 403 instead.
func TestUploadFile_DenyDoesNotLeakResponse(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	origMask := testHandler.PersonaMask
	testHandler.PersonaMask = &stubMaskInvoker{
		decisions: []PersonaMaskDecision{{Allowed: false, Reason: "regression test"}},
	}
	defer func() { testHandler.PersonaMask = origMask }()

	const sensitiveName = "very-secret-leak.pdf"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", sensitiveName)
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("classified contents"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("deny outcome must produce 403, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), sensitiveName) {
		t.Errorf("response body must not echo the sensitive filename, got %s", w.Body.String())
	}
	// Belt-and-braces: assert the full URL field is absent. A future
	// regression that switches to writeJSON(masked[0]) where masked has
	// been zeroed would still trip this — empty fields are fine, the
	// raw stored URL appearing in the body is what we're protecting
	// against.
	if strings.Contains(w.Body.String(), "https://cdn") || strings.Contains(w.Body.String(), "/uploads/") {
		t.Errorf("response body must not leak a download URL, got %s", w.Body.String())
	}
}
