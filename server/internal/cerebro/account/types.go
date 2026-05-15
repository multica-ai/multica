package account

import (
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Account is the package-internal representation of a workspace account.
// sqlc emits a distinct row type per query (CreateCerebroAccountRow,
// GetCerebroAccountRow, ListCerebroAccountsRow, ...). They all carry the
// same column set, so we normalize to a single type at the service
// boundary instead of plumbing five row types through the handler.
type Account struct {
	ID             pgtype.UUID
	WorkspaceID    pgtype.UUID
	Provider       string
	LoginIdentity  string
	UsageWindowPct pgtype.Float4
	ThrottledUntil pgtype.Timestamptz
	Tokens5h       int64
	Tokens7d       int64
	ExtraSpendOn   bool
	PausedManual   bool
	CreatedAt      pgtype.Timestamptz
	UpdatedAt      pgtype.Timestamptz
}

func accountFromList(r cerebrodb.ListCerebroAccountsRow) Account {
	return Account{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, LoginIdentity: r.LoginIdentity,
		UsageWindowPct: r.UsageWindowPct, ThrottledUntil: r.ThrottledUntil,
		Tokens5h: r.Tokens5h, Tokens7d: r.Tokens7d,
		ExtraSpendOn: r.ExtraSpendOn, PausedManual: r.PausedManual,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func accountFromGet(r cerebrodb.GetCerebroAccountRow) Account {
	return Account{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, LoginIdentity: r.LoginIdentity,
		UsageWindowPct: r.UsageWindowPct, ThrottledUntil: r.ThrottledUntil,
		Tokens5h: r.Tokens5h, Tokens7d: r.Tokens7d,
		ExtraSpendOn: r.ExtraSpendOn, PausedManual: r.PausedManual,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func accountFromCreate(r cerebrodb.CreateCerebroAccountRow) Account {
	return Account{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, LoginIdentity: r.LoginIdentity,
		UsageWindowPct: r.UsageWindowPct, ThrottledUntil: r.ThrottledUntil,
		Tokens5h: r.Tokens5h, Tokens7d: r.Tokens7d,
		ExtraSpendOn: r.ExtraSpendOn, PausedManual: r.PausedManual,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func accountFromUpdateUsage(r cerebrodb.UpdateCerebroAccountUsageRow) Account {
	return Account{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, LoginIdentity: r.LoginIdentity,
		UsageWindowPct: r.UsageWindowPct, ThrottledUntil: r.ThrottledUntil,
		Tokens5h: r.Tokens5h, Tokens7d: r.Tokens7d,
		ExtraSpendOn: r.ExtraSpendOn, PausedManual: r.PausedManual,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func accountFromUpdateControls(r cerebrodb.UpdateCerebroAccountControlsRow) Account {
	return Account{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, LoginIdentity: r.LoginIdentity,
		UsageWindowPct: r.UsageWindowPct, ThrottledUntil: r.ThrottledUntil,
		Tokens5h: r.Tokens5h, Tokens7d: r.Tokens7d,
		ExtraSpendOn: r.ExtraSpendOn, PausedManual: r.PausedManual,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

type accountResponse struct {
	ID                    string   `json:"id"`
	WorkspaceID           string   `json:"workspace_id"`
	Provider              string   `json:"provider"`
	LoginIdentity         string   `json:"login_identity"`
	UsageWindowPct        *float32 `json:"usage_window_pct"`
	ThrottledUntil        *string  `json:"throttled_until"`
	Tokens5h              int64    `json:"tokens_5h"`
	Tokens7d              int64    `json:"tokens_7d"`
	ExtraSpendOn          bool     `json:"extra_spend_on"`
	PausedManual          bool     `json:"paused_manual"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
	RuntimeCount          int32    `json:"runtime_count"`
	AvailableRuntimeCount int32    `json:"available_runtime_count"`
	NearestUnpauseAt      *string  `json:"nearest_unpause_at"`
	Status                string   `json:"status"`
}

func accountResponseFromModel(a Account) accountResponse {
	resp := accountResponse{
		ID:            util.UUIDToString(a.ID),
		WorkspaceID:   util.UUIDToString(a.WorkspaceID),
		Provider:      a.Provider,
		LoginIdentity: a.LoginIdentity,
		Tokens5h:      a.Tokens5h,
		Tokens7d:      a.Tokens7d,
		ExtraSpendOn:  a.ExtraSpendOn,
		PausedManual:  a.PausedManual,
		CreatedAt:     util.TimestampToString(a.CreatedAt),
		UpdatedAt:     util.TimestampToString(a.UpdatedAt),
		Status:        "no_runtime",
	}
	if a.UsageWindowPct.Valid {
		v := a.UsageWindowPct.Float32
		resp.UsageWindowPct = &v
	}
	resp.ThrottledUntil = util.TimestampToPtr(a.ThrottledUntil)
	return resp
}

func accountResponseFromAvailabilityRow(r cerebrodb.ListCerebroAccountsWithAvailabilityRow) accountResponse {
	var nearestUnpauseAt *string
	if r.NearestUnpauseAt.Valid {
		s := util.TimestampToString(r.NearestUnpauseAt)
		nearestUnpauseAt = &s
	}
	status := deriveAccountStatus(r.RuntimeCount, r.AvailableRuntimeCount, nearestUnpauseAt)
	resp := accountResponse{
		ID:                    util.UUIDToString(r.ID),
		WorkspaceID:           util.UUIDToString(r.WorkspaceID),
		Provider:              r.Provider,
		LoginIdentity:         r.LoginIdentity,
		Tokens5h:              r.Tokens5h,
		Tokens7d:              r.Tokens7d,
		ExtraSpendOn:          r.ExtraSpendOn,
		PausedManual:          r.PausedManual,
		CreatedAt:             util.TimestampToString(r.CreatedAt),
		UpdatedAt:             util.TimestampToString(r.UpdatedAt),
		RuntimeCount:          r.RuntimeCount,
		AvailableRuntimeCount: r.AvailableRuntimeCount,
		NearestUnpauseAt:      nearestUnpauseAt,
		Status:                status,
	}
	if r.UsageWindowPct.Valid {
		v := r.UsageWindowPct.Float32
		resp.UsageWindowPct = &v
	}
	resp.ThrottledUntil = util.TimestampToPtr(r.ThrottledUntil)
	return resp
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
