package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	maxIdempotencyBodySize = 4 << 20
)

// IdempotencyResult describes the action the HTTP adapter should take after
// claiming a request receipt.
type IdempotencyResult struct {
	// State is one of "execute", "processing", or "replay".
	State        string
	ReceiptID    string
	Status       int
	ContentType  string
	ResponseBody []byte
}

type storedIdempotencyResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	BodyBase64  string `json:"body_base64"`
}

// BeginIdempotency atomically claims a workspace/actor/operation/key tuple.
// The receipt is committed before the business handler runs, so a retry that
// arrives while a request is still processing receives 202 instead of
// executing the mutation twice. Expired receipts can be safely reused.
func (s *Service) BeginIdempotency(ctx context.Context, workspaceID, actorKey, operation, key, requestHash string) (IdempotencyResult, error) {
	if err := s.checkEnabled(); err != nil {
		return IdempotencyResult{}, err
	}
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return IdempotencyResult{}, err
	}
	actorKey = strings.TrimSpace(actorKey)
	operation = strings.TrimSpace(operation)
	key = strings.TrimSpace(key)
	requestHash = strings.TrimSpace(requestHash)
	if actorKey == "" || operation == "" || key == "" || requestHash == "" {
		return IdempotencyResult{}, badRequest("invalid_idempotency_request", "idempotency request fields are required")
	}
	if len([]rune(key)) > 200 {
		return IdempotencyResult{}, badRequest("idempotency_key_too_long", "Idempotency-Key must be at most 200 characters")
	}

	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return IdempotencyResult{}, internal("failed to start idempotency transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, workspace); err != nil {
		return IdempotencyResult{}, err
	}

	var receiptID string
	err = tx.QueryRow(ctx, `
		INSERT INTO knowledge_request(workspace_id,actor_key,operation,idempotency_key,request_hash,status,expires_at)
		VALUES($1,$2,$3,$4,$5,'processing',now()+interval '24 hours')
		ON CONFLICT (workspace_id,actor_key,operation,idempotency_key) DO NOTHING
		RETURNING id::text`, workspace, actorKey, operation, key, requestHash).Scan(&receiptID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return IdempotencyResult{}, internal("failed to commit idempotency receipt", err)
		}
		return IdempotencyResult{State: "execute", ReceiptID: receiptID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IdempotencyResult{}, internal("failed to create idempotency receipt", err)
	}

	var currentHash, status string
	var resultRef []byte
	var expired bool
	if err := tx.QueryRow(ctx, `
		SELECT id::text,request_hash,status,result_ref,(expires_at<=now())
		FROM knowledge_request
		WHERE workspace_id=$1 AND actor_key=$2 AND operation=$3 AND idempotency_key=$4
		FOR UPDATE`, workspace, actorKey, operation, key).Scan(&receiptID, &currentHash, &status, &resultRef, &expired); err != nil {
		return IdempotencyResult{}, internal("failed to load idempotency receipt", err)
	}
	if expired {
		if _, err := tx.Exec(ctx, `
			UPDATE knowledge_request
			SET request_hash=$2,status='processing',dependency_refs='{}',result_ref='{}',expires_at=now()+interval '24 hours',updated_at=now()
			WHERE id=$1`, receiptID, requestHash); err != nil {
			return IdempotencyResult{}, internal("failed to recycle expired idempotency receipt", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return IdempotencyResult{}, internal("failed to commit recycled idempotency receipt", err)
		}
		return IdempotencyResult{State: "execute", ReceiptID: receiptID}, nil
	}
	if currentHash != requestHash {
		return IdempotencyResult{}, conflict("idempotency_conflict", "the same Idempotency-Key was used with a different request body")
	}
	if err := tx.Commit(ctx); err != nil {
		return IdempotencyResult{}, internal("failed to commit idempotency lookup", err)
	}
	if status == "processing" {
		return IdempotencyResult{State: "processing", ReceiptID: receiptID, Status: http.StatusAccepted}, nil
	}
	if status != "succeeded" && status != "failed" {
		return IdempotencyResult{}, internal("idempotency receipt has an invalid status", nil)
	}
	var stored storedIdempotencyResponse
	if err := json.Unmarshal(resultRef, &stored); err != nil || stored.Status < 100 || stored.Status > 599 {
		return IdempotencyResult{}, internal("idempotency receipt has an invalid response", err)
	}
	body, err := base64.StdEncoding.DecodeString(stored.BodyBase64)
	if err != nil || len(body) > maxIdempotencyBodySize {
		return IdempotencyResult{}, internal("idempotency receipt body is invalid", err)
	}
	return IdempotencyResult{State: "replay", ReceiptID: receiptID, Status: stored.Status, ContentType: stored.ContentType, ResponseBody: body}, nil
}

// FinishIdempotency stores the exact HTTP response for later retries. Knowledge
// responses are intentionally bounded; all current idempotent mutations return
// small JSON envelopes, while the uploaded source itself stays in private
// object storage and is never copied into this receipt.
func (s *Service) FinishIdempotency(ctx context.Context, receiptID string, status int, contentType string, responseBody []byte, success bool) error {
	if strings.TrimSpace(receiptID) == "" {
		return nil
	}
	if len(responseBody) > maxIdempotencyBodySize {
		return internal("idempotency response exceeds storage limit", nil)
	}
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}
	stored, err := json.Marshal(storedIdempotencyResponse{
		Status:      status,
		ContentType: strings.TrimSpace(contentType),
		BodyBase64:  base64.StdEncoding.EncodeToString(responseBody),
	})
	if err != nil {
		return internal("failed to encode idempotency response", err)
	}
	state := "failed"
	if success {
		state = "succeeded"
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE knowledge_request
		SET status=$2,result_ref=$3,updated_at=now()
		WHERE id=$1 AND status='processing'`, receiptID, state, stored); err != nil {
		return internal("failed to finalize idempotency receipt", err)
	}
	return nil
}
