package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/storage"
)

// withAvatarStorage swaps the handler's storage/config for one test and
// restores it afterwards. Mirrors the setup the attachment download tests use.
func withAvatarStorage(t *testing.T, store storage.Storage, publicURL string) {
	t.Helper()
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.PublicURL = publicURL
	testHandler.cfg.AttachmentDownloadMode = "auto"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})
}

const testAvatarKey = "workspaces/11111111-1111-1111-1111-111111111111/avatar.png"

// TestResolveAvatarURL_PrivateStorageRewritesToSignedEndpoint is the #6024
// case: a private bucket with no public CDN stored a raw S3 URL in avatar_url,
// which the browser can only 403 on. Reads must hand back the signed avatar
// endpoint instead.
func TestResolveAvatarURL_PrivateStorageRewritesToSignedEndpoint(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	raw := "https://cdn.example.com/" + testAvatarKey
	got := testHandler.resolveAvatarURL(raw)

	want := avatarURLPathPrefix + signAvatarKey(testAvatarKey) + "/" + testAvatarKey
	if got != want {
		t.Fatalf("resolveAvatarURL = %q, want %q", got, want)
	}
	key, ok := avatarKeyFromServedURL(got)
	if !ok || key != testAvatarKey {
		t.Fatalf("resolved URL does not verify back to the key: key=%q ok=%v", key, ok)
	}
}

// TestResolveAvatarURL_PublicCdnUnchanged guards the deployments that already
// work: a public CDN domain with no per-request signing serves the raw URL
// fine, and routing it through the API would only add a hop.
func TestResolveAvatarURL_PublicCdnUnchanged(t *testing.T) {
	withAvatarStorage(t, &mockStorage{}, "")

	raw := "https://cdn.example.com/" + testAvatarKey
	if got := testHandler.resolveAvatarURL(raw); got != raw {
		t.Fatalf("resolveAvatarURL = %q, want unchanged %q", got, raw)
	}
}

// TestResolveAvatarURL_CloudFrontSignedRewrites covers the other private
// shape: a CDN domain IS configured, but it serves private content through
// per-request signed URLs, so the unsigned stored URL is a 403.
func TestResolveAvatarURL_CloudFrontSignedRewrites(t *testing.T) {
	withAvatarStorage(t, &mockStorage{}, "")
	testHandler.CFSigner = testCloudFrontSigner(t)

	raw := "https://cdn.example.com/" + testAvatarKey
	if got := testHandler.resolveAvatarURL(raw); !strings.HasPrefix(got, avatarURLPathPrefix) {
		t.Fatalf("resolveAvatarURL = %q, want the signed avatar endpoint", got)
	}
}

// TestResolveAvatarURL_LocalStorageUnchanged — LocalStorage objects are served
// by the public /uploads/* route, so they are already loadable as-is.
func TestResolveAvatarURL_LocalStorageUnchanged(t *testing.T) {
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Skip("local storage unavailable")
	}
	withAvatarStorage(t, local, "")

	raw := local.ObjectURL(testAvatarKey)
	if got := testHandler.resolveAvatarURL(raw); got != raw {
		t.Fatalf("resolveAvatarURL = %q, want unchanged %q", got, raw)
	}
}

// TestResolveAvatarURL_PassesThroughForeignValues — avatar_url also holds
// emoji markers, inline data URIs, and third-party profile URLs. None of them
// are our storage objects and all must survive untouched.
func TestResolveAvatarURL_PassesThroughForeignValues(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	for _, raw := range []string{
		"",
		"emoji:🐙",
		"data:image/svg+xml,%3Csvg%3E%3C/svg%3E",
		"https://lh3.googleusercontent.com/a/profile.png",
		"https://avatars.githubusercontent.com/u/12345?v=4",
	} {
		if got := testHandler.resolveAvatarURL(raw); got != raw {
			t.Errorf("resolveAvatarURL(%q) = %q, want unchanged", raw, got)
		}
	}
}

// TestResolveAvatarURL_NonImageKeyUnchanged — an avatar_url pointed at a
// non-image object must not be laundered into a publicly fetchable URL.
func TestResolveAvatarURL_NonImageKeyUnchanged(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	for _, key := range []string{"workspaces/ws/secret.pdf", "workspaces/ws/logo.svg", "workspaces/ws/noext"} {
		raw := "https://cdn.example.com/" + key
		if got := testHandler.resolveAvatarURL(raw); got != raw {
			t.Errorf("resolveAvatarURL(%q) = %q, want unchanged", raw, got)
		}
	}
}

// TestResolveAvatarURL_AnchorsOnPublicURL — clients that don't share the API's
// document origin (Desktop, mobile webview) need an absolute URL.
func TestResolveAvatarURL_AnchorsOnPublicURL(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "https://api.example.test/")

	got := testHandler.resolveAvatarURL("https://cdn.example.com/" + testAvatarKey)
	if !strings.HasPrefix(got, "https://api.example.test"+avatarURLPathPrefix) {
		t.Fatalf("resolveAvatarURL = %q, want it anchored on PublicURL", got)
	}
	key, ok := avatarKeyFromServedURL(got)
	if !ok || key != testAvatarKey {
		t.Fatalf("absolute URL does not verify back to the key: key=%q ok=%v", key, ok)
	}
}

// TestResolveAvatarURL_RoundTripIsStable — a client may PATCH a resolved
// response value straight back into avatar_url. Resolving it again must not
// nest a second prefix.
func TestResolveAvatarURL_RoundTripIsStable(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	once := testHandler.resolveAvatarURL("https://cdn.example.com/" + testAvatarKey)
	if twice := testHandler.resolveAvatarURL(once); twice != once {
		t.Fatalf("resolveAvatarURL is not idempotent: %q then %q", once, twice)
	}
}

// TestResolveAvatarURL_ForgedAvatarPathNotReSigned — an unsigned or
// wrongly-signed avatar path must not be accepted and re-signed, or storing
// one would be a way to publish an arbitrary object.
func TestResolveAvatarURL_ForgedAvatarPathNotReSigned(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	forged := avatarURLPathPrefix + "not-a-signature/" + testAvatarKey
	if got := testHandler.resolveAvatarURL(forged); got != forged {
		t.Fatalf("resolveAvatarURL(%q) = %q, want unchanged (no re-signing)", forged, got)
	}
}

// TestNormalizeStoredAvatarURL_RecoversObjectURL — the write side keeps the
// column holding a durable object reference even when a client sends back the
// resolved value.
func TestNormalizeStoredAvatarURL_RecoversObjectURL(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "https://api.example.test")

	raw := "https://cdn.example.com/" + testAvatarKey
	resolved := testHandler.resolveAvatarURL(raw)
	if got := testHandler.normalizeStoredAvatarURL(resolved); got != raw {
		t.Fatalf("normalizeStoredAvatarURL = %q, want the object URL %q", got, raw)
	}
	// Anything else is stored verbatim.
	for _, other := range []string{raw, "emoji:🐙", "https://lh3.googleusercontent.com/a/p.png"} {
		if got := testHandler.normalizeStoredAvatarURL(other); got != other {
			t.Errorf("normalizeStoredAvatarURL(%q) = %q, want unchanged", other, got)
		}
	}
}

func serveAvatarRequest(t *testing.T, sig, key string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/avatars/{sig}/*", testHandler.ServeAvatar)
	req := httptest.NewRequest(http.MethodGet, "/api/avatars/"+sig+"/"+key, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// avatarObjectOpts describes a seeded storage object the way UploadFile would
// have written it: the attachment row's id IS the object filename.
type avatarObjectOpts struct {
	contentType string
	// boundToIssue attaches the row to a real issue — the shape a private
	// image attachment has, and the shape that must never become a public
	// avatar.
	boundToIssue bool
	// foreignWorkspace seeds the object in a freshly created workspace the
	// fixture user is not a member of.
	foreignWorkspace bool
}

// seedForeignWorkspace creates a workspace the fixture user has no membership
// in, for the cross-workspace leg of the authorization tests.
func seedForeignWorkspace(t *testing.T) string {
	t.Helper()
	slug := "avatar-foreign-" + uuid.NewString()[:8]
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $1, '', 'AVF')
		RETURNING id::text
	`, slug).Scan(&id); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

// seedIssueForAvatarTest creates an issue an attachment can hang off.
func seedIssueForAvatarTest(t *testing.T, workspaceID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'avatar authorization fixture', 'todo', 'medium', 'member', $2,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id::text
	`, workspaceID, testUserID).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
	})
	return id
}

// seedAvatarObject inserts the attachment row and returns its storage key.
func seedAvatarObject(t *testing.T, opts avatarObjectOpts) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate attachment id: %v", err)
	}
	if opts.contentType == "" {
		opts.contentType = "image/png"
	}
	workspaceID := testWorkspaceID
	if opts.foreignWorkspace {
		workspaceID = seedForeignWorkspace(t)
	}
	var issueID any
	if opts.boundToIssue {
		issueID = seedIssueForAvatarTest(t, workspaceID)
	}
	key := "workspaces/" + workspaceID + "/" + id.String() + ".png"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO attachment (id, workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, issue_id)
		VALUES ($1, $2, 'member', $3, 'avatar.png', $4, $5, 10, $6)
	`, id.String(), workspaceID, testUserID,
		"https://cdn.example.com/"+key, opts.contentType, issueID); err != nil {
		t.Fatalf("seed attachment row: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id.String())
	})
	return key
}

// TestServeAvatar_PresignRedirects — the private-bucket read path: a valid
// signature yields a 302 to a freshly presigned storage URL with no forced
// download disposition, so it renders inside an <img>.
func TestServeAvatar_PresignRedirects(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{})

	w := serveAvatarRequest(t, signAvatarKey(key), key)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("Location = %q, want a presigned storage URL", loc.String())
	}
	if got := loc.Query().Get("response-content-disposition"); got != "" {
		t.Fatalf("response-content-disposition = %q, want inline-loadable URL", got)
	}
}

// TestServeAvatar_RejectsForgedSignature — the signature is the credential;
// without it the endpoint must not reach storage at all.
func TestServeAvatar_RejectsForgedSignature(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{})

	for _, sig := range []string{"", "deadbeef", signAvatarKey("workspaces/other/x.png")} {
		if w := serveAvatarRequest(t, sig, key); w.Code != http.StatusNotFound {
			t.Errorf("sig %q: status = %d, want 404", sig, w.Code)
		}
	}
}

// TestServeAvatar_RejectsNonImageKey — a correctly signed non-image key still
// 404s, so this route can never serve a document.
func TestServeAvatar_RejectsNonImageKey(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	key := "workspaces/ws/private.pdf"
	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestServeAvatar_ProxiesPrivateHostBody — a bucket reachable only from inside
// the network (http://rustfs:9000) can't be redirected to, so the body streams
// through the API with an inline image content type.
func TestServeAvatar_ProxiesPrivateHostBody(t *testing.T) {
	store := &mockStorage{}
	withAvatarStorage(t, store, "")
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	key := seedAvatarObject(t, avatarObjectOpts{})
	store.put(key, []byte("PNGBYTES"))

	w := serveAvatarRequest(t, signAvatarKey(key), key)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
	if w.Body.String() != "PNGBYTES" {
		t.Fatalf("body = %q, want the stored object", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Authorization boundary (MUL-5393 review)
// ---------------------------------------------------------------------------
//
// Naming a storage object is not permission to publish it. These cover the
// reproduced escalation: a private image attached to an issue must not become
// a permanently public, unauthenticated avatar link — on the read side or the
// write side, in this workspace or any other.

// TestServeAvatar_RejectsAttachmentBoundToIssue is the reproduced attack. Even
// with a valid signature (i.e. the value really did get stored in avatar_url),
// an object attached to workspace content must never be served.
func TestServeAvatar_RejectsAttachmentBoundToIssue(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{boundToIssue: true})

	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an issue attachment", w.Code)
	}
}

// TestServeAvatar_RejectsForeignWorkspaceAttachment — the cross-workspace leg:
// a private image from a workspace the avatar's owner has nothing to do with
// is still an issue attachment, so it stays unreachable.
func TestServeAvatar_RejectsForeignWorkspaceAttachment(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{foreignWorkspace: true, boundToIssue: true})

	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another workspace's attachment", w.Code)
	}
}

// TestServeAvatar_RejectsNonImageAttachmentRow — the extension says image, the
// row says otherwise. Fail closed on the row, which is the authoritative one.
func TestServeAvatar_RejectsNonImageAttachmentRow(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{contentType: "application/pdf"})

	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a non-image row", w.Code)
	}
}

// TestServeAvatar_RejectsWorkspaceKeyWithoutAttachmentRow — a workspace-scoped
// key with no backing row can't be shown to be avatar-class, so it fails
// closed rather than being served on the strength of its shape alone.
func TestServeAvatar_RejectsWorkspaceKeyWithoutAttachmentRow(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := "workspaces/" + testWorkspaceID + "/" + uuid.NewString() + ".png"

	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a key with no attachment row", w.Code)
	}
}

// TestServeAvatar_AllowsUserNamespaceUpload — UploadFile's no-workspace branch
// writes to users/<id>/ and creates no attachment row. That namespace never
// holds issue/comment/chat content, so it stays serveable.
func TestServeAvatar_AllowsUserNamespaceUpload(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := "users/" + testUserID + "/" + uuid.NewString() + ".png"

	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 for a standalone user upload", w.Code)
	}
}

func acceptAvatarURL(t *testing.T, raw, current string) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/me", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	value, ok := testHandler.acceptAvatarURL(w, req, raw, current)
	if !ok {
		return "", w.Code
	}
	return value, http.StatusOK
}

// TestAcceptAvatarURL_RejectsBoundAttachment — the write side refuses to store
// the value at all, so the escalation is stopped before anything is persisted.
func TestAcceptAvatarURL_RejectsBoundAttachment(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{boundToIssue: true})

	_, status := acceptAvatarURL(t, "https://cdn.example.com/"+key, "")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for an issue attachment", status)
	}
}

// TestAcceptAvatarURL_AcceptsStandaloneUpload — the real avatar flow: an
// unbound image upload is exactly what AvatarUploadControl produces.
func TestAcceptAvatarURL_AcceptsStandaloneUpload(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{})

	raw := "https://cdn.example.com/" + key
	value, status := acceptAvatarURL(t, raw, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if value != raw {
		t.Fatalf("stored value = %q, want the object URL %q", value, raw)
	}
}

// TestAcceptAvatarURL_AcceptsUnchangedResend — clients round-trip whole
// objects. Re-sending what is already persisted grants nothing new, so it must
// not start failing after this gate landed (agent duplicate, profile form
// resubmit).
func TestAcceptAvatarURL_AcceptsUnchangedResend(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{boundToIssue: true})
	raw := "https://cdn.example.com/" + key

	if _, status := acceptAvatarURL(t, raw, raw); status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an unchanged re-send", status)
	}
}

// TestAcceptAvatarURL_NormalizesServedURL — a client that PATCHes back the
// resolved response value stores the durable object reference, not the signed
// path, so a JWT_SECRET rotation can't strand the avatar.
func TestAcceptAvatarURL_NormalizesServedURL(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := seedAvatarObject(t, avatarObjectOpts{})
	raw := "https://cdn.example.com/" + key

	value, status := acceptAvatarURL(t, testHandler.resolveAvatarURL(raw), "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if value != raw {
		t.Fatalf("stored value = %q, want the object URL %q", value, raw)
	}
}

// TestAcceptAvatarURL_PassesThroughForeignValues — emoji markers, data URIs
// and third-party profile URLs are not ours to authorize and must not 403.
func TestAcceptAvatarURL_PassesThroughForeignValues(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	for _, raw := range []string{
		"",
		"emoji:🐙",
		"data:image/svg+xml,%3Csvg%3E%3C/svg%3E",
		"https://lh3.googleusercontent.com/a/profile.png",
	} {
		value, status := acceptAvatarURL(t, raw, "")
		if status != http.StatusOK {
			t.Errorf("acceptAvatarURL(%q) status = %d, want 200", raw, status)
			continue
		}
		if value != raw {
			t.Errorf("acceptAvatarURL(%q) = %q, want unchanged", raw, value)
		}
	}
}

// TestAvatarRedirectMaxAge_StaysBelowSignatureTTL — ATTACHMENT_DOWNLOAD_URL_TTL
// takes any positive duration. A fixed 60s cache would let a browser replay a
// redirect to an already-expired storage URL on a short-TTL deployment.
func TestAvatarRedirectMaxAge_StaysBelowSignatureTTL(t *testing.T) {
	origCfg := testHandler.cfg
	t.Cleanup(func() { testHandler.cfg = origCfg })

	for _, tc := range []struct {
		ttl  time.Duration
		want int
	}{
		{0, avatarRedirectMaxAgeCap}, // unset -> 30m default
		{30 * time.Minute, avatarRedirectMaxAgeCap},
		{2 * time.Minute, avatarRedirectMaxAgeCap},
		{30 * time.Second, 15},
		{10 * time.Second, 5},
		{time.Second, 0},
	} {
		testHandler.cfg.AttachmentDownloadURLTTL = tc.ttl
		got := testHandler.avatarRedirectMaxAge()
		if got != tc.want {
			t.Errorf("ttl %s: avatarRedirectMaxAge = %d, want %d", tc.ttl, got, tc.want)
		}
		if ttl := int(testHandler.attachmentDownloadURLTTL() / time.Second); got >= ttl {
			t.Errorf("ttl %s: cache max-age %d is not strictly below the signature lifetime %d", tc.ttl, got, ttl)
		}
	}
}

// TestServeAvatar_ShortTTLDisablesRedirectCaching — the header the clamp
// actually produces at the low end.
func TestServeAvatar_ShortTTLDisablesRedirectCaching(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	testHandler.cfg.AttachmentDownloadURLTTL = time.Second
	key := seedAvatarObject(t, avatarObjectOpts{})

	w := serveAvatarRequest(t, signAvatarKey(key), key)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

// TestServeAvatar_RejectsChannelMediaKey — channel ingest writes under
// `workspaces/<ws>/lark/…` with no attachment row. Those are inbound chat
// media, not avatars, so the namespace rule must fail closed on them rather
// than treating "no row" as permission.
func TestServeAvatar_RejectsChannelMediaKey(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")
	key := "workspaces/" + testWorkspaceID + "/lark/" + uuid.NewString() + "/deadbeef.png"

	if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a channel media key", w.Code)
	}
}

// TestAvatarKeyIsPublishable_OperatorNamespace — an object the operator placed
// in the bucket outside the upload flow stays usable. Only the workspace
// namespace can hold content belonging to someone other than whoever is
// setting the avatar, so only it has to prove itself; rejecting everything
// else would break the documented "an explicit avatar_url is preserved"
// contract for self-hosted deployments that host their own brand assets.
func TestAvatarKeyIsPublishable_OperatorNamespace(t *testing.T) {
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	for _, key := range []string{"brand/logo.png", "avatars/agent.png"} {
		if !testHandler.avatarKeyIsPublishable(context.Background(), key) {
			t.Errorf("avatarKeyIsPublishable(%q) = false, want true", key)
		}
		if w := serveAvatarRequest(t, signAvatarKey(key), key); w.Code != http.StatusFound {
			t.Errorf("serve %q: status = %d, want 302", key, w.Code)
		}
	}
}
