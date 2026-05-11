package account

import (
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type accountResponse struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspace_id"`
	Provider      string `json:"provider"`
	LoginIdentity string `json:"login_identity"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func accountResponseFromModel(a cerebrodb.CerebroAccount) accountResponse {
	return accountResponse{
		ID:            util.UUIDToString(a.ID),
		WorkspaceID:   util.UUIDToString(a.WorkspaceID),
		Provider:      a.Provider,
		LoginIdentity: a.LoginIdentity,
		CreatedAt:     util.TimestampToString(a.CreatedAt),
		UpdatedAt:     util.TimestampToString(a.UpdatedAt),
	}
}
