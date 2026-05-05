-- CEREBRO-PATCH(sqlc-user-profile): cerebro modification of upstream file
-- name: GetUserProfile :one
SELECT * FROM user_profile
WHERE user_id = $1;

-- name: UpsertUserProfile :one
INSERT INTO user_profile (
    user_id, persona, language, length_pref, autonomy_pref, tech_pref, anti_patterns
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id) DO UPDATE SET
    persona = EXCLUDED.persona,
    language = EXCLUDED.language,
    length_pref = EXCLUDED.length_pref,
    autonomy_pref = EXCLUDED.autonomy_pref,
    tech_pref = EXCLUDED.tech_pref,
    anti_patterns = EXCLUDED.anti_patterns,
    updated_at = now()
RETURNING *;

-- name: DeleteUserProfile :exec
DELETE FROM user_profile WHERE user_id = $1;
