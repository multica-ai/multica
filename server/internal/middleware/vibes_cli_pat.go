package middleware

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type vibesCLIPATResult struct {
	Found              bool
	Allowed            bool
	Unavailable        bool
	MulticaUserID      string
	MulticaWorkspaceID string
	Role               tagaccess.Role
}

func authorizeVIBESCLIPAT(ctx context.Context, queries *db.Queries, gate *tagaccess.Gate, tokenHash string) vibesCLIPATResult {
	if queries == nil {
		return vibesCLIPATResult{}
	}
	binding, err := queries.GetVIBESCLIPATBindingByTokenHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return vibesCLIPATResult{}
	}
	if err != nil {
		return vibesCLIPATResult{Unavailable: true}
	}
	result := vibesCLIPATResult{
		Found: true, MulticaUserID: uuidToString(binding.MulticaUserID),
		MulticaWorkspaceID: uuidToString(binding.MulticaWorkspaceID),
	}
	if gate == nil || binding.AccountEpoch < 1 || binding.SessionWorkspaceGeneration < 1 || binding.AuthorityVersion < 1 || binding.MembershipGeneration < 1 {
		result.Unavailable = true
		return result
	}
	decision := gate.Authorize(ctx, tagaccess.AccessRequest{
		TagSessionID: binding.TagSessionID, VIBESSessionID: binding.VibesSessionID,
		VIBESUserID: binding.VibesUserID, WorkspaceID: binding.VibesWorkspaceID,
		AccountEpoch: uint64(binding.AccountEpoch), SessionWorkspaceGeneration: uint64(binding.SessionWorkspaceGeneration),
		AuthorityVersion: uint64(binding.AuthorityVersion), MembershipGeneration: uint64(binding.MembershipGeneration),
	})
	result.Allowed = decision.Allowed
	result.Role = decision.Role
	result.Unavailable = decision.Reason == tagaccess.DenyStoreUnavailable
	return result
}
