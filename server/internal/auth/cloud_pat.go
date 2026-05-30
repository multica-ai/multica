package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// CloudPATPrefix is the literal token prefix that identifies an mcn_
// (Multica Cloud Node) PAT. Tokens with this prefix are validated by
// calling the Multica Cloud Fleet service rather than by hitting our
// local personal_access_tokens table — the cloud is the authoritative
// owner of the token's lifecycle, status, and (owner_id, instance_id)
// binding.
const CloudPATPrefix = "mcn_"

// cloudPATCachePrefix namespaces cloud-PAT cache keys away from
// mul_/mdt_ caches so the three token kinds can't accidentally share
// keys. The trailing slash mirrors the existing patCachePrefix /
// daemonTokenCachePrefix conventions.
const cloudPATCachePrefix = "mul:auth:mcn:"

// cloudPATCacheTTL bounds how long a verified mcn_ token stays cached
// before we re-ask Fleet. The Cloud doc explicitly recommends 30–60s
// "if upstream really needs to cache" — anything longer widens the
// revocation window beyond what Cloud is comfortable with. We pick the
// upper bound: short enough that a revoked / instance-terminated PAT
// stops working within ~1 minute, long enough that a busy daemon /
// CLI on a node collapses to one verify call per minute per token.
//
// We deliberately do NOT reuse AuthCacheTTL (10m) — that's tuned for
// our own DB-backed PAT/daemon-token paths where revocation
// invalidates the cache key directly. We have no way to invalidate
// when Cloud revokes a PAT, so the TTL itself IS the revocation
// latency bound.
const cloudPATCacheTTL = 60 * time.Second

// cloudPATVerifyPath is the Fleet endpoint we POST to. The Cloud doc
// places this under /api/v1/pat/verify; the upstream runs no app-layer
// auth on it (network-level VPC isolation is the access control), so
// we just send the token plaintext and let Fleet decide.
const cloudPATVerifyPath = "/api/v1/pat/verify"

// cloudPATVerifyRequestMaxBytes is the documented Fleet hard cap on
// request bodies (4 KiB). Our marshalled requests are well under that
// — this constant exists to make the intent visible if anyone adds
// optional fields later.
const cloudPATVerifyRequestMaxBytes = 4 * 1024

// cloudPATVerifyResponseMaxBytes bounds how much of a Fleet response
// we'll read. The success payload is well under 1 KiB; we pick 64 KiB
// to stay well clear of pathological responses without enabling a
// memory-exhaustion vector via a misbehaving Fleet.
const cloudPATVerifyResponseMaxBytes = 64 * 1024

// cloudPATDefaultTimeout is the per-request HTTP timeout for verify
// calls when the caller doesn't supply an *http.Client. Auth must
// stay snappy: Fleet should answer in tens of milliseconds, and a
// hung verify would block every incoming request behind it. Tighter
// than cloudruntime's 35s because that one proxies arbitrary user
// traffic; this one only ever sees a small JSON exchange.
const cloudPATDefaultTimeout = 5 * time.Second

// Verifier sentinel errors. Callers (the Auth / DaemonAuth middlewares)
// branch on these to map cloud outcomes onto HTTP status codes:
//
//   - ErrCloudPATInvalid       → 401 (Fleet says token is bad)
//   - ErrCloudPATUnavailable   → 503 (Fleet unreachable / 5xx)
//   - ErrCloudPATNotConfigured → 401 (server has no Fleet URL set; we
//     don't reveal that mcn_ is "supported but disabled" — failing
//     closed avoids treating misconfigured prod the same as enabled)
var (
	ErrCloudPATInvalid       = errors.New("cloud pat invalid")
	ErrCloudPATUnavailable   = errors.New("cloud pat verifier unavailable")
	ErrCloudPATNotConfigured = errors.New("cloud pat verifier not configured")
)

// CloudPATIdentity is what a successful verify resolves to. We keep
// only the fields the auth path actually needs:
//
//   - OwnerID is the user whose request this is (mapped to X-User-ID).
//   - InstanceID / InstanceRecordID are recorded so downstream code can
//     correlate the request with a specific cloud node; they are not
//     used for authorization today, but stashing them now keeps the
//     wire shape stable for callers that later want to assert a
//     particular instance binding.
//
// We deliberately drop token_last4, status, issued_at, etc. — those
// are diagnostic fields that don't belong in cached auth state.
type CloudPATIdentity struct {
	OwnerID          string `json:"o"`
	InstanceID       string `json:"i"`
	InstanceRecordID string `json:"r"`
}

// CloudPATInvalidError carries the Fleet-reported reason for a
// valid=false response. The middleware uses this to log why an mcn_
// token was rejected without exposing the reason in the 401 body —
// per the Cloud doc, callers shouldn't differentiate token_not_found
// vs token_revoked for security decisions.
//
// The "owner_unknown" reason is also produced locally by Verify when
// Cloud accepted the token but the returned owner_id does not map to
// a real user in our DB. Treating that as a Cloud-style "invalid"
// keeps the middleware's response shape uniform — the result is the
// same: 401, drop the token.
type CloudPATInvalidError struct {
	Reason string
}

func (e *CloudPATInvalidError) Error() string {
	if e == nil || e.Reason == "" {
		return "cloud pat invalid"
	}
	return "cloud pat invalid: " + e.Reason
}

// Is lets errors.Is(err, ErrCloudPATInvalid) match any
// CloudPATInvalidError, so callers can branch on the category without
// caring about the exact reason string.
func (e *CloudPATInvalidError) Is(target error) bool {
	return target == ErrCloudPATInvalid
}

// CloudPATInvalidReasonOwnerUnknown is the synthetic reason emitted
// when Cloud verified the token but the returned owner_id was not
// found in the local users table. The Cloud `owner_id` and our
// `users.id` share the same UUID space by contract; a mismatch means
// either the user has been deleted on our side after the node was
// minted, or (worse) something is impersonating Cloud and trying to
// surface a forged owner_id. Either way the request must be rejected.
const CloudPATInvalidReasonOwnerUnknown = "owner_unknown"

// OwnerLookupFunc is the user-existence check Verify runs against
// Cloud's owner_id before caching / returning the identity. The
// caller (typically the middleware closure) wires it to a
// queries.GetUser call.
//
// Return semantics:
//   - (true, nil)  → owner_id is a valid local user; Verify returns success.
//   - (false, nil) → owner_id does not exist locally; Verify returns
//     ErrCloudPATInvalid with reason="owner_unknown" and does NOT
//     cache (a missing user can be re-created later, and we don't
//     want to lock that retry out for a TTL window).
//   - (_, err)     → infrastructure error (DB unreachable, etc.);
//     Verify wraps as ErrCloudPATUnavailable so the caller emits 503,
//     not 401.
type OwnerLookupFunc func(ctx context.Context, ownerID string) (bool, error)

// CloudPATVerifier resolves mcn_ PATs by calling
// POST <fleetURL>/api/v1/pat/verify and caches verified results in
// Redis for cloudPATCacheTTL.
//
// A nil *CloudPATVerifier is safe — Verify returns
// ErrCloudPATNotConfigured. The Auth/DaemonAuth middlewares treat
// "verifier nil" the same as "fleet URL empty", so a server with no
// MULTICA_CLOUD_FLEET_URL configured simply rejects mcn_ tokens at
// the prefix branch instead of nil-derefing.
type CloudPATVerifier struct {
	baseURL string
	http    *http.Client
	rdb     *redis.Client // may be nil — disables caching
}

// CloudPATVerifierConfig assembles the dependencies for
// NewCloudPATVerifier. Keeping this a struct (vs positional args)
// leaves room for future knobs (custom TTL, expected_owner_id binding)
// without churning every call site.
type CloudPATVerifierConfig struct {
	// FleetBaseURL is the Cloud Fleet base URL (e.g.
	// https://fleet.multica.cloud). Trailing slashes are trimmed.
	// Empty disables the verifier — NewCloudPATVerifier returns nil.
	FleetBaseURL string

	// HTTPClient is the client used for verify calls. Optional —
	// when nil, a client with cloudPATDefaultTimeout is created.
	// Pass a shared client when you want connection pooling /
	// per-deployment transport tuning.
	HTTPClient *http.Client

	// Redis backs the positive-result cache. Nil disables caching —
	// every Verify call hits Fleet. Same nil-safe contract as
	// PATCache / DaemonTokenCache.
	Redis *redis.Client
}

// NewCloudPATVerifier returns a verifier for cfg.FleetBaseURL. If the
// URL is empty after trimming, returns nil — callers (router /
// middleware) treat nil as "mcn_ not supported on this deployment".
func NewCloudPATVerifier(cfg CloudPATVerifierConfig) *CloudPATVerifier {
	base := strings.TrimRight(strings.TrimSpace(cfg.FleetBaseURL), "/")
	if base == "" {
		return nil
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cloudPATDefaultTimeout}
	}
	return &CloudPATVerifier{
		baseURL: base,
		http:    client,
		rdb:     cfg.Redis,
	}
}

// Configured reports whether the verifier has a Fleet URL. Convenience
// for telemetry — a nil receiver also returns false. The middleware
// uses ordinary nil checks instead of this in hot paths.
func (v *CloudPATVerifier) Configured() bool {
	return v != nil && v.baseURL != ""
}

// Verify resolves token to a CloudPATIdentity by consulting (in order):
//
//  1. Redis cache, keyed by sha256(token). Hit → return cached
//     identity, no Fleet round-trip and no DB lookup. The cache only
//     ever contains identities that have already passed both Cloud's
//     verify AND the local owner-existence check, so a cache hit is
//     a fully-validated decision.
//  2. Fleet POST /api/v1/pat/verify. The response distinguishes:
//     - HTTP 200 + valid=true   → continues to step 3
//     - HTTP 200 + valid=false  → CloudPATInvalidError{Reason:...}
//     (also wraps as ErrCloudPATInvalid via Is)
//     - HTTP 4xx/5xx, network, timeout, decode failure
//     → ErrCloudPATUnavailable
//  3. Local owner-existence check via lookup(owner_id):
//     - exists  → cache + return success
//     - missing → ErrCloudPATInvalid (reason="owner_unknown"), NOT cached
//     - error   → ErrCloudPATUnavailable
//
// `lookup` may be nil — Verify then skips step 3. Production callers
// (Auth / DaemonAuth) always supply one; nil mode is for unit tests
// that exercise the verifier in isolation from the DB.
//
// We deliberately do NOT cache valid=false responses or
// owner_unknown rejections — per the Cloud doc, negative results can
// flip back to positive (lazy-revoke reconciliation, owner created
// later in our DB), and a stale negative would permanently lock out
// a freshly minted token within the TTL window.
//
// On a nil receiver returns ErrCloudPATNotConfigured so the
// middleware can map mcn_ tokens to 401 cleanly when the deployment
// has no Fleet URL.
func (v *CloudPATVerifier) Verify(ctx context.Context, token string, lookup OwnerLookupFunc) (CloudPATIdentity, error) {
	if v == nil || v.baseURL == "" {
		return CloudPATIdentity{}, ErrCloudPATNotConfigured
	}
	if token == "" {
		return CloudPATIdentity{}, ErrCloudPATInvalid
	}

	hash := HashToken(token)
	if id, ok := v.cacheGet(ctx, hash); ok {
		return id, nil
	}

	id, err := v.fetch(ctx, token)
	if err != nil {
		return CloudPATIdentity{}, err
	}

	if lookup != nil {
		exists, lookupErr := lookup(ctx, id.OwnerID)
		if lookupErr != nil {
			// Treat a DB / infrastructure error the same as a Cloud
			// outage: surface 503 so the caller retries instead of
			// throwing out a still-valid token. The cache is NOT
			// populated, so a transient blip resolves on the next
			// request.
			slog.Warn("cloud_pat: owner lookup failed; treating as unavailable", "error", lookupErr)
			return CloudPATIdentity{}, ErrCloudPATUnavailable
		}
		if !exists {
			// Cloud accepted the token, but the owner_id it returned
			// is not a user we know. Reject without caching — if the
			// user is created later, the next request must succeed
			// without waiting for the TTL.
			slog.Warn("cloud_pat: cloud-verified owner_id has no local user", "owner_id", id.OwnerID)
			return CloudPATIdentity{}, &CloudPATInvalidError{Reason: CloudPATInvalidReasonOwnerUnknown}
		}
	}

	v.cacheSet(ctx, hash, id)
	return id, nil
}

// fleetVerifyRequest mirrors the Cloud doc's request schema. We only
// send `token` today — `expected_owner_id` / `expected_instance_id`
// would let the verifier fail a token bound to a different user than
// the request claims, but at this layer we don't yet know the
// "claimed" user. Wiring those in is a future hardening step.
type fleetVerifyRequest struct {
	Token string `json:"token"`
}

// fleetVerifyResponse is the union response shape. `Valid` discriminates
// the two arms; on `valid:false` only `Reason` is meaningful (per the
// Cloud doc, mismatch responses deliberately omit binding info to
// avoid serving as a probing oracle).
type fleetVerifyResponse struct {
	Valid            bool   `json:"valid"`
	Reason           string `json:"reason,omitempty"`
	OwnerID          string `json:"owner_id,omitempty"`
	InstanceID       string `json:"instance_id,omitempty"`
	InstanceRecordID string `json:"instance_record_id,omitempty"`
}

func (v *CloudPATVerifier) fetch(ctx context.Context, token string) (CloudPATIdentity, error) {
	body, err := json.Marshal(fleetVerifyRequest{Token: token})
	if err != nil {
		// json.Marshal of a fixed struct with a string field cannot
		// realistically fail; surfacing as Unavailable keeps callers
		// on the "treat as cloud error" branch.
		return CloudPATIdentity{}, fmt.Errorf("%w: marshal request: %v", ErrCloudPATUnavailable, err)
	}
	if len(body) > cloudPATVerifyRequestMaxBytes {
		// Defense in depth: would only fire if a future change adds
		// expected_* fields and someone passes pathological input.
		return CloudPATIdentity{}, fmt.Errorf("%w: request body exceeds %d bytes", ErrCloudPATUnavailable, cloudPATVerifyRequestMaxBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+cloudPATVerifyPath, bytes.NewReader(body))
	if err != nil {
		return CloudPATIdentity{}, fmt.Errorf("%w: build request: %v", ErrCloudPATUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := v.http.Do(req)
	if err != nil {
		// Network error / timeout / DNS / dial failure. We deliberately
		// don't expose `err.Error()` to the HTTP response — let the
		// middleware emit a generic 503. Local logs still see the
		// real cause via the slog at the call site.
		slog.Warn("cloud_pat: verify request failed", "error", err)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Per the Cloud doc, 400 means our request was malformed and
		// 500 means Fleet itself is broken. Either way the right move
		// upstream is "treat the token as un-verifiable right now,
		// return 503". Read a small chunk of the body for log
		// context only.
		var snippet string
		if buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512)); len(buf) > 0 {
			snippet = strings.TrimSpace(string(buf))
		}
		slog.Warn("cloud_pat: verify returned non-200", "status", resp.StatusCode, "body", snippet)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, cloudPATVerifyResponseMaxBytes+1))
	if err != nil {
		slog.Warn("cloud_pat: read response failed", "error", err)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}
	if len(raw) > cloudPATVerifyResponseMaxBytes {
		slog.Warn("cloud_pat: verify response too large", "limit", cloudPATVerifyResponseMaxBytes)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	var parsed fleetVerifyResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		slog.Warn("cloud_pat: decode response failed", "error", err)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	if !parsed.Valid {
		// Surface the reason in the error so the middleware can log
		// "why" while still returning a generic 401 to the client.
		return CloudPATIdentity{}, &CloudPATInvalidError{Reason: parsed.Reason}
	}

	if parsed.OwnerID == "" {
		// Defense against a Fleet response that claims valid:true
		// but omits owner_id — without owner_id we have nothing to
		// put in X-User-ID, so it's effectively unusable. Fail
		// closed rather than passing an empty user id downstream.
		slog.Warn("cloud_pat: verify returned valid=true with empty owner_id")
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	return CloudPATIdentity{
		OwnerID:          parsed.OwnerID,
		InstanceID:       parsed.InstanceID,
		InstanceRecordID: parsed.InstanceRecordID,
	}, nil
}

func cloudPATCacheKey(hash string) string { return cloudPATCachePrefix + hash }

func (v *CloudPATVerifier) cacheGet(ctx context.Context, hash string) (CloudPATIdentity, bool) {
	if v == nil || v.rdb == nil {
		return CloudPATIdentity{}, false
	}
	raw, err := v.rdb.Get(ctx, cloudPATCacheKey(hash)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("cloud_pat: cache get failed; falling back to fleet", "error", err)
		}
		return CloudPATIdentity{}, false
	}
	var id CloudPATIdentity
	if err := json.Unmarshal(raw, &id); err != nil {
		slog.Warn("cloud_pat: cache entry malformed; falling back to fleet", "error", err)
		return CloudPATIdentity{}, false
	}
	if id.OwnerID == "" {
		// Safety net: a malformed cache entry (e.g. left over from a
		// prior schema) without an owner_id must not be treated as a
		// hit, otherwise the middleware would set X-User-ID to "".
		return CloudPATIdentity{}, false
	}
	return id, true
}

func (v *CloudPATVerifier) cacheSet(ctx context.Context, hash string, id CloudPATIdentity) {
	if v == nil || v.rdb == nil {
		return
	}
	raw, err := json.Marshal(id)
	if err != nil {
		slog.Warn("cloud_pat: cache marshal failed", "error", err)
		return
	}
	if err := v.rdb.Set(ctx, cloudPATCacheKey(hash), raw, cloudPATCacheTTL).Err(); err != nil {
		slog.Warn("cloud_pat: cache set failed", "error", err)
	}
}
