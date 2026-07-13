package middleware

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ownerLookupFor returns an auth.OwnerLookupFunc that asks the
// generated GetUser query whether `ownerID` is a real row in our
// `user` table. It is used by the mcn_ branches of Auth and
// DaemonAuth to confirm that Cloud's owner_id maps to a known local
// user before the verifier returns success / caches the result.
//
// Behaviour:
//   - queries==nil    → returns nil (no lookup; verifier skips step 3).
//     This path only kicks in when a middleware is constructed
//     without a DB handle, which only happens in tests that exercise
//     the verifier wiring without a real database.
//   - GetUser hits   → (true, nil): owner_id is a known user.
//   - pgx.ErrNoRows  → (false, nil): owner_id is unknown.
//     The verifier maps this to ErrCloudPATInvalid (reason
//     "owner_unknown") without caching.
//   - any other error → (_, err): treated as infrastructure failure;
//     the verifier maps this to ErrCloudPATUnavailable so the
//     middleware returns 503 (transient).
//
// Parsing the UUID is done via util.ParseUUID, which returns a zero
// UUID on a malformed input. A zero UUID will not match any real row,
// so the eventual GetUser call cleanly resolves to (false, nil) and
// the request is rejected — there is no need for a separate "looks
// like a UUID" precheck here. Cloud has already vetted the format
// before signing the verify response.
func ownerLookupFor(queries *db.Queries) auth.OwnerLookupFunc {
	if queries == nil {
		return nil
	}
	return func(ctx context.Context, ownerID string) (bool, error) {
		uuid, err := util.ParseUUID(ownerID)
		if err != nil {
			// Cloud returned something that doesn't parse as a UUID.
			// That's a contract violation, not a transient failure —
			// reject the token like any owner_unknown.
			return false, nil
		}
		_, err = queries.GetUser(ctx, uuid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
}
