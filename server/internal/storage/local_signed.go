package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signed-query auth for /uploads/* on the local-storage backend.
//
// The S3 + CloudFront deployment path lets clients fetch attachments
// directly via a presigned URL — `<img src="...?Signature=...">` works
// from any browser context because the credentials are inside the URL,
// not in an Authorization header. The local-storage backend has no
// equivalent: a self-hosted deployment with `/uploads/*` behind
// `middleware.Auth` works for cookie-auth web clients (browser auto-sends
// the cookie on `<img>` loads) but fails for token-auth clients (Desktop's
// default mode, legacy localStorage Web sessions, the mobile app, anything
// using `Authorization: Bearer ...`) because browsers do not attach the
// Authorization header to native `<img>` / `<video>` resource loads.
//
// To close that gap without giving up the auth wins, we sign the upload
// URLs the same way CloudFront does: append `?exp=<unix>&sig=<HMAC>` and
// let `ServeLocalUpload` accept that signature as an alternative to the
// Bearer / cookie path. The sig is HMAC-SHA256 over `key|exp` with the
// server's JWTSecret. Each signed URL is bound to one specific key, has
// a short TTL (we reuse `defaultAttachmentDownloadURLTTL` = 30 min, the
// same TTL as CloudFront-mode), and cannot be widened to expose other
// files in the bucket — exactly the same security model as the S3 path.
//
// The handler API surface is `SignLocalUploadURL` (callable from
// attachmentToResponse to mint a fresh URL each time the metadata is
// served) and `VerifyLocalUploadSignature` (callable from the request
// path to authorize the read). The functions take the key and the secret
// as parameters so the storage package does not depend on the auth
// package — the caller injects `auth.JWTSecret()`.
const (
	signedURLExpParam = "exp"
	signedURLSigParam = "sig"
)

// SignLocalUploadURL returns rawURL with `?exp=<unix>&sig=<HMAC>` query
// params appended. The signature is HMAC-SHA256 over `key|exp` with the
// supplied secret.
//
// `key` MUST be the storage key (e.g. "workspaces/abc/file.png"), not
// the full URL — `ServeLocalUpload` re-derives the key from the request
// path and runs the same HMAC, so they have to match exactly. Picking
// the key keeps the signature stable across local vs base-URL builds and
// avoids canonicalization bugs with the URL host / scheme.
//
// `expiry` is an absolute time; the verifier compares it against
// time.Now() with no skew tolerance because the signer and verifier are
// the same process.
//
// If rawURL already has query parameters, they are preserved.
func SignLocalUploadURL(rawURL, key string, secret []byte, expiry time.Time) string {
	exp := strconv.FormatInt(expiry.Unix(), 10)
	sig := computeLocalUploadSignature(key, exp, secret)
	q := signedQueryParams(exp, sig)

	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		// Preserve existing query but ensure ours wins on collision.
		existing, err := url.ParseQuery(rawURL[idx+1:])
		if err == nil {
			existing.Del(signedURLExpParam)
			existing.Del(signedURLSigParam)
			merged := existing.Encode()
			if merged != "" {
				return rawURL[:idx+1] + merged + "&" + q
			}
		}
		return rawURL[:idx+1] + q
	}
	return rawURL + "?" + q
}

// VerifyLocalUploadSignature returns true when (expStr, sigStr) is a
// well-formed, unexpired signature for `key` produced by
// SignLocalUploadURL with the same secret. Constant-time comparison and
// hard parse-failure rejection ensure that a malformed input cannot fall
// through to an "unsigned-but-trusted" branch.
func VerifyLocalUploadSignature(key, expStr, sigStr string, secret []byte, now time.Time) bool {
	if key == "" || expStr == "" || sigStr == "" {
		return false
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	if now.Unix() >= expUnix {
		return false
	}
	want := computeLocalUploadSignature(key, expStr, secret)
	// Both `want` and `sigStr` are URL-safe base64 strings of fixed
	// length, so a byte-wise constant-time compare is enough. A malformed
	// sigStr will simply not equal `want` and return false.
	return hmac.Equal([]byte(want), []byte(sigStr))
}

func computeLocalUploadSignature(key, expStr string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	// `|` is not part of the URL-safe base64 alphabet and cannot appear
	// in a Unix timestamp, so it is a safe separator and there is no
	// collision risk between (key, expStr) and (key', expStr') when
	// concatenated.
	mac.Write([]byte(key))
	mac.Write([]byte{'|'})
	mac.Write([]byte(expStr))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func signedQueryParams(exp, sig string) string {
	v := url.Values{}
	v.Set(signedURLExpParam, exp)
	v.Set(signedURLSigParam, sig)
	return v.Encode()
}

// LocalUploadSignatureFromQuery extracts the (exp, sig) pair from a
// URL query string. Empty strings indicate "no signature present" —
// callers should treat that as "fall through to other auth paths",
// not as a verification failure.
func LocalUploadSignatureFromQuery(q url.Values) (exp, sig string) {
	return q.Get(signedURLExpParam), q.Get(signedURLSigParam)
}
