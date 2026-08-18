package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Phase 1 of MUL-5372 / GitHub #5999: bulk responses stop pre-signing
// attachment download URLs for callers that advertise they can resolve the
// stable path themselves. The whole design rests on the server default never
// moving, so most of what these tests pin is what happens when a caller says
// NOTHING.

func loadTestAttachment(t *testing.T, id string) db.Attachment {
	t.Helper()
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	return att
}

// withCloudFrontSigner installs a real signer so DownloadURL takes the signed
// branch — the only mode where the new capability changes anything.
func withCloudFrontSigner(t *testing.T) {
	t.Helper()
	orig := testHandler.CFSigner
	testHandler.CFSigner = testCloudFrontSigner(t)
	t.Cleanup(func() { testHandler.CFSigner = orig })
}

func requestWithCapabilities(caps string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/issues/x/comments", nil)
	if caps != "" {
		r.Header.Set("X-Client-Capabilities", caps)
	}
	return r
}

// TestAttachmentURLMode_DefaultsToSignedWithoutCapability is the compatibility
// promise itself: a caller that does not advertise the capability — every
// installed mobile build, every third-party script — must get exactly the
// pre-signed URL it gets today.
func TestAttachmentURLMode_DefaultsToSignedWithoutCapability(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps string
	}{
		{"no header at all", ""},
		{"unrelated capabilities", "chat_draft_restore,something_else"},
		{"near-miss token", "stable_attachment_url"},
		{"empty header", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := attachmentURLModeFromRequest(requestWithCapabilities(tc.caps)); got != attachmentURLModeSigned {
				t.Errorf("caps %q resolved to stable mode; the server default must never move", tc.caps)
			}
		})
	}
	// A nil request has no declaration to read either.
	if got := attachmentURLModeFromRequest(nil); got != attachmentURLModeSigned {
		t.Errorf("nil request must resolve to signed mode")
	}
}

func TestAttachmentURLMode_HonorsAdvertisedCapability(t *testing.T) {
	for _, caps := range []string{
		ClientCapabilityStableAttachmentURLs,
		"chat_draft_restore," + ClientCapabilityStableAttachmentURLs,
		ClientCapabilityStableAttachmentURLs + ",other",
		"  " + ClientCapabilityStableAttachmentURLs + "  ",
	} {
		if got := attachmentURLModeFromRequest(requestWithCapabilities(caps)); got != attachmentURLModeStable {
			t.Errorf("caps %q did not resolve to stable mode", caps)
		}
	}
}

// TestAttachmentToResponse_StableModeDropsSignature covers the actual payload
// win: same attachment, same signer, two modes.
func TestAttachmentToResponse_StableModeDropsSignature(t *testing.T) {
	withCloudFrontSigner(t)

	id := seedAttachmentURL(t, "https://static.example.test/ws/a.png", "a.png", "image/png", 1234)
	att := loadTestAttachment(t, id)

	signed := testHandler.attachmentToResponse(att, attachmentURLModeSigned)
	stable := testHandler.attachmentToResponse(att, attachmentURLModeStable)

	if !strings.Contains(signed.DownloadURL, "Signature=") {
		t.Fatalf("signed mode should carry a signature, got %q", signed.DownloadURL)
	}
	want := "/api/attachments/" + id + "/download"
	if stable.DownloadURL != want {
		t.Errorf("stable download_url = %q, want %q", stable.DownloadURL, want)
	}
	if len(stable.DownloadURL) >= len(signed.DownloadURL) {
		t.Errorf("stable mode did not shrink download_url (%d vs %d bytes)", len(stable.DownloadURL), len(signed.DownloadURL))
	}

	// Only download_url may move. url and markdown_url carry different
	// contracts (raw storage identity / persistable reference) and clients
	// index markdown bodies on markdown_url, so drift there would corrupt
	// already-persisted content.
	if stable.URL != signed.URL {
		t.Errorf("url must not change with mode: %q vs %q", stable.URL, signed.URL)
	}
	if stable.MarkdownURL != signed.MarkdownURL {
		t.Errorf("markdown_url must not change with mode: %q vs %q", stable.MarkdownURL, signed.MarkdownURL)
	}
	if stable.ID != signed.ID || stable.Filename != signed.Filename ||
		stable.ContentType != signed.ContentType || stable.SizeBytes != signed.SizeBytes {
		t.Errorf("stable mode altered identity/metadata fields")
	}
}

// TestAttachmentToResponse_SignedModeRotatesButStableDoesNot documents why this
// change exists at all: the signed value is a function of an expiry the server
// re-derives per request, so identical content yields different bytes on every
// read and defeats content-keyed caching. Stable mode has no such term.
func TestAttachmentToResponse_SignedModeRotatesButStableDoesNot(t *testing.T) {
	withCloudFrontSigner(t)

	id := seedAttachmentURL(t, "https://static.example.test/ws/b.png", "b.png", "image/png", 10)
	att := loadTestAttachment(t, id)

	stableA := testHandler.attachmentToResponse(att, attachmentURLModeStable)
	stableB := testHandler.attachmentToResponse(att, attachmentURLModeStable)
	if stableA.DownloadURL != stableB.DownloadURL {
		t.Errorf("stable download_url must be deterministic: %q vs %q", stableA.DownloadURL, stableB.DownloadURL)
	}
	if strings.Contains(stableA.DownloadURL, "Policy=") || strings.Contains(stableA.DownloadURL, "Signature=") {
		t.Errorf("stable download_url leaked signature params: %q", stableA.DownloadURL)
	}

	// The rotation half. attachmentToResponse mints its expiry from time.Now()
	// at second granularity, so calling it twice in a row would usually land in
	// the same second and produce identical bytes — asserting on that directly
	// would be a flaky test of a real property. Drive the signer with two
	// explicit expiries instead: it proves the URL varies with the expiry term,
	// which is exactly what makes the response unstable as the clock advances.
	signed := testHandler.attachmentToResponse(att, attachmentURLModeSigned)
	if !strings.Contains(signed.DownloadURL, "Policy=") {
		t.Fatalf("signed mode should embed a policy, got %q", signed.DownloadURL)
	}
	base := time.Now()
	first := testHandler.CFSigner.SignedURL(att.Url, base.Add(30*time.Minute))
	second := testHandler.CFSigner.SignedURL(att.Url, base.Add(30*time.Minute+time.Second))
	if first == second {
		t.Errorf("signed URL did not change with the expiry; rotation is the premise of this whole change")
	}
	if strings.Split(first, "?")[0] != strings.Split(second, "?")[0] {
		t.Errorf("only the query should rotate, the resource path must be stable: %q vs %q", first, second)
	}
	// And re-signing with the SAME expiry must reproduce the same bytes —
	// otherwise the rotation above would prove nothing about the clock.
	if again := testHandler.CFSigner.SignedURL(att.Url, base.Add(30*time.Minute)); again != first {
		t.Errorf("signing is not deterministic for a fixed expiry: %q vs %q", again, first)
	}
}

// TestAttachmentToResponse_StableModeIsNoOpWithoutSigner pins the presign /
// proxy deployments: they never took the signing branch, so the capability
// changes nothing for them.
func TestAttachmentToResponse_StableModeIsNoOpWithoutSigner(t *testing.T) {
	orig := testHandler.CFSigner
	testHandler.CFSigner = nil
	t.Cleanup(func() { testHandler.CFSigner = orig })

	id := seedAttachmentURL(t, "http://rustfs:9000/test-bucket/c.txt", "c.txt", "text/plain", 5)
	att := loadTestAttachment(t, id)

	signed := testHandler.attachmentToResponse(att, attachmentURLModeSigned)
	stable := testHandler.attachmentToResponse(att, attachmentURLModeStable)
	if signed.DownloadURL != stable.DownloadURL {
		t.Errorf("without a signer both modes must agree: %q vs %q", signed.DownloadURL, stable.DownloadURL)
	}
}

// TestGetAttachmentByID_IgnoresStableCapability is the load-bearing carve-out.
// Stable-mode callers exchange the stable path for a loadable URL here —
// `multica attachment download <id>` reads download_url off this endpoint — so
// honoring the capability would break the flow that makes stable mode safe.
func TestGetAttachmentByID_IgnoresStableCapability(t *testing.T) {
	withCloudFrontSigner(t)

	id := seedAttachmentURL(t, "https://static.example.test/ws/d.png", "d.png", "image/png", 7)

	req := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id, nil)
	req.Header.Set("X-Client-Capabilities", ClientCapabilityStableAttachmentURLs)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	testHandler.GetAttachmentByID(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/attachments/{id} = %d, body %s", w.Code, w.Body.String())
	}
	var resp AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.DownloadURL, "Signature=") {
		t.Errorf("single-attachment endpoint must always sign, even for stable-mode callers; got %q", resp.DownloadURL)
	}
}
