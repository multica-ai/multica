package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

const capabilityTestAttachmentID = "11111111-2222-3333-4444-555555555555"

// capabilityQuery returns just the query string of a freshly minted
// capability, so tests can vary one field at a time.
func capabilityQuery(t *testing.T, attachmentID string, now time.Time) url.Values {
	t.Helper()
	path := attachmentCapabilityPath(attachmentID, now)
	idx := strings.Index(path, "?")
	if idx < 0 {
		t.Fatalf("capability path has no query: %q", path)
	}
	values, err := url.ParseQuery(path[idx+1:])
	if err != nil {
		t.Fatalf("parse capability query: %v", err)
	}
	return values
}

// newCapabilityRequest deliberately sets NO authentication headers. The whole
// point of the capability route is that it works for a native download that
// carries neither a Bearer token nor a session cookie.
func newCapabilityRequest(attachmentID string, query url.Values) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/signed-download?"+query.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", attachmentID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req, httptest.NewRecorder()
}

// ---------------------------------------------------------------------------
// Signature unit tests (no DB)
// ---------------------------------------------------------------------------

func TestAttachmentCapability_RoundTrips(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	query := capabilityQuery(t, capabilityTestAttachmentID, now)

	if !verifyAttachmentCapability(capabilityTestAttachmentID, query.Get("exp"), query.Get("sig"), now) {
		t.Fatal("freshly minted capability did not verify")
	}
	// Still valid one second before the TTL elapses.
	if !verifyAttachmentCapability(
		capabilityTestAttachmentID, query.Get("exp"), query.Get("sig"),
		now.Add(attachmentCapabilityTTL-time.Second),
	) {
		t.Fatal("capability expired before its TTL elapsed")
	}
}

func TestAttachmentCapability_FailsClosed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	valid := capabilityQuery(t, capabilityTestAttachmentID, now)

	// A capability minted for a DIFFERENT attachment must not verify against
	// this one — the id is inside the signed message, not just the URL.
	other := capabilityQuery(t, "99999999-8888-7777-6666-555555555555", now)

	tampered := []byte(valid.Get("sig"))
	if tampered[0] == 'a' {
		tampered[0] = 'b'
	} else {
		tampered[0] = 'a'
	}

	cases := []struct {
		name     string
		id       string
		exp      string
		sig      string
		now      time.Time
		expected bool
	}{
		{"valid", capabilityTestAttachmentID, valid.Get("exp"), valid.Get("sig"), now, true},
		{"expired", capabilityTestAttachmentID, valid.Get("exp"), valid.Get("sig"), now.Add(attachmentCapabilityTTL + time.Second), false},
		{"tampered signature", capabilityTestAttachmentID, valid.Get("exp"), string(tampered), now, false},
		{"extended expiry", capabilityTestAttachmentID, strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10), valid.Get("sig"), now, false},
		{"signature for another attachment", capabilityTestAttachmentID, other.Get("exp"), other.Get("sig"), now, false},
		{"missing signature", capabilityTestAttachmentID, valid.Get("exp"), "", now, false},
		{"missing expiry", capabilityTestAttachmentID, "", valid.Get("sig"), now, false},
		{"missing id", "", valid.Get("exp"), valid.Get("sig"), now, false},
		{"non-numeric expiry", capabilityTestAttachmentID, "not-a-number", valid.Get("sig"), now, false},
		{"non-hex signature", capabilityTestAttachmentID, valid.Get("exp"), "zzzz", now, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifyAttachmentCapability(tc.id, tc.exp, tc.sig, tc.now); got != tc.expected {
				t.Fatalf("verify = %v, want %v", got, tc.expected)
			}
		})
	}
}

// The capability key must not be the JWT secret, nor a bare hash of it. If the
// two signing domains ever shared a key, a capability signature and a session
// signature would be interchangeable.
func TestAttachmentCapability_KeyIsDomainSeparatedFromJWTSecret(t *testing.T) {
	key := attachmentCapabilitySigningKey()
	if bytes.Equal(key, auth.JWTSecret()) {
		t.Fatal("capability key equals the raw JWT secret")
	}
	bare := sha256.Sum256(auth.JWTSecret())
	if bytes.Equal(key, bare[:]) {
		t.Fatal("capability key is an undomained hash of the JWT secret")
	}
}

// ---------------------------------------------------------------------------
// Route tests
// ---------------------------------------------------------------------------

// installProxyModeStorage puts the handler into proxy mode with an in-memory
// backend, which is the deployment shape (local disk / private object host)
// where capabilities are minted.
func installProxyModeStorage(t *testing.T) *mockStorage {
	t.Helper()
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})
	return store
}

func TestDownloadAttachmentWithCapability_ServesWithoutAuthentication(t *testing.T) {
	store := installProxyModeStorage(t)
	body := []byte("quarterly numbers, do not leak")
	id := seedPreviewAttachment(t, store, "downloads/report.pdf", "Q3 report.pdf", "application/pdf", body)

	req, w := newCapabilityRequest(id, capabilityQuery(t, id, time.Now()))
	testHandler.DownloadAttachmentWithCapability(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatalf("body = %q, want %q", w.Body.String(), body)
	}
	// The original filename must survive to the save dialog — that is half
	// of what the bug report asked for.
	if disposition := w.Header().Get("Content-Disposition"); !strings.Contains(disposition, "Q3 report.pdf") {
		t.Fatalf("Content-Disposition = %q, want the original filename", disposition)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestDownloadAttachmentWithCapability_RejectsInvalidCapabilities(t *testing.T) {
	store := installProxyModeStorage(t)
	body := []byte("secret bytes")
	id := seedPreviewAttachment(t, store, "downloads/secret.txt", "secret.txt", "text/plain", body)

	expired := capabilityQuery(t, id, time.Now().Add(-2*attachmentCapabilityTTL))

	forged := capabilityQuery(t, id, time.Now())
	forged.Set("sig", strings.Repeat("0", len(forged.Get("sig"))))

	// A capability legitimately minted for a different attachment must not
	// unlock this one.
	otherID := seedPreviewAttachment(t, store, "downloads/other.txt", "other.txt", "text/plain", []byte("other"))
	crossed := capabilityQuery(t, otherID, time.Now())

	cases := []struct {
		name  string
		query url.Values
	}{
		{"expired", expired},
		{"forged signature", forged},
		{"capability for another attachment", crossed},
		{"no capability at all", url.Values{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, w := newCapabilityRequest(id, tc.query)
			testHandler.DownloadAttachmentWithCapability(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
			}
			if bytes.Contains(w.Body.Bytes(), body) {
				t.Fatal("rejected response leaked the attachment body")
			}
		})
	}
}

// Range support is what makes an interrupted download resumable (RAS-29). The
// capability route reuses proxyAttachmentDownload, so it must not regress.
func TestDownloadAttachmentWithCapability_PreservesRangeAndHeaders(t *testing.T) {
	store := installProxyModeStorage(t)
	body := bytes.Repeat([]byte("abcdefgh"), 512) // 4 KiB
	id := seedPreviewAttachment(t, store, "downloads/big.bin", "big.bin", "application/octet-stream", body)

	req, w := newCapabilityRequest(id, capabilityQuery(t, id, time.Now()))
	req.Header.Set("Range", "bytes=100-199")
	testHandler.DownloadAttachmentWithCapability(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206; body len=%d", w.Code, w.Body.Len())
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 100-199/4096" {
		t.Fatalf("Content-Range = %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), body[100:200]) {
		t.Fatalf("partial body mismatch: len=%d", w.Body.Len())
	}
	if disposition := w.Header().Get("Content-Disposition"); !strings.Contains(disposition, "big.bin") {
		t.Fatalf("Content-Disposition = %q, want the original filename", disposition)
	}
}

// ---------------------------------------------------------------------------
// Minting
// ---------------------------------------------------------------------------

func TestGetAttachmentByID_ProxyModeReturnsRedeemableCapability(t *testing.T) {
	store := installProxyModeStorage(t)
	id := seedPreviewAttachment(t, store, "downloads/inline.png", "inline.png", "image/png", []byte("png-bytes"))

	req := httptest.NewRequest("GET", "/api/attachments/"+id, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	testHandler.GetAttachmentByID(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}

	// Site-relative on purpose: an absolute URL here would be picked up by
	// the inline-media re-sign path in packages/views/editor/attachment.tsx
	// and pinned into an <img> for far longer than the 60s TTL.
	if !strings.HasPrefix(resp.DownloadURL, "/api/attachments/"+id+"/signed-download?") {
		t.Fatalf("download_url = %q, want a site-relative capability path", resp.DownloadURL)
	}

	parsed, err := url.Parse(resp.DownloadURL)
	if err != nil {
		t.Fatalf("parse download_url: %v", err)
	}
	if !verifyAttachmentCapability(id, parsed.Query().Get("exp"), parsed.Query().Get("sig"), time.Now()) {
		t.Fatalf("minted capability does not verify: %q", resp.DownloadURL)
	}

	// markdown_url is persisted into comment bodies and must outlive the
	// session, so a 60-second capability must never reach it — that is the
	// exact class of bug MUL-3130 fixed.
	if strings.Contains(resp.MarkdownURL, "signed-download") {
		t.Fatalf("markdown_url = %q, must not embed a short-lived capability", resp.MarkdownURL)
	}
}

// List-shaped responses are held far longer than the capability TTL, so
// attachmentToResponse must keep emitting the stable endpoint.
func TestAttachmentToResponse_ProxyModeDoesNotMintCapability(t *testing.T) {
	store := installProxyModeStorage(t)
	id := seedPreviewAttachment(t, store, "downloads/listed.txt", "listed.txt", "text/plain", []byte("listed"))

	att, err := testHandler.Queries.GetAttachmentByIDOnly(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatalf("GetAttachmentByIDOnly: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	if want := "/api/attachments/" + id + "/download"; resp.DownloadURL != want {
		t.Fatalf("download_url = %q, want stable %q (no capability in list responses)", resp.DownloadURL, want)
	}
}

// The pre-existing authenticated route must stay authenticated. Adding a
// public capability entry point must not turn the original path into an open
// one for clients that predate this change.
func TestDownloadAttachment_StillRequiresAuthentication(t *testing.T) {
	store := installProxyModeStorage(t)
	id := seedPreviewAttachment(t, store, "downloads/guarded.txt", "guarded.txt", "text/plain", []byte("guarded"))

	req := httptest.NewRequest("GET", "/api/attachments/"+id+"/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	testHandler.DownloadAttachment(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("unauthenticated request to the legacy download path succeeded: %s", w.Body.String())
	}
}
