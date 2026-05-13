package account

import (
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type accountResponse struct {
	ID                    string  `json:"id"`
	WorkspaceID           string  `json:"workspace_id"`
	Provider              string  `json:"provider"`
	LoginIdentity         string  `json:"login_identity"`
	CreatedAt             string  `json:"created_at"`
	UpdatedAt             string  `json:"updated_at"`
	RuntimeCount          int32   `json:"runtime_count"`
	AvailableRuntimeCount int32   `json:"available_runtime_count"`
	NearestUnpauseAt      *string `json:"nearest_unpause_at"`
	Status                string  `json:"status"`
}

func accountResponseFromModel(a cerebrodb.CerebroAccount) accountResponse {
	return accountResponse{
		ID:            util.UUIDToString(a.ID),
		WorkspaceID:   util.UUIDToString(a.WorkspaceID),
		Provider:      a.Provider,
		LoginIdentity: a.LoginIdentity,
		CreatedAt:     util.TimestampToString(a.CreatedAt),
		UpdatedAt:     util.TimestampToString(a.UpdatedAt),
		Status:        "no_runtime",
	}
}

func accountResponseFromAvailabilityRow(r cerebrodb.ListCerebroAccountsWithAvailabilityRow) accountResponse {
	var nearestUnpauseAt *string
	if r.NearestUnpauseAt.Valid {
		s := util.TimestampToString(r.NearestUnpauseAt)
		nearestUnpauseAt = &s
	}
	status := deriveAccountStatus(r.RuntimeCount, r.AvailableRuntimeCount, nearestUnpauseAt)
	return accountResponse{
		ID:                    util.UUIDToString(r.ID),
		WorkspaceID:           util.UUIDToString(r.WorkspaceID),
		Provider:              r.Provider,
		LoginIdentity:         r.LoginIdentity,
		CreatedAt:             util.TimestampToString(r.CreatedAt),
		UpdatedAt:             util.TimestampToString(r.UpdatedAt),
		RuntimeCount:          r.RuntimeCount,
		AvailableRuntimeCount: r.AvailableRuntimeCount,
		NearestUnpauseAt:      nearestUnpauseAt,
		Status:                status,
	}
}

// deriveAccountStatus maps runtime availability counters to a human-readable
// status slug consumed by the frontend for badge rendering.
//   - available:   at least one runtime is unpaused
//   - throttled:   all runtimes are paused but will auto-resume (unpause_at set)
//   - paused:      all runtimes are manually paused (no auto-resume scheduled)
//   - no_runtime:  no runtimes linked to this account yet
func deriveAccountStatus(runtimeCount, availableCount int32, nearestUnpauseAt *string) string {
	switch {
	case availableCount > 0:
		return "available"
	case runtimeCount > 0 && nearestUnpauseAt != nil:
		return "throttled"
	case runtimeCount > 0:
		return "paused"
	default:
		return "no_runtime"
	}
}
