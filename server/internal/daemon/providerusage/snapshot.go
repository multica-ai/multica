// Package providerusage reads plan limits from CLIs and editors that are
// already signed in on the daemon machine. It returns derived snapshots
// only. Tokens, cookies, and auth.json contents never leave this package.
package providerusage

import "time"

const (
	ProviderClaude      = "claude"
	ProviderCursor      = "cursor"
	ProviderCodex       = "codex"
	ProviderCopilot     = "copilot"
	ProviderAntigravity = "antigravity"
	ProviderGrok        = "grok"
	ProviderKimi        = "kimi"
	ProviderKiro        = "kiro"
	ProviderOpenCode    = "opencode"

	ReasonNotLoggedIn        = "not_logged_in"
	ReasonAPIKeyOnly         = "api_key_only"
	ReasonUnauthorized       = "unauthorized"
	ReasonCLIUnavailable     = "cli_unavailable"
	ReasonSessionUnavailable = "session_unavailable"
	ReasonUnsupported        = "unsupported"
)

// Window is one vendor limit bucket, already reduced to the fields the
// server is allowed to store.
type Window struct {
	ID          string
	PercentUsed float64
	ResetsAt    *time.Time
}

// Snapshot is the upload payload for one provider. ReasonCode is set only
// when Windows is empty: a normal absence, not a task failure.
type Snapshot struct {
	Provider    string
	PlanName    string
	CollectedAt time.Time
	ReasonCode  string
	Windows     []Window
}

// Result tells the poller whether to replace the server's copy. Upload is
// false for a transient failure so the last good snapshot stays put.
type Result struct {
	Snapshot Snapshot
	Upload   bool
	Backoff  time.Duration
}
