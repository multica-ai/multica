-- name: ListKnowledgeEvalCases :many
SELECT * FROM knowledge_eval_case
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC
LIMIT 200;

-- name: GetKnowledgeEvalCase :one
SELECT * FROM knowledge_eval_case WHERE workspace_id = $1 AND id = $2;

-- name: CreateKnowledgeEvalCase :one
INSERT INTO knowledge_eval_case (
    workspace_id, question, expected_kind, expected_document_id, owner_squad_id, created_by_type, created_by_id
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: DeleteKnowledgeEvalCase :execrows
DELETE FROM knowledge_eval_case WHERE workspace_id = $1 AND id = $2;

-- name: CreateKnowledgeEvalRun :one
INSERT INTO knowledge_eval_run (workspace_id, total, hits, hit_rate, results)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListKnowledgeEvalRuns :many
-- Newest first. The digest reads the first two: the latest run and the one
-- before it.
SELECT * FROM knowledge_eval_run
WHERE workspace_id = $1
ORDER BY ran_at DESC, id DESC
LIMIT $2;

-- name: CreateKnowledgeHealthRun :one
INSERT INTO knowledge_health_run (workspace_id, job, summary)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListLatestKnowledgeHealthRuns :many
-- The newest run of each job.
SELECT DISTINCT ON (job) * FROM knowledge_health_run
WHERE workspace_id = $1
ORDER BY job ASC, ran_at DESC, id DESC;

-- name: GetAgentSquadID :one
-- The squad an agent belongs to. An agent in several squads gets the first one
-- it joined.
SELECT squad_id FROM squad_member
WHERE member_type = 'agent' AND member_id = $1
ORDER BY created_at ASC
LIMIT 1;
