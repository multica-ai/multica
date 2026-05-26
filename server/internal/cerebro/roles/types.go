package roles

import (
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type roleResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type roleAssignmentResponse struct {
	RoleID             string  `json:"role_id"`
	SubjectType        string  `json:"subject_type"`
	SubjectID          string  `json:"subject_id"`
	SubjectDisplayName *string `json:"subject_display_name"`
	AddedBy            *string `json:"added_by"`
	AddedAt            string  `json:"added_at"`
}

func roleResponseFromModel(role cerebrodb.CerebroRole) roleResponse {
	return roleResponse{
		ID:          util.UUIDToString(role.ID),
		WorkspaceID: util.UUIDToString(role.WorkspaceID),
		Name:        role.Name,
		Description: role.Description,
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
	}
}

func roleAssignmentResponseFromModel(row cerebrodb.CerebroRoleAssignment) roleAssignmentResponse {
	return roleAssignmentResponse{
		RoleID:      util.UUIDToString(row.RoleID),
		SubjectType: row.SubjectType,
		SubjectID:   util.UUIDToString(row.SubjectID),
		AddedBy:     util.UUIDToPtr(row.AddedBy),
		AddedAt:     util.TimestampToString(row.AddedAt),
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
	}
}
