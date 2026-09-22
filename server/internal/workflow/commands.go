package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
)

// workflowCommandHash deliberately excludes the idempotency key. A key can be
// replayed only when the business request, target resource, and expected
// version are the same; the key itself is the lookup identity, not request
// content.
func workflowCommandHash(operation, resourceID string, payload any) string {
	request, _ := json.Marshal(struct {
		Operation  string `json:"operation"`
		ResourceID string `json:"resource_id"`
		Payload    any    `json:"payload"`
	}{Operation: operation, ResourceID: resourceID, Payload: payload})
	digest := sha256.Sum256(request)
	return hex.EncodeToString(digest[:])
}

// beginWorkflowRunCommand inserts the durable command boundary while the
// caller still holds the run row lock. A replay is returned only after the
// same actor has passed the caller's resource/manager authorization checks.
// Failed transactions roll back the placeholder, so a later request cannot
// replay a response for a command that never committed.
func beginWorkflowRunCommand(ctx context.Context, tx pgx.Tx, workspaceID, actorID, operation, runID, key, requestHash string) (*Run, error) {
	responseBody, err := beginWorkflowCommand(ctx, tx, workspaceID, actorID, operation, runID, key, requestHash)
	if err != nil || len(responseBody) == 0 {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(responseBody, &run); err != nil {
		return nil, fmt.Errorf("decode stored workflow command response: %w", err)
	}
	return &run, nil
}

func beginWorkflowCommand(ctx context.Context, tx pgx.Tx, workspaceID, actorID, operation, resourceID, key, requestHash string) ([]byte, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	if len(key) > 200 {
		return nil, Bad("idempotency_key must be at most 200 bytes")
	}
	if _, err := util.ParseUUID(actorID); err != nil {
		return nil, Bad("invalid workflow command actor")
	}
	if _, err := util.ParseUUID(resourceID); err != nil {
		return nil, Bad("invalid workflow command resource")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workflow_command(
  id,workspace_id,actor_id,operation,resource_id,idempotency_key,request_hash
)
VALUES($1,$2,$3,$4,$5,$6,$7)

ON CONFLICT DO NOTHING`, id(), workspaceID, uuid(actorID), operation, uuid(resourceID), key, requestHash); err != nil {
		return nil, err
	}
	var storedHash string
	var statusCode int
	var responseBody []byte
	err := tx.QueryRow(ctx, `
SELECT request_hash,status_code,response_body
FROM workflow_command
WHERE workspace_id=$1 AND actor_id=$2 AND operation=$3 AND resource_id=$4 AND idempotency_key=$5
FOR UPDATE`, workspaceID, uuid(actorID), operation, uuid(resourceID), key).Scan(&storedHash, &statusCode, &responseBody)
	if err != nil {
		return nil, err
	}
	if storedHash != requestHash {
		return nil, Conflict("idempotency key was already used for a different workflow action")
	}
	if statusCode == 0 || len(responseBody) == 0 {
		return nil, nil
	}
	return responseBody, nil
}

func finishWorkflowRunCommand(ctx context.Context, tx pgx.Tx, workspaceID, actorID, operation, runID, key, requestHash string, run Run) error {
	return finishWorkflowCommand(ctx, tx, workspaceID, actorID, operation, runID, key, requestHash, run)
}

func finishWorkflowCommand(ctx context.Context, tx pgx.Tx, workspaceID, actorID, operation, resourceID, key, requestHash string, response any) error {
	return finishWorkflowCommandStatus(ctx, tx, workspaceID, actorID, operation, resourceID, key, requestHash, response, 200)
}

func finishWorkflowCommandStatus(ctx context.Context, tx pgx.Tx, workspaceID, actorID, operation, resourceID, key, requestHash string, response any, statusCode int) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	result, err := tx.Exec(ctx, `
UPDATE workflow_command
SET status_code=$7,response_body=$8
WHERE workspace_id=$1 AND actor_id=$2 AND operation=$3 AND resource_id=$4
  AND idempotency_key=$5 AND request_hash=$6`, workspaceID, uuid(actorID), operation, uuid(resourceID), key, requestHash, statusCode, body(response))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("workflow command response row was not found")
	}
	return nil
}
