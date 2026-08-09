package runtimeaccess

import db "github.com/multica-ai/multica/server/pkg/db/generated"

// CanUse reports whether member may place work on runtime. A valid Workspace
// membership is always required; elevated Workspace roles, Runtime ownership,
// and public visibility are the only grants.
func CanUse(member db.Member, runtime db.AgentRuntime) bool {
	if !member.UserID.Valid || !member.WorkspaceID.Valid || !runtime.WorkspaceID.Valid || member.WorkspaceID != runtime.WorkspaceID {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	if runtime.OwnerID.Valid && member.UserID == runtime.OwnerID {
		return true
	}
	return runtime.Visibility == "public"
}
