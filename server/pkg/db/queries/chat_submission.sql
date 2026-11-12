-- name: GetChatSubmissionReceipt :one
SELECT request_fingerprint, response FROM chat_submission_receipt
WHERE chat_session_id = $1 AND workspace_id = $2 AND initiator_user_id = $3
  AND actor_type = $4 AND actor_id = $5 AND submission_id = $6;

-- name: CreateChatSubmissionReceipt :exec
INSERT INTO chat_submission_receipt (
    chat_session_id, workspace_id, initiator_user_id, actor_type, actor_id,
    submission_id, request_fingerprint, response
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: DeleteChatSubmissionReceiptsBySession :exec
DELETE FROM chat_submission_receipt WHERE chat_session_id = $1;

-- name: DeleteChatSubmissionReceiptsBySystemRuntimeAgents :exec
DELETE FROM chat_submission_receipt
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);
