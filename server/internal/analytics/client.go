// Package analytics ships product telemetry events to an external analytics
// backend (Amplitude). Events feed the acquisition → activation → expansion
// funnel — see docs/analytics.md for the event contract.
//
// Design:
//   - Capture is non-blocking. Request handlers must never wait on analytics
//     network I/O, so we enqueue into a bounded channel and a background
//     worker flushes to Amplitude in batches.
//   - When the queue is full events are dropped (and counted). A broken
//     analytics backend must never degrade the product.
//   - When AMPLITUDE_API_KEY is empty the package runs a no-op client, which
//     keeps local dev and self-hosted instances friction-free.
package analytics

import (
	"log/slog"
	"os"
	"time"
)

// Event is a single analytics capture. Fields mirror Amplitude's event shape
// but are framework-agnostic so alternate backends can plug in later.
type Event struct {
	// Name of the event (e.g. "signup", "story_created").
	Name string

	// DistinctID identifies the person this event belongs to. For logged-in
	// users this is user.id; maps to Amplitude's user_id.
	DistinctID string

	// WorkspaceID scopes the event to a workspace. Required when the event is
	// about a workspace-level action (workspace_created, story_created, ...).
	// Empty is allowed for pre-workspace events (signup).
	WorkspaceID string

	// Properties is the free-form bag of event attributes. Only serialisable
	// values (string, number, bool, nested maps/slices of the same) should
	// go here. Never put raw PII like full emails here — use email_domain.
	Properties map[string]any

	// SetOnce properties attach to the user record and are only written the
	// first time they appear. Use this for acquisition attribution
	// (initial_utm_source, etc.) so later events don't overwrite the origin.
	// Maps to Amplitude's $setOnce user property operation.
	SetOnce map[string]any

	// Set properties attach to the user record and overwrite on every write.
	// Use this for mutable cohort signals (role, use_case, platform_preference)
	// that users can legitimately change during onboarding.
	// Maps to Amplitude's $set user property operation.
	Set map[string]any

	// Timestamp is optional; when zero the client fills in time.Now().
	Timestamp time.Time
}

// Client is the narrow surface the rest of the codebase depends on. Handlers
// call Capture and move on; the implementation is responsible for buffering,
// batching, and shipping.
type Client interface {
	Capture(e Event)
	// Close drains pending events. Call once during graceful shutdown.
	Close()
}

// NewFromEnv returns a Client configured from environment variables:
//
//   - AMPLITUDE_API_KEY: project API key. Empty → no-op client.
//   - AMPLITUDE_HOST:    API host (default https://api2.amplitude.com).
//   - ANALYTICS_DISABLED: set to "true"/"1" to force a no-op client even
//     when AMPLITUDE_API_KEY is set (useful for CI and self-hosted opt-out).
func NewFromEnv() Client {
	if isDisabled() {
		slog.Info("analytics disabled via ANALYTICS_DISABLED")
		return NoopClient{}
	}
	key := os.Getenv("AMPLITUDE_API_KEY")
	if key == "" {
		slog.Info("analytics: AMPLITUDE_API_KEY not set, using noop client")
		return NoopClient{}
	}
	host := os.Getenv("AMPLITUDE_HOST")
	if host == "" {
		host = "https://api2.amplitude.com"
	}
	slog.Info("analytics: amplitude client enabled", "host", host)
	return NewAmplitudeClient(AmplitudeConfig{APIKey: key, Host: host})
}

func isDisabled() bool {
	v := os.Getenv("ANALYTICS_DISABLED")
	return v == "true" || v == "1"
}

// NoopClient silently drops all events. Used in tests, in local dev when
// AMPLITUDE_API_KEY is unset, and in self-hosted instances that opt out.
type NoopClient struct{}

func (NoopClient) Capture(Event) {}
func (NoopClient) Close()        {}
