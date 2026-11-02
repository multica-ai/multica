-- The boss's ratings of briefs (spec 2.8). Every query filters by workspace_id;
-- there are no foreign keys.

-- name: UpsertBriefFeedback :one
-- One rating per (brief, member): a second submission replaces the rating, its
-- tags and note, and moves updated_at. created_at keeps the first submission.
INSERT INTO brief_feedback (
    id, workspace_id, brief_id, member_id, rating, tags, note, created_at, updated_at
) VALUES (
    @id, @workspace_id, @brief_id, @member_id, @rating, @tags, @note, now(), now()
)
ON CONFLICT (brief_id, member_id) DO UPDATE SET
    rating     = EXCLUDED.rating,
    tags       = EXCLUDED.tags,
    note       = EXCLUDED.note,
    updated_at = now()
RETURNING *;

-- name: GetBriefFeedbackForMember :one
SELECT * FROM brief_feedback
WHERE workspace_id = @workspace_id AND brief_id = @brief_id AND member_id = @member_id;

-- name: ListRecentBriefFeedback :many
-- The workspace's ratings given since the cut-off, newest first, with the brief's
-- kind and publish time so the reader can date each one.
SELECT f.id, f.brief_id, f.member_id, f.rating, f.tags, f.note, f.created_at, f.updated_at,
       b.kind AS brief_kind, b.created_at AS brief_created_at
FROM brief_feedback f
JOIN brief b ON b.id = f.brief_id AND b.workspace_id = f.workspace_id
WHERE f.workspace_id = @workspace_id AND f.updated_at >= @since
ORDER BY f.updated_at DESC, f.id DESC
LIMIT @row_limit;

-- name: CountBriefFeedbackSince :one
-- The usefulness inputs: how many ratings the window holds, and how many are useful.
SELECT count(*)::bigint AS ratings,
       (count(*) FILTER (WHERE rating = 'useful'))::bigint AS useful
FROM brief_feedback
WHERE workspace_id = @workspace_id AND updated_at >= @since;
