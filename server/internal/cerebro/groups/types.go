package groups

import (
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type groupResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type groupMemberResponse struct {
	GroupID       string  `json:"group_id"`
	UserID        string  `json:"user_id"`
	AddedBy       *string `json:"added_by"`
	AddedAt       string  `json:"added_at"`
	UserName      string  `json:"user_name"`
	UserEmail     string  `json:"user_email"`
	UserAvatarURL *string `json:"user_avatar_url"`
}

func groupResponseFromModel(group cerebrodb.CerebroGroup) groupResponse {
	return groupResponse{
		ID:          util.UUIDToString(group.ID),
		WorkspaceID: util.UUIDToString(group.WorkspaceID),
		Name:        group.Name,
		Description: util.TextToPtr(group.Description),
		CreatedBy:   util.UUIDToPtr(group.CreatedBy),
		CreatedAt:   util.TimestampToString(group.CreatedAt),
		UpdatedAt:   util.TimestampToString(group.UpdatedAt),
	}
}

func groupMemberResponseFromAdd(row cerebrodb.AddCerebroGroupMemberRow) groupMemberResponse {
	return groupMemberResponse{
		GroupID:       util.UUIDToString(row.GroupID),
		UserID:        util.UUIDToString(row.UserID),
		AddedBy:       util.UUIDToPtr(row.AddedBy),
		AddedAt:       util.TimestampToString(row.AddedAt),
		UserName:      row.UserName,
		UserEmail:     row.UserEmail,
		UserAvatarURL: util.TextToPtr(row.UserAvatarUrl),
	}
}

func groupMemberResponseFromRemoved(row cerebrodb.RemoveCerebroGroupMemberRow) groupMemberResponse {
	return groupMemberResponse{
		GroupID:       util.UUIDToString(row.GroupID),
		UserID:        util.UUIDToString(row.UserID),
		AddedBy:       util.UUIDToPtr(row.AddedBy),
		AddedAt:       util.TimestampToString(row.AddedAt),
		UserName:      row.UserName,
		UserEmail:     row.UserEmail,
		UserAvatarURL: util.TextToPtr(row.UserAvatarUrl),
	}
}

func groupMemberResponseFromList(row cerebrodb.ListCerebroGroupMembersRow) groupMemberResponse {
	return groupMemberResponse{
		GroupID:       util.UUIDToString(row.GroupID),
		UserID:        util.UUIDToString(row.UserID),
		AddedBy:       util.UUIDToPtr(row.AddedBy),
		AddedAt:       util.TimestampToString(row.AddedAt),
		UserName:      row.UserName,
		UserEmail:     row.UserEmail,
		UserAvatarURL: util.TextToPtr(row.UserAvatarUrl),
	}
}
