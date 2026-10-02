package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
)

// Native downloads cannot attach the client's Authorization header and may
// carry no session cookie. GetAttachmentByID mints a bearer capability bound
// to the authenticated user and one attachment, in every storage mode. Each
// redemption checks that user's current membership in the attachment's workspace.
// The URL is still transferable: user binding enables revocation, not proof
// that the person redeeming it is the authenticated user who requested it.
//
// Deliberately NOT a general-purpose credential:
//   - bound to a single attachment id, so it cannot be replayed against another
//   - 60-second TTL, because the download it feeds starts immediately
//   - signed with a key domain-separated from the JWT secret, so it can be
//     neither forged from nor used to forge a session token
//   - never persisted, and never emitted into list responses

const (
	// attachmentCapabilityVersion is part of the signed message so the
	// message format can change later without a v1 signature verifying
	// against a v2 verifier.
	attachmentCapabilityVersion = "v2"

	// attachmentCapabilityTTL is short by design. The client mints a
	// capability and hands it to the native downloader in the same tick;
	// anything longer only widens the window in which a leaked URL is
	// still redeemable.
	attachmentCapabilityTTL = 60 * time.Second

	// attachmentCapabilityKeyDomain separates this signing domain from
	// every other HMAC the deployment derives from the same root secret.
	attachmentCapabilityKeyDomain = "attachment-download-capability:"

	// attachmentCapabilityDownloadIntent is folded into a forced-attachment
	// ("download button") capability's signed message. This is deliberate
	// headroom, not a fix for a current threat: today the load-intent and
	// download-intent links are minted in the same response, and flipping a load
	// link into a forced download only makes it *less* dangerous (inline is the
	// risky disposition), so binding the intent stops nothing an attacker could
	// exploit now. It is kept so that if a later intent ever grants more than the
	// load link, the signature is already intent-bound and a lower-privilege link
	// cannot be replayed as it. It rides the URL as dl=1.
	attachmentCapabilityDownloadIntent = "attachment"
)

var (
	attachmentCapabilityKeyOnce sync.Once
	attachmentCapabilityKey     []byte
)

// attachmentCapabilitySigningKey derives the capability key from the
// deployment's JWT secret via SHA-256, mirroring composioStateSecret in
// server/cmd/server/router.go. Deriving rather than reusing means a
// capability signature can never collide with a JWT signature; rotating
// JWT_SECRET additionally invalidates outstanding capabilities, which is the
// behaviour an operator would expect from a rotation.
func attachmentCapabilitySigningKey() []byte {
	attachmentCapabilityKeyOnce.Do(func() {
		sum := sha256.Sum256(append([]byte(attachmentCapabilityKeyDomain), auth.JWTSecret()...))
		attachmentCapabilityKey = sum[:]
	})
	return attachmentCapabilityKey
}

// signAttachmentCapability returns the hex HMAC over the capability's fields.
//
// The fields are joined with a separator that cannot occur inside a UUID or a
// decimal timestamp, so fields cannot be re-split into a different capability.
func signAttachmentCapability(attachmentID, userID string, exp int64) string {
	return signAttachmentCapabilityIntent(attachmentID, userID, exp, "")
}

// v2 binds the requesting user as well as the attachment, expiry and intent.
// Unbound v1 links deliberately do not verify; clients must request a fresh URL.
func signAttachmentCapabilityIntent(attachmentID, userID string, exp int64, intent string) string {
	mac := hmac.New(sha256.New, attachmentCapabilitySigningKey())
	mac.Write([]byte(attachmentCapabilityVersion))
	mac.Write([]byte("|"))
	mac.Write([]byte(attachmentID))
	mac.Write([]byte("|"))
	mac.Write([]byte(userID))
	mac.Write([]byte("|"))
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	if intent != "" {
		mac.Write([]byte("|"))
		mac.Write([]byte(intent))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// attachmentCapabilityPath builds the site-relative capability URL handed back
// as `download_url`.
//
// Site-relative on purpose. Clients already resolve `download_url` against the
// configured API base, and keeping the shape relative leaves the inline-media
// re-sign path in packages/views/editor/attachment.tsx untouched: that hook
// only upgrades to ABSOLUTE URLs, so it keeps ignoring proxy-mode responses
// exactly as it does today instead of pinning a 60-second URL into an <img>
// it caches for 20 minutes.
func attachmentCapabilityPath(attachmentID, userID string, now time.Time) string {
	return attachmentCapabilityIntentPath(attachmentID, userID, now, "")
}

// attachmentDownloadCapabilityPath builds the site-relative capability URL for a
// forced-attachment ("download button") intent. Same short-lived, single-id
// shape as attachmentCapabilityPath, plus dl=1 — which the redemption route
// turns into a Content-Disposition: attachment. Kept separate, and separately
// signed, so the load-intent link the preview path consumes keeps serving media
// inline. Site-relative for the same reason: the inline-media re-sign hook only
// upgrades absolute URLs, so it keeps ignoring this one.
func attachmentDownloadCapabilityPath(attachmentID, userID string, now time.Time) string {
	return attachmentCapabilityIntentPath(attachmentID, userID, now, attachmentCapabilityDownloadIntent)
}

func attachmentCapabilityIntentPath(attachmentID, userID string, now time.Time, intent string) string {
	exp := now.Add(attachmentCapabilityTTL).Unix()
	query := url.Values{
		"uid": {userID},
		"exp": {strconv.FormatInt(exp, 10)},
		"sig": {signAttachmentCapabilityIntent(attachmentID, userID, exp, intent)},
	}
	if intent == attachmentCapabilityDownloadIntent {
		query.Set("dl", "1")
	}
	return "/api/attachments/" + attachmentID + "/signed-download?" + query.Encode()
}

// verifyAttachmentCapability fails closed on every path: a missing field, an
// unparseable expiry, an elapsed expiry, a malformed signature, and a
// signature minted for a different attachment all return false.
//
// The signature covers the claimed expiry, so extending `exp` invalidates the
// signature rather than extending the capability.
func verifyAttachmentCapability(attachmentID, userID, rawExp, rawSig, intent string, now time.Time) bool {
	if _, err := util.ParseUUID(attachmentID); err != nil {
		return false
	}
	if _, err := util.ParseUUID(userID); err != nil {
		return false
	}
	if rawExp == "" || rawSig == "" {
		return false
	}
	exp, err := strconv.ParseInt(rawExp, 10, 64)
	if err != nil {
		return false
	}
	if now.Unix() > exp {
		return false
	}
	got, err := hex.DecodeString(rawSig)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(signAttachmentCapabilityIntent(attachmentID, userID, exp, intent))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// ---------------------------------------------------------------------------
// DownloadAttachmentWithCapability — GET /api/attachments/{id}/signed-download
// ---------------------------------------------------------------------------
//
// Registered as a PUBLIC route. The capability in the query IS the credential,
// and a native download request has nothing for middleware.Auth to read, so
// putting this behind Auth would defeat its only purpose. The authenticated
// /api/attachments/{id}/download endpoint is left exactly as it was — this
// route is additive, so clients that predate it keep working unchanged and
// there is no second copy of the header/cookie/PAT/task-token resolution that
// middleware.Auth owns.
//
// Always proxy-streams, even for CDN/presign deployments. Redirecting would
// exchange the revocable capability for a storage URL that survives member
// removal. Streaming also prevents the signed query leaking in a Referer.
func (h *Handler) DownloadAttachmentWithCapability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	attachmentID := chi.URLParam(r, "id")
	query := r.URL.Query()
	userID := query.Get("uid")
	// dl=1 is the forced-attachment ("download button") intent. It is covered by
	// a distinct signature, so a load-intent link cannot flip itself to a
	// download by appending dl=1 — the verification below would fail.
	intent := ""
	if query.Get("dl") == "1" {
		intent = attachmentCapabilityDownloadIntent
	}
	if !verifyAttachmentCapability(attachmentID, userID, query.Get("exp"), query.Get("sig"), intent, time.Now()) {
		// One generic rejection for every reason, so a caller cannot
		// distinguish "expired" from "forged" from "wrong attachment"
		// and use the difference to probe the signer.
		writeError(w, http.StatusForbidden, "invalid or expired download link")
		return
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return
	}
	att, err := h.Queries.GetAttachmentByIDOnly(r.Context(), attUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	// Never consult MembershipCache here: a stale positive entry would keep a
	// removed member's links alive. Scope comes from the row, not the request.
	if _, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(att.WorkspaceID)); err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if h.Storage == nil {
		writeFeatureDisabled(w, "storage_not_configured", "storage not configured")
		return
	}

	h.proxyAttachmentDownload(w, r, att, h.Storage.KeyFromURL(att.Url), intent == attachmentCapabilityDownloadIntent)
}
