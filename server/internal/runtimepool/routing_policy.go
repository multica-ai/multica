package runtimepool

import (
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RuntimeMatchesTriggerPolicy is the Pool placement boundary. A Task without
// an explicit trigger user is cloud-only. A Task with one may use that user's
// own local Runtime, with cloud as fallback.
func RuntimeMatchesTriggerPolicy(runtime db.AgentRuntime, triggerUserID pgtype.UUID) bool {
	switch runtime.RuntimeMode {
	case "cloud":
		return true
	case "local":
		return triggerUserID.Valid && runtime.OwnerID == triggerUserID
	default:
		return false
	}
}
