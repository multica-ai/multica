package handler

// FIR-2037 — claim-time feature gate + capability key for the in-app PERSONAL
// BROWSER.
//
// The personal browser is a Multica-owned Chromium pane that lives inside the
// desktop app (apps/desktop/src/main/cerebro-browser-pane.ts). A runtime agent
// drives it through the `multica cerebro-browser` CLI, which reaches the
// desktop app's loopback control server. At claim time the daemon injects
// MULTICA_PERSONAL_BROWSER=1 when the feature is switched ON for the workspace
// (the kill switch); the CLI fast-fails without it.
//
// The AUTHORITATIVE, per-agent, per-host decision is NOT here — it is per action
// in AuthorizePersonalBrowser (personal_browser_authorize_cerebro.go), which
// resolves the unified tool-policy chain with Base=Deny + the target host so a
// host-allowlist condition bites. That split is deliberate: a host-conditioned
// grant cannot be evaluated at claim time (no host is known yet), so the
// claim-time env tracks the feature flag, not the capability.
//
// This is a cerebro-zone file (`_cerebro.go`), so it needs no per-line
// CEREBRO-PATCH markers; the only upstream touch is the one-line call to
// withPersonalBrowserEnv in ClaimTaskByRuntime.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// personalBrowserToolKey is the capability key the personal browser is
// registered under. capabilities.For("claude").Tools lists "personal-browser",
// which the capability mirror writes to cerebro_capability as
// "tools:personal-browser" (same shape PolicyToolKey produces). The per-action
// gate (AuthorizePersonalBrowser) resolves the chain on this key.
const personalBrowserToolKey = "tools:personal-browser"

// personalBrowserGrantEnv is the env var the daemon injects into a spawned
// agent when the feature is enabled for its workspace. The `multica
// cerebro-browser` CLI fast-fails without it; the real authorization is the
// per-action server gate.
const personalBrowserGrantEnv = "MULTICA_PERSONAL_BROWSER"

// personalBrowserFeatureFlag is the workspace kill switch for the whole personal
// browser feature (the renderer gates the Browser tab on it too). It is the right
// thing to key the claim-time env on — NOT the per-agent capability — because the
// authoritative, per-agent, per-host decision now happens per action in
// AuthorizePersonalBrowser. Keying the claim-time env on the capability would be
// wrong: a host-conditioned grant ("allow only on example.com") resolves to Deny
// when checked with no host, so the agent would never even get the env to try.
const personalBrowserFeatureFlag = "cerebro_browser"

// personalBrowserFeatureEnabled resolves the feature for the agent owner using
// the same precedence as the UI: a locked workspace value wins, then a personal
// override, then an unlocked workspace default. Missing rows and lookup errors
// keep the default-OFF feature sealed.
func (h *Handler) personalBrowserFeatureEnabled(ctx context.Context, wsID, ownerID pgtype.UUID) bool {
	workspaceRows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, wsID)
	if err != nil {
		return false
	}
	var workspaceEnabled, workspaceSet, workspaceLocked bool
	for _, row := range workspaceRows {
		if row.FlagKey != personalBrowserFeatureFlag {
			continue
		}
		workspaceEnabled, workspaceSet, workspaceLocked = row.Enabled, true, row.Locked
		break
	}
	if workspaceLocked {
		return workspaceEnabled
	}

	personalEnabled, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      ownerID,
		FlagKey:     personalBrowserFeatureFlag,
	})
	if err == nil {
		return personalEnabled
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	return workspaceSet && workspaceEnabled
}

// withPersonalBrowserEnv returns the agent's custom-env map with the personal
// browser grant merged in when, and only when, the feature flag is ON for the
// workspace. The env is a "feature reachable, use the per-action gate" signal the
// CLI fast-fails on when absent — it is NOT the authorization decision (that is
// per action, per host, Base=Deny, in AuthorizePersonalBrowser). A nil input map
// is allocated lazily only when the grant is added, so the off path keeps nil nil.
func (h *Handler) withPersonalBrowserEnv(ctx context.Context, env map[string]string, wsID, ownerID pgtype.UUID) map[string]string {
	if !h.personalBrowserFeatureEnabled(ctx, wsID, ownerID) {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	env[personalBrowserGrantEnv] = "1"
	return env
}
