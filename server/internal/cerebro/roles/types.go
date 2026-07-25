package roles

import (
	"encoding/json"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

type roleResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     int32           `json:"version"`
	Permissions json.RawMessage `json:"permissions"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedBy   *string         `json:"created_by"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type roleAssignmentResponse struct {
	RoleID             string  `json:"role_id"`
	SubjectType        string  `json:"subject_type"`
	SubjectID          string  `json:"subject_id"`
	SubjectDisplayName *string `json:"subject_display_name"`
	AddedBy            *string `json:"added_by"`
	AddedAt            string  `json:"added_at"`
	ExpiresAt          *string `json:"expires_at"`
}

func roleResponseFromModel(role cerebrodb.CerebroRole) roleResponse {
	permissions := json.RawMessage(role.Permissions)
	if len(permissions) == 0 {
		permissions = json.RawMessage(`{}`)
	}
	return roleResponse{
		ID:          util.UUIDToString(role.ID),
		WorkspaceID: util.UUIDToString(role.WorkspaceID),
		Name:        role.Name,
		Description: role.Description,
		Version:     role.Version,
		Permissions: permissions,
		ArchivedAt:  util.TimestampToPtr(role.ArchivedAt),
		CreatedBy:   util.UUIDToPtr(role.CreatedBy),
		CreatedAt:   util.TimestampToString(role.CreatedAt),
		UpdatedAt:   util.TimestampToString(role.UpdatedAt),
	}
}

func roleAssignmentResponseFromAssign(row cerebrodb.AssignCerebroRoleRow) roleAssignmentResponse {
	return roleAssignmentResponse{
		RoleID:      util.UUIDToString(row.RoleID),
		SubjectType: row.SubjectType,
		SubjectID:   util.UUIDToString(row.SubjectID),
		AddedBy:     util.UUIDToPtr(row.AddedBy),
		AddedAt:     util.TimestampToString(row.AddedAt),
		ExpiresAt:   util.TimestampToPtr(row.ExpiresAt),
	}
}

func roleAssignmentResponseFromModel(row cerebrodb.CerebroRoleAssignment) roleAssignmentResponse {
	return roleAssignmentResponse{
		RoleID:      util.UUIDToString(row.RoleID),
		SubjectType: row.SubjectType,
		SubjectID:   util.UUIDToString(row.SubjectID),
		AddedBy:     util.UUIDToPtr(row.AddedBy),
		AddedAt:     util.TimestampToString(row.AddedAt),
		ExpiresAt:   util.TimestampToPtr(row.ExpiresAt),
	}
}

// roleAssignmentResponseFromNamedRow maps the display row that carries the
// resolved subject name. An empty name (subject deleted) is sent as null so
// the client falls back to its own label.
func roleAssignmentResponseFromNamedRow(row cerebrodb.ListCerebroRoleAssignmentsWithNamesRow) roleAssignmentResponse {
	var displayName *string
	if row.SubjectDisplayName != "" {
		name := row.SubjectDisplayName
		displayName = &name
	}
	return roleAssignmentResponse{
		RoleID:             util.UUIDToString(row.RoleID),
		SubjectType:        row.SubjectType,
		SubjectID:          util.UUIDToString(row.SubjectID),
		SubjectDisplayName: displayName,
		AddedBy:            util.UUIDToPtr(row.AddedBy),
		AddedAt:            util.TimestampToString(row.AddedAt),
		ExpiresAt:          util.TimestampToPtr(row.ExpiresAt),
	}
}

// RolePermissions is the durable profile payload consumed directly by the
// tool-policy resolver. Each capability may have several resource/condition
// scoped rules; the API never flattens those into a broader whole-tool grant.
type RolePermissions map[string][]RolePermissionRule

type RolePermissionRule struct {
	Setting         string                `json:"setting"`
	ResourcePattern string                `json:"resource_pattern"`
	Conditions      *toolpolicy.Condition `json:"conditions"`
}
