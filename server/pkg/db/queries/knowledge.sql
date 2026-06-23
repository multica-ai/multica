-- name: ListKnowledgeItems :many
SELECT ki.*
FROM knowledge_item ki
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND (
      sqlc.arg('include_inactive')::boolean
      OR ki.lifecycle_status NOT IN ('archived', 'deprecated')
  )
  AND (sqlc.narg('type')::text IS NULL OR ki.type = sqlc.narg('type'))
  AND (sqlc.narg('status')::text IS NULL OR ki.lifecycle_status = sqlc.narg('status'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR ki.domain_labels && sqlc.narg('labels')::text[]
  )
  AND (
      sqlc.narg('query')::text IS NULL
      OR LOWER(ki.title) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(ki.problem_pattern) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(ki.trigger_conditions) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(ki.diagnostic_steps) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(ki.recommended_practice) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(ki.anti_patterns) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
      OR LOWER(ki.applicability) LIKE '%' || LOWER(sqlc.narg('query')::text) || '%'
  )
  AND (
      (
          sqlc.narg('source_type')::text IS NULL
          AND sqlc.narg('source_id')::uuid IS NULL
      )
      OR EXISTS (
          SELECT 1
          FROM knowledge_source ks
          WHERE ks.knowledge_item_id = ki.id
            AND ks.workspace_id = ki.workspace_id
            AND (sqlc.narg('source_type')::text IS NULL OR ks.source_type = sqlc.narg('source_type'))
            AND (sqlc.narg('source_id')::uuid IS NULL OR ks.source_id = sqlc.narg('source_id'))
      )
  )
ORDER BY
    CASE WHEN ki.review_needed_at IS NULL THEN 0 ELSE 1 END ASC,
    ki.updated_at DESC,
    ki.created_at DESC
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
    updated_by = COALESCE(sqlc.narg('updated_by'), updated_by),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: SetKnowledgeLifecycleStatus :one
UPDATE knowledge_item SET
    lifecycle_status = sqlc.arg('lifecycle_status'),
    reviewed_by = CASE
        WHEN sqlc.narg('reviewed_by')::uuid IS NOT NULL THEN sqlc.narg('reviewed_by')::uuid
        ELSE reviewed_by
    END,
    reviewed_at = CASE
        WHEN sqlc.narg('reviewed_by')::uuid IS NOT NULL THEN now()
        ELSE reviewed_at
    END,
    published_at = CASE
        WHEN sqlc.arg('lifecycle_status')::text = 'published' AND published_at IS NULL THEN now()
        ELSE published_at
    END,
    archived_at = CASE
        WHEN sqlc.arg('lifecycle_status')::text = 'archived' AND archived_at IS NULL THEN now()
        WHEN sqlc.arg('lifecycle_status')::text NOT IN ('archived', 'deprecated') THEN NULL
        ELSE archived_at
    END,
    deprecated_at = CASE
        WHEN sqlc.arg('lifecycle_status')::text = 'deprecated' AND deprecated_at IS NULL THEN now()
        WHEN sqlc.arg('lifecycle_status')::text NOT IN ('archived', 'deprecated') THEN NULL
        ELSE deprecated_at
    END,
    updated_by = sqlc.arg('updated_by'),
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

-- name: ListKnowledgePublishTargets :many
SELECT *
FROM knowledge_publish_target
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id')
ORDER BY created_at ASC;

-- name: GetKnowledgePublishTargetByType :one
SELECT *
FROM knowledge_publish_target
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND target_type = sqlc.arg('target_type');

-- name: UpsertKnowledgePublishTarget :one
INSERT INTO knowledge_publish_target (
    knowledge_item_id, workspace_id, target_type, target_id,
    target_url, target_title, metadata, created_by
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('target_type'),
    sqlc.narg('target_id'), sqlc.narg('target_url'), sqlc.narg('target_title'),
    COALESCE(sqlc.narg('metadata'), '{}'::jsonb), sqlc.narg('created_by')
)
ON CONFLICT (knowledge_item_id, target_type)
DO UPDATE SET
    target_id = EXCLUDED.target_id,
    target_url = EXCLUDED.target_url,
    target_title = EXCLUDED.target_title,
    metadata = EXCLUDED.metadata,
    created_by = COALESCE(EXCLUDED.created_by, knowledge_publish_target.created_by),
    updated_at = now()
RETURNING *;

-- name: ListKnowledgeEmbeddingMetadata :many
SELECT id, knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedded_at, created_at
FROM knowledge_embedding
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id')
ORDER BY embedded_at DESC;

-- name: GetKnowledgeEmbeddingAttempt :one
SELECT *
FROM knowledge_embedding_attempt
WHERE knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: UpsertKnowledgeEmbeddingAttempt :one
INSERT INTO knowledge_embedding_attempt (
    knowledge_item_id, workspace_id, status, provider, model, dimension,
    content_hash, error_message, attempted_at, embedded_at
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('status'),
    NULLIF(btrim(sqlc.arg('provider')::text), ''),
    NULLIF(btrim(sqlc.arg('model')::text), ''),
    sqlc.narg('dimension')::int,
    NULLIF(btrim(sqlc.arg('content_hash')::text), ''),
    NULLIF(btrim(sqlc.arg('error_message')::text), ''),
    now(),
    sqlc.narg('embedded_at')::timestamptz
)
ON CONFLICT (knowledge_item_id, workspace_id)
DO UPDATE SET
    status = EXCLUDED.status,
    provider = EXCLUDED.provider,
    model = EXCLUDED.model,
    dimension = EXCLUDED.dimension,
    content_hash = EXCLUDED.content_hash,
    error_message = EXCLUDED.error_message,
    attempted_at = EXCLUDED.attempted_at,
    embedded_at = EXCLUDED.embedded_at,
    updated_at = now()
RETURNING *;

-- name: ListKnowledgeItemsForEmbeddingRebuild :many
SELECT *
FROM knowledge_item
WHERE workspace_id = sqlc.arg('workspace_id')
  AND lifecycle_status IN ('reviewed', 'published')
ORDER BY updated_at DESC, created_at DESC
LIMIT sqlc.arg('limit');

-- name: UpsertKnowledgeEmbedding :one
INSERT INTO knowledge_embedding (
    knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedding_1536, embedded_at
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('provider'),
    sqlc.arg('model'), 1536, sqlc.arg('content_hash'), sqlc.arg('embedding')::vector(1536), now()
)
ON CONFLICT (knowledge_item_id, provider, model, dimension, content_hash)
DO UPDATE SET embedding_1536 = EXCLUDED.embedding_1536, embedded_at = now()
RETURNING id, knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedded_at, created_at;

-- name: UpsertKnowledgeEmbedding3072 :one
INSERT INTO knowledge_embedding (
    knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedding_3072, embedded_at
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('provider'),
    sqlc.arg('model'), 3072, sqlc.arg('content_hash'), sqlc.arg('embedding')::vector(3072), now()
)
ON CONFLICT (knowledge_item_id, provider, model, dimension, content_hash)
DO UPDATE SET embedding_3072 = EXCLUDED.embedding_3072, embedded_at = now()
RETURNING id, knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedded_at, created_at;

-- name: UpsertKnowledgeEmbedding1024 :one
INSERT INTO knowledge_embedding (
    knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedding_1024, embedded_at
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('provider'),
    sqlc.arg('model'), 1024, sqlc.arg('content_hash'), sqlc.arg('embedding')::vector(1024), now()
)
ON CONFLICT (knowledge_item_id, provider, model, dimension, content_hash)
DO UPDATE SET embedding_1024 = EXCLUDED.embedding_1024, embedded_at = now()
RETURNING id, knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedded_at, created_at;

-- name: UpsertKnowledgeEmbedding768 :one
INSERT INTO knowledge_embedding (
    knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedding_768, embedded_at
) VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('provider'),
    sqlc.arg('model'), 768, sqlc.arg('content_hash'), sqlc.arg('embedding')::vector(768), now()
)
ON CONFLICT (knowledge_item_id, provider, model, dimension, content_hash)
DO UPDATE SET embedding_768 = EXCLUDED.embedding_768, embedded_at = now()
RETURNING id, knowledge_item_id, workspace_id, provider, model, dimension, content_hash, embedded_at, created_at;

-- name: SearchKnowledgeText :many
WITH query_terms AS (
    SELECT COALESCE(array_agg(term), '{}')::text[] AS terms
    FROM (
        SELECT DISTINCT term
        FROM regexp_split_to_table(LOWER(sqlc.arg('query')::text), '[^[:alnum:]_]+') AS term
        WHERE length(term) > 2
    ) q
),
candidates AS (
    SELECT ki.*,
        (
            CASE WHEN LOWER(ki.title) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 4 ELSE 0 END +
            CASE WHEN LOWER(ki.problem_pattern) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 3 ELSE 0 END +
            CASE WHEN LOWER(ki.recommended_practice) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 3 ELSE 0 END +
            CASE WHEN LOWER(ki.anti_patterns) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 2 ELSE 0 END +
            CASE WHEN LOWER(ki.trigger_conditions) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 1 ELSE 0 END +
            CASE WHEN LOWER(ki.diagnostic_steps) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 1 ELSE 0 END +
            CASE WHEN LOWER(ki.applicability) LIKE '%' || LOWER(sqlc.arg('query')::text) || '%' THEN 1 ELSE 0 END +
            COALESCE((
                SELECT COUNT(*)::float8 * 0.8
                FROM unnest((SELECT terms FROM query_terms)) AS term
                WHERE LOWER(ki.title) LIKE '%' || term || '%'
            ), 0) +
            COALESCE((
                SELECT COUNT(*)::float8 * 0.6
                FROM unnest((SELECT terms FROM query_terms)) AS term
                WHERE LOWER(ki.problem_pattern) LIKE '%' || term || '%'
                   OR LOWER(ki.recommended_practice) LIKE '%' || term || '%'
            ), 0) +
            COALESCE((
                SELECT COUNT(*)::float8 * 0.3
                FROM unnest((SELECT terms FROM query_terms)) AS term
                WHERE LOWER(ki.anti_patterns) LIKE '%' || term || '%'
                   OR LOWER(ki.trigger_conditions) LIKE '%' || term || '%'
                   OR LOWER(ki.diagnostic_steps) LIKE '%' || term || '%'
                   OR LOWER(ki.applicability) LIKE '%' || term || '%'
            ), 0)
        )::float8 AS text_score
    FROM knowledge_item ki
    WHERE ki.workspace_id = sqlc.arg('workspace_id')
      AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
      AND (
          COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
          OR ki.lifecycle_status = 'published'
      )
      AND (
          COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
          OR EXISTS (
              SELECT 1
              FROM knowledge_publish_target kpt
              WHERE kpt.knowledge_item_id = ki.id
                AND kpt.workspace_id = ki.workspace_id
                AND kpt.target_type = 'rag'
          )
      )
      AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR ki.type = ANY(sqlc.narg('types')::text[]))
      AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR ki.lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
      AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
      AND (
          COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
          OR ki.domain_labels && sqlc.narg('labels')::text[]
      )
)
SELECT *
FROM candidates
WHERE text_score > 0
ORDER BY
    (text_score * GREATEST(0.2, LEAST(1, effectiveness_score / 100.0)) *
        CASE
            WHEN review_needed_at IS NULL THEN 1
            WHEN conflict_group IS NOT NULL THEN 0.25
            WHEN stale_score >= 80 THEN 0.35
            ELSE 0.6
        END
    ) DESC,
    updated_at DESC
LIMIT sqlc.arg('limit');

-- name: SearchKnowledgeVector :many
SELECT
    ki.*,
    (1 - MIN(ke.embedding_1536 <=> sqlc.arg('embedding')::vector(1536)))::float8 AS vector_score
FROM knowledge_item ki
JOIN knowledge_embedding ke ON ke.knowledge_item_id = ki.id AND ke.workspace_id = ki.workspace_id
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND ke.dimension = 1536
  AND ke.embedding_1536 IS NOT NULL
  AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR ki.lifecycle_status = 'published'
  )
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR EXISTS (
          SELECT 1
          FROM knowledge_publish_target kpt
          WHERE kpt.knowledge_item_id = ki.id
            AND kpt.workspace_id = ki.workspace_id
            AND kpt.target_type = 'rag'
      )
  )
  AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR ki.type = ANY(sqlc.narg('types')::text[]))
  AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR ki.lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR ki.domain_labels && sqlc.narg('labels')::text[]
  )
GROUP BY ki.id
ORDER BY
    ((1 - MIN(ke.embedding_1536 <=> sqlc.arg('embedding')::vector(1536))) * GREATEST(0.2, LEAST(1, ki.effectiveness_score / 100.0)) *
        CASE
            WHEN ki.review_needed_at IS NULL THEN 1
            WHEN ki.conflict_group IS NOT NULL THEN 0.25
            WHEN ki.stale_score >= 80 THEN 0.35
            ELSE 0.6
        END
    ) DESC,
    ki.updated_at DESC
LIMIT sqlc.arg('limit');

-- name: SearchKnowledgeVector3072 :many
SELECT
    ki.*,
    (1 - MIN(ke.embedding_3072 <=> sqlc.arg('embedding')::vector(3072)))::float8 AS vector_score
FROM knowledge_item ki
JOIN knowledge_embedding ke ON ke.knowledge_item_id = ki.id AND ke.workspace_id = ki.workspace_id
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND ke.dimension = 3072
  AND ke.embedding_3072 IS NOT NULL
  AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR ki.lifecycle_status = 'published'
  )
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR EXISTS (
          SELECT 1
          FROM knowledge_publish_target kpt
          WHERE kpt.knowledge_item_id = ki.id
            AND kpt.workspace_id = ki.workspace_id
            AND kpt.target_type = 'rag'
      )
  )
  AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR ki.type = ANY(sqlc.narg('types')::text[]))
  AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR ki.lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR ki.domain_labels && sqlc.narg('labels')::text[]
  )
GROUP BY ki.id
ORDER BY
    ((1 - MIN(ke.embedding_3072 <=> sqlc.arg('embedding')::vector(3072))) * GREATEST(0.2, LEAST(1, ki.effectiveness_score / 100.0)) *
        CASE
            WHEN ki.review_needed_at IS NULL THEN 1
            WHEN ki.conflict_group IS NOT NULL THEN 0.25
            WHEN ki.stale_score >= 80 THEN 0.35
            ELSE 0.6
        END
    ) DESC,
    ki.updated_at DESC
LIMIT sqlc.arg('limit');

-- name: SearchKnowledgeVector1024 :many
SELECT
    ki.*,
    (1 - MIN(ke.embedding_1024 <=> sqlc.arg('embedding')::vector(1024)))::float8 AS vector_score
FROM knowledge_item ki
JOIN knowledge_embedding ke ON ke.knowledge_item_id = ki.id AND ke.workspace_id = ki.workspace_id
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND ke.dimension = 1024
  AND ke.embedding_1024 IS NOT NULL
  AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR ki.lifecycle_status = 'published'
  )
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR EXISTS (
          SELECT 1
          FROM knowledge_publish_target kpt
          WHERE kpt.knowledge_item_id = ki.id
            AND kpt.workspace_id = ki.workspace_id
            AND kpt.target_type = 'rag'
      )
  )
  AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR ki.type = ANY(sqlc.narg('types')::text[]))
  AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR ki.lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR ki.domain_labels && sqlc.narg('labels')::text[]
  )
GROUP BY ki.id
ORDER BY
    ((1 - MIN(ke.embedding_1024 <=> sqlc.arg('embedding')::vector(1024))) * GREATEST(0.2, LEAST(1, ki.effectiveness_score / 100.0)) *
        CASE
            WHEN ki.review_needed_at IS NULL THEN 1
            WHEN ki.conflict_group IS NOT NULL THEN 0.25
            WHEN ki.stale_score >= 80 THEN 0.35
            ELSE 0.6
        END
    ) DESC,
    ki.updated_at DESC
LIMIT sqlc.arg('limit');

-- name: SearchKnowledgeVector768 :many
SELECT
    ki.*,
    (1 - MIN(ke.embedding_768 <=> sqlc.arg('embedding')::vector(768)))::float8 AS vector_score
FROM knowledge_item ki
JOIN knowledge_embedding ke ON ke.knowledge_item_id = ki.id AND ke.workspace_id = ki.workspace_id
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND ke.dimension = 768
  AND ke.embedding_768 IS NOT NULL
  AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR ki.lifecycle_status = 'published'
  )
  AND (
      COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) > 0
      OR EXISTS (
          SELECT 1
          FROM knowledge_publish_target kpt
          WHERE kpt.knowledge_item_id = ki.id
            AND kpt.workspace_id = ki.workspace_id
            AND kpt.target_type = 'rag'
      )
  )
  AND (COALESCE(cardinality(sqlc.narg('types')::text[]), 0) = 0 OR ki.type = ANY(sqlc.narg('types')::text[]))
  AND (COALESCE(cardinality(sqlc.narg('statuses')::text[]), 0) = 0 OR ki.lifecycle_status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
  AND (
      COALESCE(cardinality(sqlc.narg('labels')::text[]), 0) = 0
      OR ki.domain_labels && sqlc.narg('labels')::text[]
  )
GROUP BY ki.id
ORDER BY
    ((1 - MIN(ke.embedding_768 <=> sqlc.arg('embedding')::vector(768))) * GREATEST(0.2, LEAST(1, ki.effectiveness_score / 100.0)) *
        CASE
            WHEN ki.review_needed_at IS NULL THEN 1
            WHEN ki.conflict_group IS NOT NULL THEN 0.25
            WHEN ki.stale_score >= 80 THEN 0.35
            ELSE 0.6
        END
    ) DESC,
    ki.updated_at DESC
LIMIT sqlc.arg('limit');

-- name: CreateKnowledgeFeedback :one
INSERT INTO knowledge_feedback (knowledge_item_id, workspace_id, member_id, agent_task_id, value, note)
VALUES (
    sqlc.arg('knowledge_item_id'), sqlc.arg('workspace_id'), sqlc.arg('member_id'),
    sqlc.narg('agent_task_id'), sqlc.arg('value'), sqlc.narg('note')
)
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
    workspace_id, member_id, agent_task_id, query, query_source, retrieval_mode,
    filters, result_count, top_knowledge_item_ids, result_scores
) VALUES (
    sqlc.arg('workspace_id'), sqlc.narg('member_id'), sqlc.narg('agent_task_id'),
    sqlc.narg('query'), sqlc.arg('query_source'), sqlc.arg('retrieval_mode'),
    COALESCE(sqlc.narg('filters'), '{}'::jsonb), sqlc.arg('result_count'),
    COALESCE(sqlc.narg('top_knowledge_item_ids')::uuid[], '{}'),
    COALESCE(sqlc.narg('result_scores'), '[]'::jsonb)
)
RETURNING *;

-- name: CreateKnowledgeInjectionEvent :one
INSERT INTO knowledge_injection_event (
    workspace_id, knowledge_item_id, agent_task_id, injection_target,
    retrieval_event_id, rank, score, injection_reason, token_budget, discarded_reason
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('knowledge_item_id'), sqlc.narg('agent_task_id'),
    sqlc.arg('injection_target'), sqlc.narg('retrieval_event_id'), sqlc.narg('rank'),
    sqlc.narg('score'), sqlc.narg('injection_reason'), sqlc.narg('token_budget'),
    sqlc.narg('discarded_reason')
)
RETURNING *;

-- name: CreateKnowledgeUsageEvent :one
INSERT INTO knowledge_usage_event (
    workspace_id, knowledge_item_id, agent_task_id, usage_source,
    reference_text, task_status, task_result
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('knowledge_item_id'), sqlc.narg('agent_task_id'),
    sqlc.arg('usage_source'), sqlc.narg('reference_text'), sqlc.narg('task_status'),
    sqlc.narg('task_result')
)
RETURNING *;

-- name: ListKnowledgeItemsByIDs :many
SELECT *
FROM knowledge_item
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = ANY(sqlc.arg('ids')::uuid[]);

-- name: ListKnowledgeAnalytics :many
WITH filtered_items AS (
    SELECT ki.*
    FROM knowledge_item ki
    WHERE ki.workspace_id = sqlc.arg('workspace_id')
      AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR ki.id = sqlc.narg('knowledge_item_id'))
      AND (sqlc.narg('project_id')::uuid IS NULL OR ki.project_id = sqlc.narg('project_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR ki.agent_id = sqlc.narg('agent_id'))
),
retrieval AS (
    SELECT kid AS knowledge_item_id, COUNT(*)::bigint AS retrieval_count
    FROM knowledge_retrieval_event kre
    CROSS JOIN LATERAL unnest(kre.top_knowledge_item_ids) AS kid
    LEFT JOIN agent_task_queue atq ON atq.id = kre.agent_task_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    WHERE kre.workspace_id = sqlc.arg('workspace_id')
      AND kre.created_at >= sqlc.arg('since')::timestamptz
      AND kre.created_at < sqlc.arg('until')::timestamptz
      AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR kid = sqlc.narg('knowledge_item_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id'))
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
    GROUP BY kid
),
injection AS (
    SELECT knowledge_item_id,
        COUNT(*)::bigint AS injection_count,
        COUNT(DISTINCT agent_task_id) FILTER (WHERE agent_task_id IS NOT NULL)::bigint AS injected_task_count
    FROM knowledge_injection_event kie
    LEFT JOIN agent_task_queue atq ON atq.id = kie.agent_task_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    WHERE kie.workspace_id = sqlc.arg('workspace_id')
      AND kie.created_at >= sqlc.arg('since')::timestamptz
      AND kie.created_at < sqlc.arg('until')::timestamptz
      AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR kie.knowledge_item_id = sqlc.narg('knowledge_item_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id'))
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
    GROUP BY knowledge_item_id
),
usage AS (
    SELECT knowledge_item_id,
        COUNT(*)::bigint AS usage_count,
        COUNT(*) FILTER (WHERE usage_source = 'agent_reference')::bigint AS agent_reference_count,
        COUNT(*) FILTER (WHERE usage_source = 'active_search')::bigint AS active_search_count
    FROM knowledge_usage_event kue
    LEFT JOIN agent_task_queue atq ON atq.id = kue.agent_task_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    WHERE kue.workspace_id = sqlc.arg('workspace_id')
      AND kue.created_at >= sqlc.arg('since')::timestamptz
      AND kue.created_at < sqlc.arg('until')::timestamptz
      AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR kue.knowledge_item_id = sqlc.narg('knowledge_item_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id'))
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
    GROUP BY knowledge_item_id
),
feedback AS (
    SELECT knowledge_item_id,
        COUNT(*) FILTER (WHERE value = 'helpful')::bigint AS helpful_count,
        COUNT(*) FILTER (WHERE value = 'not_helpful')::bigint AS not_helpful_count,
        COUNT(*) FILTER (WHERE value = 'misleading')::bigint AS misleading_count,
        COUNT(*) FILTER (WHERE value = 'outdated')::bigint AS outdated_count,
        MAX(kf.created_at) FILTER (WHERE value IN ('not_helpful', 'misleading', 'outdated'))::timestamptz AS latest_negative_feedback_at
    FROM knowledge_feedback kf
    LEFT JOIN agent_task_queue atq ON atq.id = kf.agent_task_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    WHERE kf.workspace_id = sqlc.arg('workspace_id')
      AND kf.created_at >= sqlc.arg('since')::timestamptz
      AND kf.created_at < sqlc.arg('until')::timestamptz
      AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR kf.knowledge_item_id = sqlc.narg('knowledge_item_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id'))
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
    GROUP BY knowledge_item_id
),
task_edges AS (
    SELECT knowledge_item_id, agent_task_id
    FROM knowledge_injection_event
    WHERE workspace_id = sqlc.arg('workspace_id') AND agent_task_id IS NOT NULL
    UNION
    SELECT knowledge_item_id, agent_task_id
    FROM knowledge_usage_event
    WHERE workspace_id = sqlc.arg('workspace_id') AND agent_task_id IS NOT NULL
),
task_token AS (
    SELECT
        task_id,
        SUM(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens)::bigint AS total_tokens
    FROM task_usage
    GROUP BY task_id
),
task_outcome AS (
    SELECT te.knowledge_item_id,
        COUNT(DISTINCT atq.id) FILTER (WHERE atq.status = 'completed')::bigint AS successful_task_count,
        COUNT(DISTINCT atq.id) FILTER (WHERE atq.status = 'failed')::bigint AS failed_task_count,
        COALESCE(SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at))) FILTER (
            WHERE atq.status IN ('completed', 'failed')
              AND atq.started_at IS NOT NULL
              AND atq.completed_at IS NOT NULL
        ), 0)::bigint AS total_task_seconds,
        COALESCE(SUM(tt.total_tokens), 0)::bigint AS total_tokens
    FROM task_edges te
    JOIN agent_task_queue atq ON atq.id = te.agent_task_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    LEFT JOIN task_token tt ON tt.task_id = atq.id
    WHERE atq.completed_at >= sqlc.arg('since')::timestamptz
      AND atq.completed_at < sqlc.arg('until')::timestamptz
      AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR te.knowledge_item_id = sqlc.narg('knowledge_item_id'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id'))
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
    GROUP BY te.knowledge_item_id
)
SELECT
    fi.id AS knowledge_item_id,
    fi.title,
    fi.type,
    fi.lifecycle_status,
    COALESCE(r.retrieval_count, 0)::bigint AS retrieval_count,
    COALESCE(i.injection_count, 0)::bigint AS injection_count,
    COALESCE(i.injected_task_count, 0)::bigint AS injected_task_count,
    COALESCE(u.usage_count, 0)::bigint AS usage_count,
    COALESCE(u.agent_reference_count, 0)::bigint AS agent_reference_count,
    COALESCE(u.active_search_count, 0)::bigint AS active_search_count,
    COALESCE(f.helpful_count, 0)::bigint AS helpful_count,
    COALESCE(f.not_helpful_count, 0)::bigint AS not_helpful_count,
    COALESCE(f.misleading_count, 0)::bigint AS misleading_count,
    COALESCE(f.outdated_count, 0)::bigint AS outdated_count,
    f.latest_negative_feedback_at,
    COALESCE(t.successful_task_count, 0)::bigint AS successful_task_count,
    COALESCE(t.failed_task_count, 0)::bigint AS failed_task_count,
    COALESCE(t.total_task_seconds, 0)::bigint AS total_task_seconds,
    COALESCE(t.total_tokens, 0)::bigint AS total_tokens
FROM filtered_items fi
LEFT JOIN retrieval r ON r.knowledge_item_id = fi.id
LEFT JOIN injection i ON i.knowledge_item_id = fi.id
LEFT JOIN usage u ON u.knowledge_item_id = fi.id
LEFT JOIN feedback f ON f.knowledge_item_id = fi.id
LEFT JOIN task_outcome t ON t.knowledge_item_id = fi.id
WHERE (
    sqlc.arg('include_zero')::boolean
    OR COALESCE(r.retrieval_count, 0) > 0
    OR COALESCE(i.injection_count, 0) > 0
    OR COALESCE(u.usage_count, 0) > 0
    OR COALESCE(f.helpful_count, 0) + COALESCE(f.not_helpful_count, 0) + COALESCE(f.misleading_count, 0) + COALESCE(f.outdated_count, 0) > 0
)
ORDER BY usage_count DESC, injection_count DESC, retrieval_count DESC, fi.updated_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: UpsertKnowledgeCandidate :one
INSERT INTO knowledge_candidate (
    workspace_id, issue_id, comment_id, agent_task_id, source_type, source_id,
    trigger_reason, signal_strength, signals, score, status, dedupe_key,
    created_by, metadata, evidence, evaluated_at
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('issue_id'), sqlc.narg('comment_id'),
    sqlc.narg('agent_task_id'), sqlc.arg('source_type'), sqlc.arg('source_id'),
    sqlc.arg('trigger_reason'), sqlc.arg('signal_strength'),
    COALESCE(sqlc.narg('signals')::text[], '{}'), sqlc.arg('score'), sqlc.arg('status'),
    sqlc.arg('dedupe_key'), sqlc.narg('created_by'),
    COALESCE(sqlc.narg('metadata'), '{}'::jsonb),
    COALESCE(sqlc.narg('evidence'), '{}'::jsonb), now()
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
    evidence = EXCLUDED.evidence,
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

-- name: ListKnowledgeGovernanceCandidates :many
WITH feedback AS (
    SELECT knowledge_item_id,
        COUNT(*) FILTER (WHERE value = 'helpful')::bigint AS helpful_count,
        COUNT(*) FILTER (WHERE value = 'not_helpful')::bigint AS not_helpful_count,
        COUNT(*) FILTER (WHERE value = 'misleading')::bigint AS misleading_count,
        COUNT(*) FILTER (WHERE value = 'outdated')::bigint AS outdated_count,
        MAX(created_at) FILTER (WHERE value IN ('not_helpful', 'misleading', 'outdated'))::timestamptz AS latest_negative_feedback_at
    FROM knowledge_feedback
    WHERE workspace_id = sqlc.arg('workspace_id')
    GROUP BY knowledge_item_id
),
retrieval AS (
    SELECT kid AS knowledge_item_id, COUNT(*)::bigint AS retrieval_count
    FROM knowledge_retrieval_event kre
    CROSS JOIN LATERAL unnest(kre.top_knowledge_item_ids) AS kid
    WHERE kre.workspace_id = sqlc.arg('workspace_id')
    GROUP BY kid
),
injection AS (
    SELECT knowledge_item_id, COUNT(*)::bigint AS injection_count
    FROM knowledge_injection_event
    WHERE workspace_id = sqlc.arg('workspace_id')
    GROUP BY knowledge_item_id
),
usage AS (
    SELECT knowledge_item_id, COUNT(*)::bigint AS usage_count
    FROM knowledge_usage_event
    WHERE workspace_id = sqlc.arg('workspace_id')
    GROUP BY knowledge_item_id
)
SELECT
    ki.*,
    COALESCE(f.helpful_count, 0)::bigint AS helpful_count,
    COALESCE(f.not_helpful_count, 0)::bigint AS not_helpful_count,
    COALESCE(f.misleading_count, 0)::bigint AS misleading_count,
    COALESCE(f.outdated_count, 0)::bigint AS outdated_count,
    f.latest_negative_feedback_at,
    COALESCE(r.retrieval_count, 0)::bigint AS retrieval_count,
    COALESCE(i.injection_count, 0)::bigint AS injection_count,
    COALESCE(u.usage_count, 0)::bigint AS usage_count
FROM knowledge_item ki
LEFT JOIN feedback f ON f.knowledge_item_id = ki.id
LEFT JOIN retrieval r ON r.knowledge_item_id = ki.id
LEFT JOIN injection i ON i.knowledge_item_id = ki.id
LEFT JOIN usage u ON u.knowledge_item_id = ki.id
WHERE ki.workspace_id = sqlc.arg('workspace_id')
  AND ki.lifecycle_status NOT IN ('archived', 'deprecated')
ORDER BY ki.updated_at DESC
LIMIT sqlc.arg('limit');

-- name: UpdateKnowledgeGovernance :one
UPDATE knowledge_item SET
    stale_score = sqlc.arg('stale_score'),
    effectiveness_score = sqlc.arg('effectiveness_score'),
    conflict_group = sqlc.arg('conflict_group'),
    review_reason = sqlc.arg('review_reason'),
    update_suggestion = sqlc.arg('update_suggestion'),
    review_needed_at = CASE
        WHEN NULLIF(btrim(sqlc.arg('review_reason')::text), '') IS NULL THEN NULL
        ELSE COALESCE(review_needed_at, now())
    END,
    governance_checked_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DismissKnowledgeGovernance :one
UPDATE knowledge_item SET
    review_reason = NULL,
    update_suggestion = NULL,
    review_needed_at = NULL,
    conflict_group = NULL,
    governance_checked_at = now(),
    updated_by = sqlc.arg('updated_by'),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ListKnowledgeGovernanceFindings :many
SELECT *
FROM knowledge_governance_finding
WHERE workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('finding_type')::text IS NULL OR finding_type = sqlc.narg('finding_type'))
  AND (sqlc.narg('knowledge_item_id')::uuid IS NULL OR knowledge_item_id = sqlc.narg('knowledge_item_id'))
ORDER BY
    CASE status WHEN 'open' THEN 0 WHEN 'drafted' THEN 1 ELSE 2 END,
    severity DESC,
    updated_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: GetKnowledgeGovernanceFinding :one
SELECT *
FROM knowledge_governance_finding
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');

-- name: UpsertKnowledgeGovernanceFinding :one
INSERT INTO knowledge_governance_finding (
    workspace_id, knowledge_item_id, finding_type, status, severity,
    reason, evidence, suggested_action, source_map
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('knowledge_item_id'), sqlc.arg('finding_type'),
    'open', sqlc.arg('severity'), sqlc.arg('reason'), COALESCE(sqlc.narg('evidence'), '{}'::jsonb),
    sqlc.arg('suggested_action'), COALESCE(sqlc.narg('source_map'), '{}'::jsonb)
)
ON CONFLICT (workspace_id, knowledge_item_id, finding_type)
DO UPDATE SET
    status = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.status
        WHEN knowledge_governance_finding.status = 'drafted'
            THEN 'drafted'
        ELSE 'open'
    END,
    severity = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.severity
        ELSE EXCLUDED.severity
    END,
    reason = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.reason
        ELSE EXCLUDED.reason
    END,
    evidence = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.evidence
        ELSE EXCLUDED.evidence
    END,
    suggested_action = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.suggested_action
        ELSE EXCLUDED.suggested_action
    END,
    source_map = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.source_map
        ELSE EXCLUDED.source_map
    END,
    updated_at = CASE
        WHEN knowledge_governance_finding.status IN ('accepted', 'rejected', 'dismissed', 'archived', 'deprecated')
            THEN knowledge_governance_finding.updated_at
        ELSE now()
    END
RETURNING *;

-- name: UpdateKnowledgeGovernanceFindingStatus :one
UPDATE knowledge_governance_finding SET
    status = sqlc.arg('status'),
    draft_knowledge_item_id = COALESCE(sqlc.narg('draft_knowledge_item_id'), draft_knowledge_item_id),
    resolved_by = CASE
        WHEN sqlc.arg('status')::text IN ('accepted', 'rejected', 'archived', 'deprecated') THEN sqlc.narg('actor_id')
        ELSE resolved_by
    END,
    resolved_at = CASE
        WHEN sqlc.arg('status')::text IN ('accepted', 'rejected', 'archived', 'deprecated') THEN now()
        ELSE resolved_at
    END,
    dismissed_by = CASE
        WHEN sqlc.arg('status')::text = 'dismissed' THEN sqlc.narg('actor_id')
        ELSE dismissed_by
    END,
    dismissed_at = CASE
        WHEN sqlc.arg('status')::text = 'dismissed' THEN now()
        ELSE dismissed_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DismissKnowledgeGovernanceFindingsForItem :many
UPDATE knowledge_governance_finding SET
    status = 'dismissed',
    dismissed_by = sqlc.arg('actor_id'),
    dismissed_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND status IN ('open', 'drafted')
RETURNING *;

-- name: ListNegativeKnowledgeFeedback :many
SELECT *
FROM knowledge_feedback
WHERE workspace_id = sqlc.arg('workspace_id')
  AND knowledge_item_id = sqlc.arg('knowledge_item_id')
  AND value IN ('not_helpful', 'misleading', 'outdated')
ORDER BY created_at DESC
LIMIT sqlc.arg('limit');

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

-- name: ListKnowledgeEffectHourly :many
SELECT bucket_hour, workspace_id, agent_id, project_id,
       model, provider, task_kind, has_injection,
       task_count, successful_count, failed_count,
       total_duration_secs, duration_task_count,
       input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
       rerun_count, follow_up_count, max_attempt
FROM knowledge_effect_hourly
WHERE workspace_id = sqlc.arg('workspace_id')
  AND bucket_hour >= sqlc.arg('since')::timestamptz
  AND bucket_hour <  sqlc.arg('until')::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('task_kind')::text IS NULL OR task_kind = sqlc.narg('task_kind'))
  AND (sqlc.narg('has_injection')::boolean IS NULL OR has_injection = sqlc.narg('has_injection'))
  AND (sqlc.narg('model')::text IS NULL OR model = sqlc.narg('model'))
ORDER BY bucket_hour DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: GetKnowledgeEffectSummary :one
SELECT
    COALESCE(SUM(task_count), 0)::bigint AS total_tasks,
    COALESCE(SUM(successful_count), 0)::bigint AS total_successful,
    COALESCE(SUM(failed_count), 0)::bigint AS total_failed,
    COALESCE(SUM(total_duration_secs), 0)::double precision AS total_duration_secs,
    COALESCE(SUM(duration_task_count), 0)::bigint AS total_duration_tasks,
    COALESCE(SUM(input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COALESCE(SUM(rerun_count), 0)::bigint AS total_reruns,
    COALESCE(SUM(follow_up_count), 0)::bigint AS total_follow_ups
FROM knowledge_effect_hourly
WHERE workspace_id = sqlc.arg('workspace_id')
  AND bucket_hour >= sqlc.arg('since')::timestamptz
  AND bucket_hour <  sqlc.arg('until')::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('task_kind')::text IS NULL OR task_kind = sqlc.narg('task_kind'))
  AND (sqlc.narg('has_injection')::boolean IS NULL OR has_injection = sqlc.narg('has_injection'))
  AND (sqlc.narg('model')::text IS NULL OR model = sqlc.narg('model'));

-- name: ListKnowledgeInjectionsByIssue :many
SELECT
    kie.id AS injection_event_id,
    kie.knowledge_item_id,
    kie.agent_task_id,
    kie.injection_target,
    kie.retrieval_event_id,
    kie.rank,
    kie.score,
    kie.injection_reason,
    kie.token_budget,
    kie.discarded_reason,
    kie.created_at AS injected_at,
    ki.title AS knowledge_title,
    ki.type AS knowledge_type,
    ki.lifecycle_status AS knowledge_lifecycle_status,
    EXISTS (
        SELECT 1 FROM knowledge_usage_event kue
        WHERE kue.knowledge_item_id = kie.knowledge_item_id
          AND kue.agent_task_id = kie.agent_task_id
    ) AS was_used,
    (
        SELECT ks.source_id
        FROM knowledge_source ks
        WHERE ks.knowledge_item_id = kie.knowledge_item_id
          AND ks.source_type = 'issue'
          AND ks.workspace_id = kie.workspace_id
        ORDER BY ks.created_at ASC
        LIMIT 1
    ) AS source_issue_id
FROM knowledge_injection_event kie
JOIN agent_task_queue atq ON atq.id = kie.agent_task_id
JOIN knowledge_item ki ON ki.id = kie.knowledge_item_id AND ki.workspace_id = kie.workspace_id
WHERE atq.issue_id = sqlc.arg('issue_id')
  AND kie.workspace_id = sqlc.arg('workspace_id')
  AND kie.discarded_reason IS NULL
ORDER BY kie.rank ASC NULLS LAST, kie.score DESC NULLS LAST;
