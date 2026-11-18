package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func validateRemoteAgentConfig(provider string, raw []byte) error {
	if provider == "kagent" {
		if os.Getenv("MULTICA_KAGENT_ENABLED") != "1" {
			return fmt.Errorf("kagent: DEV provider disabled")
		}
		return agent.ValidateKagentRuntimeConfig(raw)
	}
	return validateQoderAgentConfig(raw)
}

func (o qoderOverrides) validateFor(provider string) error {
	err := o.validate()
	if err != nil && provider == "kagent" {
		return fmt.Errorf("kagent: native-text agents do not support skills, custom environment/arguments, MCP or model overrides")
	}
	return err
}

// Native text claims intentionally receive no task credential. The claim's
// transactional authorization/receipt still runs; its token is never delivered.
func claimExecutionToken(provider, token string) string {
	if provider == "kagent" {
		return ""
	}
	return token
}

// Enrollment remains authoritative even when runtime deletion unbinds the agent.
// Missing intent never establishes that remote resources have been removed.
func hasKagentLifecycle(ctx context.Context, executor dbExecutor, a db.Agent) (bool, error) {
	var enrolled bool
	err := executor.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM kagent_enrollment WHERE agent_id = $1)
		OR EXISTS (SELECT 1 FROM agent_runtime WHERE id = $2 AND workspace_id = $3 AND provider = 'kagent')`,
		a.ID, a.RuntimeID, a.WorkspaceID).Scan(&enrolled)
	return enrolled, err
}

func lockKagentEnrollment(ctx context.Context, tx pgx.Tx, queries *db.Queries, a db.Agent) (db.KagentEnrollment, error) {
	var id string
	if err := tx.QueryRow(ctx, `SELECT agent_id FROM kagent_enrollment
		WHERE agent_id=$1 AND workspace_id=$2 AND runtime_id=$3 FOR UPDATE`,
		a.ID, a.WorkspaceID, a.RuntimeID).Scan(&id); err != nil {
		return db.KagentEnrollment{}, err
	}
	return queries.GetKagentEnrollment(ctx, db.GetKagentEnrollmentParams{
		AgentID: a.ID, WorkspaceID: a.WorkspaceID, RuntimeID: a.RuntimeID,
	})
}

func (h *Handler) allowKagentArchiveOrRestore(w http.ResponseWriter, r *http.Request, a db.Agent) bool {
	blocked, err := hasKagentLifecycle(r.Context(), h.DB, a)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
		return false
	}
	if blocked {
		writeError(w, http.StatusConflict, "kagent archive and restore require verified remote drain and are not supported")
		return false
	}
	return true
}
