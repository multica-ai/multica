package lark

import (
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// uuidString renders a pgtype.UUID as its canonical string form. Shared by
// the package's logging and map-keying sites. (Formerly defined on hub.go,
// which the channel-engine cutover removed.)
func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }
