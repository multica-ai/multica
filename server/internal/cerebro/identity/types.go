// Package identity holds the cerebro Google-identity integration: per-
// workspace auto-signup-domain + default-role settings and the hook that
// auto-creates a workspace membership when a matching email lands.
//
// FIR-2523.
package identity

import (
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// allowedRoles is the set of role strings that may be stored as the
// default_role for auto-provisioning. Mirrors normalizeMemberRole in the
// upstream workspace handler — kept inline here so a cerebro release can add
// new cerebro-only roles without round-tripping through upstream.
var allowedRoles = map[string]struct{}{
	"owner":  {},
	"admin":  {},
	"member": {},
}

// AuthSettings is the API-facing view of cerebro_workspace_auth_settings.
type AuthSettings struct {
	WorkspaceID                string   `json:"workspace_id"`
	GoogleSignupDomains        []string `json:"google_signup_domains"`
	DefaultRole                string   `json:"default_role"`
	// DefaultGroupID is the cerebro group every new workspace member is placed
	// into (auto-signup, invitation accept, manual member add). Null when the
	// workspace has not configured a fallback yet.
	DefaultGroupID             *string  `json:"default_group_id"`
	GoogleWorkspaceSyncEnabled bool     `json:"google_workspace_sync_enabled"`
	UpdatedAt                  string   `json:"updated_at"`
	UpdatedByUserID            *string  `json:"updated_by_user_id"`
}

// UpdateAuthSettingsRequest is the body for `PUT /api/cerebro/workspaces/
// {id}/auth-settings`.
type UpdateAuthSettingsRequest struct {
	GoogleSignupDomains []string `json:"google_signup_domains"`
	DefaultRole         string   `json:"default_role"`
	// DefaultGroupID sets which group new members are auto-assigned to. When
	// non-nil, the pointer value must be a group UUID in this workspace.
	DefaultGroupID *string `json:"default_group_id"`
	// GoogleWorkspaceSyncEnabled turns the FIR-2596 BigQuery group sync on or
	// off for this workspace. Default false: the sync worker never touches a
	// workspace until an owner/admin opts in here.
	GoogleWorkspaceSyncEnabled bool `json:"google_workspace_sync_enabled"`
}

func toAuthSettings(row cerebrodb.CerebroWorkspaceAuthSetting) AuthSettings {
	domains := row.GoogleSignupDomains
	if domains == nil {
		domains = []string{}
	}
	out := AuthSettings{
		WorkspaceID:                util.UUIDToString(row.WorkspaceID),
		GoogleSignupDomains:        domains,
		DefaultRole:                row.DefaultRole,
		GoogleWorkspaceSyncEnabled: row.GoogleWorkspaceSyncEnabled,
		UpdatedAt:                  row.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if row.UpdatedByUserID.Valid {
		s := util.UUIDToString(row.UpdatedByUserID)
		out.UpdatedByUserID = &s
	}
	return out
}

// defaultAuthSettings is what `GET` returns for a workspace that has never
// saved settings — empty domain list and the safest default role.
func defaultAuthSettings(workspaceID string) AuthSettings {
	return AuthSettings{
		WorkspaceID:         workspaceID,
		GoogleSignupDomains: []string{},
		DefaultRole:         "member",
		UpdatedAt:           "",
		UpdatedByUserID:     nil,
	}
}
