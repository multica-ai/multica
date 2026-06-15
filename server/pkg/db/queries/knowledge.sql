-- name: ListKnowledgeItems :many
SELECT *
FROM knowledge_item
WHERE workspace_id = sqlc.arg('workspace_id')
  AND (
      sqlc.arg('include_inactive')::boolean
      OR lifecycle_status NOT IN ('archived', 'deprecated')
  )
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type'))
  AND (sqlc.narg('status')::text IS NULL OR lifecycle_status = sqlc.narg('status'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR domain_labels && sqlc.narg('labels')::text[]
  )
  AND (
      sqlc.narg('query')::text IS NULL
      OR LOWER(title) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(problem_pattern) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(trigger_conditions) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(diagnostic_steps) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(recommended_practice) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(anti_patterns) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(applicability) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
  )
ORDER BY updated_at DESC, created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: GetKnowledgeItem :one
SELECT *
FROM knowledge_item
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');

-- name: CreateKnowledgeItem :one
INSERT INTO knowledge_item (
    workspace_id, project_id, agent_id, title, type, domain_labels,
    problem_pattern, trigger_conditions, diagnostic_steps,
    recommended_practice, anti_patterns, applicability,
    confidence_status, lifecycle_status, created_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.narg('project_id'), sqlc.narg('agent_id'),
    sqlc.arg('title'), sqlc.arg('type'), COALESCE(sqlc.narg('domain_labels')::text[], '{}'),
    COALESCE(sqlc.narg('problem_pattern'), ''), COALESCE(sqlc.narg('trigger_conditions'), ''),
    COALESCE(sqlc.narg('diagnostic_steps'), ''), COALESCE(sqlc.narg('recommended_practice'), ''),
    COALESCE(sqlc.narg('anti_patterns'), ''), COALESCE(sqlc.narg('applicability'), ''),
    sqlc.arg('confidence_status'), sqlc.arg('lifecycle_status'), sqlc.arg('created_by')
)
RETURNING *;

-- name: UpdateKnowledgeItem :one
UPDATE knowledge_item SET
    project_id = COALESCE(sqlc.narg('project_id'), project_id),
    agent_id = COALESCE(sqlc.narg('agent_id'), agent_id),
    title = COALESCE(sqlc.narg('title'), title),
    type = COALESCE(sqlc.narg('type'), type),
    domain_labels = COALESCE(sqlc.narg('domain_labels')::text[], domain_labels),
    problem_pattern = COALESCE(sqlc.narg('problem_pattern'), problem_pattern),
    trigger_conditions = COALESCE(sqlc.narg('trigger_conditions'), trigger_conditions),
    diagnostic_steps = COALESCE(sqlc.narg('diagnostic_steps'), diagnostic_steps),
    recommended_practice = COALESCE(sqlc.narg('recommended_practice'), recommended_practice),
    anti_patterns = COALESCE(sqlc.narg('anti_patterns'), anti_patterns),
    applicability = COALESCE(sqlc.narg('applicability'), applicability),
    confidence_status = COALESCE(sqlc.narg('confidence_status'), confidence_status),
    lifecycle_status = COALESCE(sqlc.narg('lifecycle_status'), lifecycle_status),
    reviewed_by = COALESCE(sqlc.narg('reviewed_by'), reviewed_by),
    reviewed_at = CASE
        WHEN sqlc.narg('reviewed_by')::uuid IS NOT NULL THEN now()
        ELSE reviewed_at
    END,
    published_at = CASE
        WHEN sqlc.narg('lifecycle_status')::text = 'published' AND published_at IS NULL THEN now()
        ELSE published_at
    END,
    archived_at = CASE
        WHEN sqlc.narg('lifecycle_status')::text = 'archived' AND archived_at IS NULL THEN now()
        ELSE archived_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ArchiveKnowledgeItem :one
UPDATE knowledge_item SET
    lifecycle_status = 'archived',
    archived_at = COALESCE(archived_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: CountKnowledgeSources :one
SELECT count(*)
FROM knowledge_source
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: ListKnowledgeSources :many
SELECT *
FROM knowledge_source
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id')
ORDER BY created_at ASC;

-- name: CreateKnowledgeSource :one
INSERT INTO knowledge_source (
    knowledge_item_id, workspace_id, source_type, source_id,
    source_url, source_title, source_excerpt
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('source_type'),
    sqlc.narg('source_id'), sqlc.narg('source_url'), sqlc.narg('source_title'), sqlc.narg('source_excerpt')
)
RETURNING *;

-- name: ListKnowledgeEmbeddingMetadata :many
SELECT id, knowledge_item_id, workspace_id, provider, model, content_hash, embedded_at, created_at
FROM knowledge_embedding
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id')
ORDER BY embedded_at DESC;

-- name: UpsertKnowledgeEmbedding :one
INSERT INTO knowledge_embedding (
    knowledge_item_id, workspace_id, provider, model, content_hash, embedding, embedded_at
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('provider'),
    sqlc.arg('model'), sqlc.arg('content_hash'), sqlc.arg('embedding')::vector, now()
)
ON CONFLICT (knowledge_item_id, provider, model, content_hash)
DO UPDATE SET embedding = EXCLUDED.embedding, embedded_at = now()
RETURNING id, knowledge_item_id, workspace_id, provider, model, content_hash, embedded_at, created_at;

-- name: SearchKnowledgeText :many
WITH candidates AS (
    SELECT *,
        (
            CASE WHEN LOWER(title) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 4 ELSE 0 END +
            CASE WHEN LOWER(problem_pattern) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 3 ELSE 0 END +
            CASE WHEN LOWER(recommended_practice) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 3 ELSE 0 END +
            CASE WHEN LOWER(anti_patterns) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 2 ELSE 0 END +
            CASE WHEN LOWER(trigger_conditions) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 1 ELSE 0 END +
            CASE WHEN LOWER(diagnostic_steps) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 1 ELSE 0 END +
            CASE WHEN LOWER(applicability) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 1 ELSE 0 END
        )::float8 AS text_score
    FROM knowledge_item
    WHERE workspace_id = sqlc.arg('workspace_id')
      AND lifecycle_status NOT IN ('archived', 'deprecated')
      AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR type = ANY(sqlc.narg('types')::text[]))
      AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
      AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id'))
      AND (
          COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
          OR domain_labels && sqlc.narg('labels')::text[]
      )
)
SELECT *
FROM candidates
WHERE text_score > 0
ORDER BY text_score DESC, updated_at DESC
LIMIT sqlc.arg('limit');

-- name: SearchKnowledgeVector :many
SELECT
    ki.*,
    (1 - MIN(ke.embedding <=> sqlc.arg('embedding')::vector))::float8 AS vector_score
FROM knowledge_item ki
JOIN knowledge_embedding ke ON ke.knowledge_item_id = ki.id AND ke.workspace_id = ki.workspace_id
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
  AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR ki.type = ANY(sqlc.narg('types')::text[]))
  AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR ki.lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR ki.domain_labels && sqlc.narg('labels')::text[]
  )
GROUP BY ki.id
ORDER BY vector_score DESC, ki.updated_at DESC
LIMIT sqlc.arg('limit');

-- name: CreateKnowledgeFeedback :one
INSERT INTO knowledge_feedback (knowledge_item_id, workspace_id, member_id, value, note)
VALUES (sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('member_id'), sqlc.arg('value'), sqlc.narg('note'))
RETURNING *;

-- name: GetKnowledgeFeedbackSummary :many
SELECT value, count(*)::bigint AS count
FROM knowledge_feedback
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id')
GROUP BY value
ORDER BY value ASC;

-- name: CreateKnowledgeRetrievalEvent :one
INSERT INTO knowledge_retrieval_event (
    workspace_id, member_id, query, retrieval_mode, filters, result_count, top_knowledge_item_ids
) VALUES (
    sqlc.arg('workspace_id'), sqlc.narg('member_id'), sqlc.narg('query'), sqlc.arg('retrieval_mode'),
    COALESCE(sqlc.narg('filters'), '{}'::jsonb), sqlc.arg('result_count'), COALESCE(sqlc.narg('top_knowledge_item_ids')::uuid[], '{}')
)
RETURNING *;

-- name: UpsertKnowledgeCandidate :one
INSERT INTO knowledge_candidate (
    workspace_id, issue_id, comment_id, agent_task_id, source_type, source_id,
    trigger_reason, signal_strength, signals, score, status, dedupe_key,
    created_by, metadata, evaluated_at
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('issue_id'), sqlc.narg('comment_id'),
    sqlc.narg('agent_task_id'), sqlc.arg('source_type'), sqlc.arg('source_id'),
    sqlc.arg('trigger_reason'), sqlc.arg('signal_strength'),
    COALESCE(sqlc.narg('signals')::text[], '{}'), sqlc.arg('score'), sqlc.arg('status'),
    sqlc.arg('dedupe_key'), sqlc.narg('created_by'),
    COALESCE(sqlc.narg('metadata'), '{}'::jsonb), now()
)
ON CONFLICT (workspace_id, dedupe_key)
DO UPDATE SET
    issue_id = EXCLUDED.issue_id,
    comment_id = EXCLUDED.comment_id,
    agent_task_id = EXCLUDED.agent_task_id,
    source_type = EXCLUDED.source_type,
    source_id = EXCLUDED.source_id,
    trigger_reason = EXCLUDED.trigger_reason,
    signal_strength = EXCLUDED.signal_strength,
    signals = EXCLUDED.signals,
    score = EXCLUDED.score,
    status = CASE
        WHEN knowledge_candidate.status = 'drafted' THEN knowledge_candidate.status
        ELSE EXCLUDED.status
    END,
    created_by = COALESCE(EXCLUDED.created_by, knowledge_candidate.created_by),
    metadata = EXCLUDED.metadata,
    evaluated_at = now(),
    updated_at = now()
RETURNING *;

-- name: ListKnowledgeCandidates :many
SELECT *
FROM knowledge_candidate
WHERE workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('source_type')::text IS NULL OR source_type = sqlc.narg('source_type'))
  AND (sqlc.narg('issue_id')::uuid IS NULL OR issue_id = sqlc.narg('issue_id'))
ORDER BY score DESC, updated_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: GetKnowledgeCandidate :one
SELECT *
FROM knowledge_candidate
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');

-- name: UpdateKnowledgeCandidateDraftState :one
UPDATE knowledge_candidate SET
    status = COALESCE(sqlc.narg('status'), status),
    metadata = COALESCE(sqlc.narg('metadata'), metadata),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: CountIssueTaskOutcomesForKnowledgeCandidate :one
SELECT
    COUNT(*)::bigint AS task_count,
    COUNT(*) FILTER (WHERE status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE status = 'failed')::bigint AS failed_count,
    COUNT(*) FILTER (WHERE trigger_comment_id IS NOT NULL)::bigint AS comment_triggered_count,
    COALESCE(MAX(attempt), 0)::int AS max_attempt
FROM agent_task_queue
WHERE issue_id = sqlc.arg('issue_id');

-- name: ListIssueCommentsForKnowledgeCandidate :many
SELECT id, author_type, content
FROM comment
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND deleted_at IS NULL
ORDER BY created_at ASC
LIMIT sqlc.arg('limit');

-- name: ListIssueCommentsForKnowledgeDraft :many
SELECT *
FROM comment
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND deleted_at IS NULL
ORDER BY created_at ASC
LIMIT sqlc.arg('limit');

-- name: ListIssueAgentTasksForKnowledgeDraft :many
SELECT atq.*
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.issue_id = sqlc.arg('issue_id')
ORDER BY atq.created_at ASC
LIMIT sqlc.arg('limit');
