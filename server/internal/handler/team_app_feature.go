package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/entitlement"
	composio "github.com/multica-ai/multica/server/internal/integrations/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Paid feature team_app_accounts (MUL-9491): team Apps accounts, their use
// by agents, and custom team rules.
//
// Additions: connecting a team account for an app that has none (including
// after a disconnect), giving an agent a team-account app or switching one
// from a personal account, raising an agent's level on a team account, and
// setting a team rule to a value that is neither its current value nor the
// default.
//
// Always allowed: reauthorizing an app's existing team account, disconnecting,
// removing an app from an agent, lowering a level, restoring default rules,
// and everything about personal accounts. The gateway (Decide) never consults
// the feature, so existing team accounts, agent apps and block rules keep
// working after a downgrade.

const (
	teamAppOpConnect     = "connect_team_account"
	teamAppOpAgentApp    = "add_agent_team_app"
	teamAppOpRaiseLevel  = "raise_agent_team_level"
	teamAppOpChangeRules = "change_team_rules"
)

// teamAppHasAccount reports whether the workspace already has a team account
// for slug that is not revoked. Connecting again then reauthorizes it: a
// workspace keeps one team account per app, so the count never grows.
// Multica does not store the external identity, so switching to another
// external account on reauthorization is indistinguishable and allowed.
func (h *Handler) teamAppHasAccount(ctx context.Context, workspaceID pgtype.UUID, slug string) (bool, error) {
	rows, err := h.Queries.ListWorkspaceComposioConnections(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.ToolkitSlug == slug {
			return true, nil
		}
	}
	if h.ConnectedApps == nil {
		return false, nil
	}
	if _, remote := h.ConnectedApps.RemoteApp(slug); !remote {
		return false, nil
	}
	remoteRows, err := h.Queries.ListRemoteAppConnectionsForOwner(ctx, db.ListRemoteAppConnectionsForOwnerParams{
		Scope: connectedapps.ScopeTeam, OwnerID: workspaceID,
	})
	if err != nil {
		return false, err
	}
	for _, row := range remoteRows {
		if row.AppSlug == slug {
			return true, nil
		}
	}
	return false, nil
}

// TeamAppConnectGate is the composio.TeamConnectGate for team_app_accounts.
// It runs when a completed handshake is about to write a team account, so a
// connect started before a downgrade, or one that raced another connect,
// cannot add an account the plan no longer includes. A refusal keeps the
// existing connection. An observed connect is recorded only after the write.
func (h *Handler) TeamAppConnectGate(ctx context.Context, workspaceID pgtype.UUID, slug string) (func(context.Context), error) {
	feature := h.paidFeature(ctx, workspaceID, entitlement.FeatureTeamAppAccounts)
	if !feature.active {
		return func(context.Context) {}, nil
	}
	reconnect, err := h.teamAppHasAccount(ctx, workspaceID, slug)
	if err != nil {
		// The restriction is commercial and fails open, like every other
		// entitlement failure.
		slog.Warn("team app connect gate: list team accounts failed; admitting", "error", err)
		return func(context.Context) {}, nil
	}
	if reconnect {
		return func(context.Context) {}, nil
	}
	admission, ok := feature.addition(ctx, teamAppOpConnect)
	if !ok {
		return nil, composio.ErrTeamConnectRefused
	}
	return admission.admitted, nil
}

// agentLevelRank orders agent levels by what they let the agent do. The
// gateway runs confirm_writes as write, so the two rank the same.
func agentLevelRank(level string) int {
	if level == connectedapps.LevelRead {
		return 0
	}
	return 1
}

// agentTeamAppAdds classifies one wanted agent app against the agent's
// current row for the same app. It returns the operation that adds to
// team_app_accounts, or "" when the change does not add: personal apps,
// unchanged team apps, and lowered levels.
func agentTeamAppAdds(prev db.AgentApp, had bool, scope, level string) string {
	if scope != connectedapps.ScopeTeam {
		return ""
	}
	if !had || prev.AccountScope != connectedapps.ScopeTeam {
		return teamAppOpAgentApp
	}
	if agentLevelRank(level) > agentLevelRank(prev.Level) {
		return teamAppOpRaiseLevel
	}
	return ""
}

// teamRulesAdd reports whether a team rules write sets any class to a value
// that is neither its current value (custom or default) nor the default.
// Same-value write-backs and restoring defaults (Allow all actions) are not
// additions.
func teamRulesAdd(slug string, current map[string]string, rules []appRuleInput) bool {
	for _, rule := range rules {
		def := connectedapps.DefaultPolicy(slug, rule.ActionClass)
		now, custom := current[rule.ActionClass]
		if !custom {
			now = def
		}
		if rule.Policy != now && rule.Policy != def {
			return true
		}
	}
	return false
}

// appRuleInput is one rule in a PUT /api/apps/rules body.
type appRuleInput struct {
	ActionClass string `json:"action_class"`
	Policy      string `json:"policy"`
}
