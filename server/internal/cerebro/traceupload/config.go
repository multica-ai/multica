// Package traceupload makes the Multica daemon send the raw JSONL transcript
// of every finished task up to the registry's agent-trace ingest endpoint
// (POST /api/agent-traces/ingest, built in Fase 1 / firtal-data-registry
// PR #63).
//
// Design in one paragraph: at task close the daemon builds a Sidecar from the
// task context, locates the runtime's session JSONL on disk, and records a
// pending entry in a persistent Ledger. A background Manager uploads each
// pending entry (gzip(meta-line + raw JSONL) → POST with x-api-key), marks the
// ledger row ingested on HTTP 200, and retries with backoff on failure. On
// daemon boot the Manager replays every non-ingested ledger row (the
// boot-sweep), so a crash, restart, or an upload that failed while the
// endpoint was down is always caught next time. Uploading is best-effort and
// MUST never fail the task: enqueue errors are swallowed, the JSONL simply
// stays unmarked in the ledger.
//
// Everything here is gated behind MULTICA_TRACE_UPLOAD_ENABLED (default off).
// With the flag off the daemon never constructs a Manager, so not a single
// HTTP call or disk write happens.
package traceupload

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the trace-upload settings resolved from the environment.
type Config struct {
	// Enabled is the master feature flag (MULTICA_TRACE_UPLOAD_ENABLED).
	// Default false until the registry endpoint is live.
	Enabled bool

	// Endpoint is the full URL of the registry ingest endpoint, e.g.
	// https://registry.example.com/api/agent-traces/ingest.
	Endpoint string

	// APIKey is the system/unrestricted registry API key sent as x-api-key.
	APIKey string

	// StateDir is where the ledger file lives. Defaults to
	// <WorkspacesRoot>/.trace-upload.
	StateDir string

	// MaxConcurrentPerWorkspace caps simultaneous uploads for a single
	// workspace so one busy workspace can't saturate the registry (F2.9).
	MaxConcurrentPerWorkspace int

	// MaxAge is the dead-letter cutoff: a ledger entry that has not been
	// confirmed ingested within this window is parked as dead_letter and an
	// alarm is logged (F2.6). Default 7 days.
	MaxAge time.Duration

	// Backoff is the per-attempt retry schedule (F2.6). Attempt N uses
	// Backoff[min(N, len-1)]. Default 1s/5s/30s.
	Backoff []time.Duration
}

// Env var names. Exported so the daemon config layer and tests share them.
const (
	EnvEnabled        = "MULTICA_TRACE_UPLOAD_ENABLED"
	EnvEndpoint       = "MULTICA_TRACE_UPLOAD_ENDPOINT"
	EnvAPIKey         = "MULTICA_TRACE_UPLOAD_API_KEY"
	EnvMaxConcurrency = "MULTICA_TRACE_UPLOAD_MAX_CONCURRENCY"
	EnvMaxAgeHours    = "MULTICA_TRACE_UPLOAD_MAX_AGE_HOURS"
)

// DefaultBackoff is the exponential retry schedule from the issue (1s/5s/30s).
var DefaultBackoff = []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

// DefaultMaxAge is the dead-letter cutoff from the issue (7 days).
const DefaultMaxAge = 7 * 24 * time.Hour

// DefaultMaxConcurrentPerWorkspace bounds parallel uploads per workspace.
const DefaultMaxConcurrentPerWorkspace = 2

// ConfigFromEnv reads the trace-upload settings from the environment.
// workspacesRoot supplies the default StateDir location. The returned config
// is always valid to hand to NewManager; NewManager itself decides whether to
// do anything based on Enabled + Endpoint + APIKey.
func ConfigFromEnv(workspacesRoot string) Config {
	c := Config{
		Enabled:                   parseBool(os.Getenv(EnvEnabled)),
		Endpoint:                  strings.TrimSpace(os.Getenv(EnvEndpoint)),
		APIKey:                    strings.TrimSpace(os.Getenv(EnvAPIKey)),
		MaxConcurrentPerWorkspace: DefaultMaxConcurrentPerWorkspace,
		MaxAge:                    DefaultMaxAge,
		Backoff:                   DefaultBackoff,
	}
	if workspacesRoot != "" {
		c.StateDir = workspacesRoot + string(os.PathSeparator) + ".trace-upload"
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxConcurrency)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxConcurrentPerWorkspace = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxAgeHours)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxAge = time.Duration(n) * time.Hour
		}
	}
	return c
}

// ConfigFromValues builds a Config from explicit endpoint/key values supplied
// by the server's trace-config endpoint (TECH-3621 fleet rollout) instead of
// the local environment. This is the path that turns trace upload on for the
// whole fleet from a single server-side setting, without the ingest key ever
// landing in plaintext on each machine's daemon env. workspacesRoot supplies
// the StateDir; maxAgeHours <= 0 keeps DefaultMaxAge. Enabled is forced true so
// NewManager runs — callers MUST only call this when endpoint and apiKey are
// both non-empty (an empty pair means the server has the feature off).
func ConfigFromValues(workspacesRoot, endpoint, apiKey string, maxAgeHours int) Config {
	c := Config{
		Enabled:                   true,
		Endpoint:                  strings.TrimSpace(endpoint),
		APIKey:                    strings.TrimSpace(apiKey),
		MaxConcurrentPerWorkspace: DefaultMaxConcurrentPerWorkspace,
		MaxAge:                    DefaultMaxAge,
		Backoff:                   DefaultBackoff,
	}
	if workspacesRoot != "" {
		c.StateDir = workspacesRoot + string(os.PathSeparator) + ".trace-upload"
	}
	if maxAgeHours > 0 {
		c.MaxAge = time.Duration(maxAgeHours) * time.Hour
	}
	return c
}

// parseBool treats 1/true/yes/on (case-insensitive) as true; everything else
// (including empty) is false, so the flag is off unless explicitly enabled.
func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
