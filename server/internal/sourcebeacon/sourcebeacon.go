// Package sourcebeacon implements the self-host onboarding source beacon
// (MUL-3708).
//
// Goal: let Multica see the anonymous "where did you hear about us"
// (onboarding source) distribution from production self-hosted instances —
// which today is invisible because self-host runs with no PostHog key and
// ships nothing.
//
// Shape: this is NOT a background telemetry pipeline. When a user fills in
// their onboarding source, a production self-host *server* fires one
// fire-and-forget HTTP beacon to Multica's public, write-only ingest. The
// official cloud keeps its existing PostHog capture unchanged and never
// fires the beacon.
//
// Privacy contract — only ever leaves a self-host instance:
//   - the selected source channel enum value(s);
//   - uid_hash  = sha256(instance_salt + user_id), truncated;
//   - instance_hash = sha256(instance_salt), truncated.
//
// The instance_salt is a per-instance secret that never leaves the box, so
// Multica cannot reverse a hash back to a user_id, and the same user on two
// different self-host instances hashes differently (no cross-instance
// correlation). Real user_id / email / name / workspace / org / domain /
// role / use_case / team_size and the source_other free-text are NEVER
// part of the payload.
package sourcebeacon

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/analytics"
)

// SchemaVersion is the wire version of the beacon payload. Bump only with a
// matching ingest change.
const SchemaVersion = 1

// MaxChannelsPerRequest caps how many channels a single beacon may carry.
// The source enum has 13 members; the cap is headroom + an abuse bound on
// the public ingest.
const MaxChannelsPerRequest = 16

// MaxBodyBytes bounds the ingest request body. The payload is a tiny fixed
// JSON; 4 KiB is ~10x the realistic ceiling and keeps the public endpoint
// from being used as bulk storage.
const MaxBodyBytes = 4 << 10

// Payload is the exact, closed shape accepted by the public ingest. The
// ingest decodes it with DisallowUnknownFields, so any extra field (e.g. a
// leaked email or source_other) is rejected outright.
type Payload struct {
	V            int      `json:"v"`
	Channels     []string `json:"channels"`
	UIDHash      string   `json:"uid_hash"`
	InstanceHash string   `json:"instance_hash"`
}

// validChannels mirrors the `Source` enum in
// packages/core/onboarding/types.ts. Keep the two in sync — there is no
// shared source of truth across the Go/TS boundary yet (known tech debt).
var validChannels = map[string]struct{}{
	"friends_colleagues": {},
	"search":             {},
	"social_x":           {},
	"social_linkedin":    {},
	"social_youtube":     {},
	"social_github":      {},
	"social_other":       {},
	"blog_newsletter":    {},
	"ai_assistant":       {},
	"from_work":          {},
	"event_conference":   {},
	"dont_remember":      {},
	"other":              {},
}

// IsValidChannel reports whether c is a known source channel enum value.
func IsValidChannel(c string) bool {
	_, ok := validChannels[c]
	return ok
}

// FilterValidChannels drops unknown channels, de-duplicates, and caps the
// result at MaxChannelsPerRequest. Used on both the sending and receiving
// side (defense in depth).
func FilterValidChannels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, c := range in {
		if !IsValidChannel(c) {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
		if len(out) >= MaxChannelsPerRequest {
			break
		}
	}
	return out
}

// HashUID returns the truncated sha256 of (instance_salt + user_id). The
// salt stays on the instance, so this is one-way for Multica.
func HashUID(salt, userID string) string {
	sum := sha256.Sum256([]byte(salt + userID))
	return hex.EncodeToString(sum[:])[:32]
}

// HashInstance returns the truncated sha256 of the instance_salt. Stable per
// instance; used for grouping and the dedup key.
func HashInstance(salt string) string {
	sum := sha256.Sum256([]byte(salt))
	return hex.EncodeToString(sum[:])[:32]
}

// IsValidHash bounds an inbound hash to lowercase hex of a sane length. The
// sender always emits 32 hex chars; the range stays lenient so a future
// truncation change doesn't break ingest.
func IsValidHash(s string) bool {
	if len(s) < 16 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// beaconNamespace is a fixed namespace for deriving deterministic event
// UUIDs (UUIDv5). Stable across releases — changing it would break PostHog
// dedup for already-ingested events.
var beaconNamespace = uuid.MustParse("b1e7c0de-5a17-4f05-9c0a-5e1f0a7d3c21")

// EventUUID is the deterministic PostHog event uuid for one
// (instance_hash, uid_hash, channel) tuple. Re-sending the same tuple
// yields the same uuid, so PostHog deduplicates it (best-effort) — the
// reason the dedup key includes channel: a multi-select user produces one
// stable event per channel.
func EventUUID(instanceHash, uidHash, channel string) string {
	return uuid.NewSHA1(beaconNamespace, []byte(instanceHash+":"+uidHash+":"+channel)).String()
}

// ShouldSendInput is the explicit (testable) input to ShouldSend.
type ShouldSendInput struct {
	AnalyticsDisabled bool
	// Environment is the normalized value from analytics.EnvironmentFromEnv
	// ("production" / "staging" / "dev").
	Environment string
	// AppHost is the canonical host of the deployment's own frontend/app
	// URL (MULTICA_APP_URL, falling back to FRONTEND_ORIGIN).
	AppHost string
}

// ShouldSend decides whether THIS deployment should emit the beacon. It is
// fail-closed: anything ambiguous returns false. We judge by the
// deployment's own frontend/app host — NOT the backend URL — because the
// official cloud reliably configures its frontend domain even when
// MULTICA_PUBLIC_URL is unset (there is a regression test for exactly that),
// so keying on the backend URL would misclassify official as self-host and
// pollute production analytics.
func ShouldSend(in ShouldSendInput) bool {
	if in.AnalyticsDisabled {
		return false
	}
	if in.Environment != "production" {
		return false
	}
	if isLocalHost(in.AppHost) {
		return false
	}
	if isManagedHost(in.AppHost) {
		return false
	}
	return true
}

// ShouldSendFromEnv evaluates ShouldSend against the process environment.
func ShouldSendFromEnv() bool {
	return ShouldSend(ShouldSendInput{
		AnalyticsDisabled: analyticsDisabled(),
		Environment:       analytics.EnvironmentFromEnv(),
		AppHost:           AppHostFromEnv(),
	})
}

// AppHostFromEnv resolves the deployment's frontend/app host the same way
// /api/config's official-cloud check does: MULTICA_APP_URL, then
// FRONTEND_ORIGIN.
func AppHostFromEnv() string {
	if h := canonicalHost(os.Getenv("MULTICA_APP_URL")); h != "" {
		return h
	}
	return canonicalHost(os.Getenv("FRONTEND_ORIGIN"))
}

func analyticsDisabled() bool {
	v := os.Getenv("ANALYTICS_DISABLED")
	return v == "true" || v == "1"
}

// canonicalHost extracts a lowercased, port-stripped hostname from a URL or
// bare host string. Empty on parse failure.
func canonicalHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

func isLocalHost(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasSuffix(host, ".localhost")
}

// isManagedHost reports whether host belongs to Multica's managed cloud.
// Matches multica.ai and ANY *.multica.ai subdomain so official prod,
// staging, preview, and internal envs are all excluded without enumerating
// each one — no self-host can live on a multica.ai subdomain, so the suffix
// match is safe. (This is intentionally broader than the exact-host
// isOfficialCloudDaemonConfig check, which serves a different purpose.)
func isManagedHost(host string) bool {
	return host == "multica.ai" || strings.HasSuffix(host, ".multica.ai")
}
