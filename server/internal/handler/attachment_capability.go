package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

// Attachment download capabilities — MUL-5292.
//
// A native download is a browser-level request: Electron's
// webContents.downloadURL (and an <img> in a cross-site webview) carries
// neither the desktop client's Authorization header nor a session cookie, so
// the authenticated /api/attachments/{id}/download endpoint answers 401 and
// the user never gets a file.
//
// CloudFront and presign deployments already sidestep this — the
// authenticated GetAttachmentByID hands those clients a signed storage URL
// that needs no credentials of ours. Proxy mode (local disk, private object
// host) had no equivalent and kept returning the auth-gated API path, which
// is the entirety of the bug: one unfinished branch of an otherwise correct
// design, not a missing Electron feature.
//
// A capability closes that branch the same way the other two modes do. The
// ALREADY-AUTHENTICATED GetAttachmentByID mints a short-lived signature
// granting read on exactly one attachment; a separate public route accepts
// it. Membership is verified when the capability is minted and never at
// redemption — the signature is the proof that the check happened.
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
	attachmentCapabilityVersion = "v1"

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
	// cannot be replayed as it. It rides the URL as dl=1; the empty (load) intent
	// signs the historical three-field message, byte-identical.
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
// decimal timestamp, so no pair of (id, exp) values can be re-split into a
// different pair that produces the same signed message.
func signAttachmentCapability(attachmentID string, exp int64) string {
	return signAttachmentCapabilityIntent(attachmentID, exp, "")
}

// signAttachmentCapabilityIntent signs the capability over (version, id, exp)
// and, for a non-empty intent, an extra domain-separation term. The load-intent
// capability (intent "") signs exactly the historical three-field message, so
// its signatures stay byte-identical to before; the download-intent capability
// (attachmentCapabilityDownloadIntent) appends "|attachment". Binding the intent
// is forward-looking headroom, not a current mitigation — flipping today's two
// intents is not itself exploitable (see attachmentCapabilityDownloadIntent) —
// but it keeps the signature honest if a future intent is ever more privileged
// than the load link.
func signAttachmentCapabilityIntent(attachmentID string, exp int64, intent string) string {
	mac := hmac.New(sha256.New, attachmentCapabilitySigningKey())
	mac.Write([]byte(attachmentCapabilityVersion))
	mac.Write([]byte("|"))
	mac.Write([]byte(attachmentID))
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
func attachmentCapabilityPath(attachmentID string, now time.Time) string {
	exp := now.Add(attachmentCapabilityTTL).Unix()
	return "/api/attachments/" + attachmentID + "/signed-download" +
		"?exp=" + strconv.FormatInt(exp, 10) +
		"&sig=" + signAttachmentCapability(attachmentID, exp)
}

// attachmentDownloadCapabilityPath builds the site-relative capability URL for a
// forced-attachment ("download button") intent. Same short-lived, single-id
// shape as attachmentCapabilityPath, plus dl=1 — which the redemption route
// turns into a Content-Disposition: attachment. Kept separate, and separately
// signed, so the load-intent link the preview path consumes keeps serving media
// inline. Site-relative for the same reason: the inline-media re-sign hook only
// upgrades absolute URLs, so it keeps ignoring this one.
func attachmentDownloadCapabilityPath(attachmentID string, now time.Time) string {
	exp := now.Add(attachmentCapabilityTTL).Unix()
	return "/api/attachments/" + attachmentID + "/signed-download" +
		"?exp=" + strconv.FormatInt(exp, 10) +
		"&sig=" + signAttachmentCapabilityIntent(attachmentID, exp, attachmentCapabilityDownloadIntent) +
		"&dl=1"
}

// verifyAttachmentCapability fails closed on every path: a missing field, an
// unparseable expiry, an elapsed expiry, a malformed signature, and a
// signature minted for a different attachment all return false.
//
// The signature covers the claimed expiry, so extending `exp` invalidates the
// signature rather than extending the capability.
func verifyAttachmentCapability(attachmentID, rawExp, rawSig, intent string, now time.Time) bool {
	if attachmentID == "" || rawExp == "" || rawSig == "" {
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
	want, err := hex.DecodeString(signAttachmentCapabilityIntent(attachmentID, exp, intent))
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
// Always proxy-streams. Capabilities are only minted in proxy mode, and
// streaming means this route never emits a cross-origin redirect, so the
// signed query cannot leak to a CDN in a Referer.
func (h *Handler) DownloadAttachmentWithCapability(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	query := r.URL.Query()
	// dl=1 is the forced-attachment ("download button") intent. It is covered by
	// a distinct signature, so a load-intent link cannot flip itself to a
	// download by appending dl=1 — the verification below would fail.
	intent := ""
	if query.Get("dl") == "1" {
		intent = attachmentCapabilityDownloadIntent
	}
	if !verifyAttachmentCapability(attachmentID, query.Get("exp"), query.Get("sig"), intent, time.Now()) {
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
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}

	h.setAttachmentPreviewSecurityHeaders(w)
	// The signature travels in the query string. If the streamed body is
	// itself a document that loads subresources, no-referrer keeps that
	// query out of the outbound Referer.
	w.Header().Set("Referrer-Policy", "no-referrer")

	h.proxyAttachmentDownload(w, r, att, h.Storage.KeyFromURL(att.Url), intent == attachmentCapabilityDownloadIntent)
}
